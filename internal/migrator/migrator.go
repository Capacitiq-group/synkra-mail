// Package migrator: the IMAP migration assistant worker (phase 11).
//
// Flow per job:
//  1. connect to the source IMAP host (config-selected security),
//  2. LIST the source folders, build the folder map (source → dest name),
//  3. stamp migration_item rows for every source message (progress probe),
//  4. process items one at a time: FETCH the raw RFC822, deliver through the
//     normal ingest path (all the wire normalisation, spam analysis and blob
//     handling of a real delivery), then move the item from INBOX to the
//     mapped destination folder,
//  5. pause/cancel are honoured BETWEEN items by re-reading the job state —
//     no item is ever half-delivered.
//
// Failures are per-message: a bad message lands on the attention list, the
// job continues.
package migrator

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"

	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/store"
)

type Deps struct {
	Store *store.Store
	Blobs blob.Store
	Cfg   *config.Config
}

// SourceConfig is the job's source jsonb (unsealed copy for the worker).
type SourceConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // none | starttls | tls
	Username string `json:"username"`
	Password string `json:"password"` // sealed in the DB row; unsealed here
}

func UnsealSource(raw json.RawMessage, kmsKey []byte) (*SourceConfig, error) {
	var s SourceConfig
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.Password == "" {
		return nil, errors.New("migration source password missing")
	}
	if len(s.Password) > 4 && s.Password[:5] == "enc1." {
		plain, err := krypto.OpenSealedString(kmsKey, s.Password)
		if err != nil {
			return nil, err
		}
		s.Password = plain
	}
	return &s, nil
}

// dial opens the IMAP connection with the configured security mode.
func dial(ctx context.Context, s *SourceConfig) (*imapclient.Client, error) {
	if s.Security != "none" && s.Security != "starttls" && s.Security != "tls" {
		return nil, fmt.Errorf("unsupported security %q", s.Security)
	}
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	opts := &imapclient.Options{}
	if s.Security == "tls" {
		opts.TLSConfig = &tls.Config{ServerName: s.Host}
		c, err := imapclient.DialTLS(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("dial tls: %w", err)
		}
		return c, nil
	}
	if s.Security == "starttls" {
		opts.TLSConfig = &tls.Config{ServerName: s.Host}
		return imapclient.DialStartTLS(addr, opts)
	}
	c, err := imapclient.DialInsecure(addr, opts)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	return c, nil
}

// ProbeSource connects and lists the source folders with message counts —
// the wizard's folder-mapping screen (nothing persisted).
type SourceFolder struct {
	Name   string `json:"name"`
	Total  int    `json:"total"`
	Unseen int    `json:"unseen"`
	Dest   string `json:"dest"` // suggested destination folder
}

func ProbeSource(ctx context.Context, s *SourceConfig) ([]SourceFolder, error) {
	c, err := dial(ctx, s)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if err := c.Login(s.Username, s.Password).Wait(); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	listCmd := c.List("", "*", &imap.ListOptions{})
	out := make([]SourceFolder, 0, 16)
	for {
		mbox := listCmd.Next()
		if mbox == nil {
			break
		}
		name := mbox.Mailbox
		status, err := c.Status(name, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
		if err != nil {
			continue
		}
		total, unseen := 0, 0
		if status.NumMessages != nil {
			total = int(*status.NumMessages)
		}
		if status.NumUnseen != nil {
			unseen = int(*status.NumUnseen)
		}
		out = append(out, SourceFolder{
			Name:   name,
			Total:  total,
			Unseen: unseen,
			Dest:   suggestedDest(name),
		})
	}
	return out, listCmd.Close()
}

func suggestedDest(src string) string {
	switch {
	case equalFold(src, "INBOX"):
		return "INBOX"
	case containsFold(src, "sent"):
		return "Sent"
	case containsFold(src, "draft"):
		return "Drafts"
	case containsFold(src, "trash") || containsFold(src, "deleted"):
		return "Trash"
	case containsFold(src, "spam") || containsFold(src, "junk"):
		return "Spam"
	case containsFold(src, "archive"):
		return "Archive"
	default:
		return src
	}
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := lower(a[i]), lower(b[i])
		if ca != cb {
			return false
		}
	}
	return true
}

func containsFold(s, sub string) bool {
	n := len(sub)
	if n == 0 {
		return true
	}
	for i := 0; i+n <= len(s); i++ {
		ok := true
		for j := 0; j < n; j++ {
			if lower(s[i+j]) != lower(sub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func lower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

// InventoryJob connects, enumerates source messages and stamps migration_item
// rows + the folder map + the total. Runs when the job first starts.
func (d *Deps) InventoryJob(ctx context.Context, job *store.MigrationJob, src *SourceConfig) error {
	c, err := dial(ctx, src)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Login(src.Username, src.Password).Wait(); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	items := make([]store.MigrationItem, 0, 256)
	folderMap := map[string]string{}
	total := 0
	listCmd := c.List("", "*", &imap.ListOptions{})
	for {
		mbox := listCmd.Next()
		if mbox == nil {
			break
		}
		name := mbox.Mailbox
		if equalFold(name, "INBOX") {
			name = "INBOX"
		}
		dest := suggestedDest(name)
		folderMap[name] = dest
		sel, err := c.Select(name, nil).Wait()
		if err != nil {
			continue
		}
		if sel.NumMessages == 0 {
			continue
		}
		var seqSet imap.SeqSet
		seqSet.AddRange(1, sel.NumMessages)
		fetch := c.Fetch(seqSet,
			&imap.FetchOptions{UID: true, Envelope: true, RFC822Size: true})
		for {
			msg := fetch.Next()
			if msg == nil {
				break
			}
			m, merr := msg.Collect()
			if merr != nil || m == nil || m.Envelope == nil {
				continue
			}
			from := ""
			if len(m.Envelope.From) > 0 {
				from = fmt.Sprintf("%s@%s", m.Envelope.From[0].Mailbox, m.Envelope.From[0].Host)
			}
			items = append(items, store.MigrationItem{
				Folder: name, UID: int64(m.UID), Subject: m.Envelope.Subject,
				FromAddr: from, Size: int64(m.RFC822Size), State: "pending",
			})
			total++
		}
	}
	_ = listCmd.Close()

	return d.Store.WithTx(ctx, func(tx store.Tx) error {
		fm, _ := json.Marshal(folderMap)
		if _, err := tx.Exec(ctx, `update mail.migration_job set folder_map=$2, updated_at=now() where id=$1`, job.ID, fm); err != nil {
			return err
		}
		if err := store.SetMigrationJobTotals(ctx, tx, job.ID, total, nil); err != nil {
			return err
		}
		return store.ReplaceMigrationItems(ctx, tx, job.ID, items)
	})
}

// ProcessJob runs one work cycle for a claimed job: inventory (first run) or
// the next batch of items. Returns true when the job reached a terminal state.
func (d *Deps) ProcessJob(ctx context.Context, job *store.MigrationJob) (finished bool, err error) {
	src, err := UnsealSource(job.Source, d.Cfg.KMSKeyBytes())
	if err != nil {
		_ = d.Store.WithTx(ctx, func(tx store.Tx) error {
			if err := store.SetMigrationJobTotals(ctx, tx, job.ID, job.Total, strPtr("source credentials could not be read: "+err.Error())); err != nil {
				return err
			}
			return store.TransitionMigrationJob(ctx, tx, job.ID, []string{"queued", "running"}, "failed")
		})
		return true, nil
	}

	// First cycle: inventory.
	if job.Total == 0 {
		if ierr := d.InventoryJob(ctx, job, src); ierr != nil {
			_ = d.Store.WithTx(ctx, func(tx store.Tx) error {
				if err := store.SetMigrationJobTotals(ctx, tx, job.ID, 0, strPtr("inventory failed: "+ierr.Error())); err != nil {
					return err
				}
				return store.TransitionMigrationJob(ctx, tx, job.ID, []string{"queued", "running"}, "failed")
			})
			return true, nil
		}
		return false, nil // next cycle processes items
	}

	c, err := dial(ctx, src)
	if err != nil {
		return false, nil // leave the job claimed; retried next tick
	}
	defer c.Close()
	if err := c.Login(src.Username, src.Password).Wait(); err != nil {
		return false, nil
	}

	qd := &ingest.Deps{Store: d.Store, Blobs: d.Blobs, Cfg: d.Cfg}
	for i := 0; i < 25; i++ { // one heartbeat per 25 items
		// Pause/cancel between items — never mid-delivery.
		cur, err := store.GetMigrationJob(ctx, d.Store.Pool, job.ID)
		if err != nil {
			return true, err
		}
		if cur.State == "paused" {
			return false, nil
		}
		if cur.State == "cancelled" {
			return true, nil
		}

		var item *store.MigrationItem
		var done bool
		if err := d.Store.WithTx(ctx, func(tx store.Tx) error {
			item, err = store.ClaimMigrationItem(ctx, tx, job.ID)
			if err != nil {
				return err
			}
			if item == nil {
				// Nothing pending: check completion.
				done = true
			}
			return nil
		}); err != nil {
			return true, err
		}
		if done {
			if err := d.Store.WithTx(ctx, func(tx store.Tx) error {
				return store.TransitionMigrationJob(ctx, tx, job.ID,
					[]string{"running", "queued"}, "completed")
			}); err != nil {
				return true, err
			}
			return true, nil
		}

		d.deliverOne(ctx, qd, c, job, item)
	}
	return false, nil
}

// deliverOne fetches one message and delivers + moves it. Per-message
// failures land on the attention list; the job continues.
func (d *Deps) deliverOne(ctx context.Context, qd *ingest.Deps, c *imapclient.Client, job *store.MigrationJob, item *store.MigrationItem) {
	fail := func(err error) {
		log.Printf("[migrator] job %s %s/%d: %v", job.ID, item.Folder, item.UID, err)
		_ = d.Store.WithTx(ctx, func(tx store.Tx) error {
			return store.SetMigrationItemState(ctx, tx, item.ID, "failed", strPtr(err.Error()))
		})
	}

	dest, ok := job.FolderMap[item.Folder]
	if !ok {
		dest = suggestedDest(item.Folder)
	}

	sel, err := c.Select(item.Folder, nil).Wait()
	if err != nil {
		fail(fmt.Errorf("select source folder: %w", err))
		return
	}
	_ = sel

	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(item.UID))
	fetch := c.Fetch(uidSet, &imap.FetchOptions{
		UID: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}},
	})
	msg := fetch.Next()
	if msg == nil {
		fail(errors.New("message vanished from source"))
		return
	}
	m, merr := msg.Collect()
	if merr != nil || m == nil {
		fail(errors.New("fetch failed"))
		return
	}
	var raw []byte
	for _, bs := range m.BodySection {
		raw = bs.Bytes
		break
	}
	if len(raw) == 0 {
		fail(errors.New("empty message body from source"))
		return
	}

	rcpt := d.sourceTargetAddr(job)
	res, derr := qd.DeliverInbound(ctx, raw, "migration@"+d.Cfg.PlatformDomain, rcpt, &ingest.AuthVerdicts{SPF: "none", DKIM: "none", DMARC: "none"})
	if derr != nil {
		fail(fmt.Errorf("deliver: %w", derr))
		return
	}

	// Move from INBOX to the mapped destination folder.
	if !equalFold(dest, "INBOX") {
		if err := d.Store.WithTx(ctx, func(tx store.Tx) error {
			var destID uuid.UUID
			if err := tx.QueryRow(ctx, `select id from mail.folder where mailbox_id=$1 and name=$2`,
				res.MailboxID, dest).Scan(&destID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// Create the destination folder on first use.
					if _, e2 := tx.Exec(ctx, `insert into mail.folder (id, mailbox_id, name, special_use, uid_validity)
						values ($1,$2,$3,'', (extract(epoch from now())*1000)::bigint)`,
						uuid.New(), res.MailboxID, dest); e2 != nil {
						return e2
					}
					if err := tx.QueryRow(ctx, `select id from mail.folder where mailbox_id=$1 and name=$2`,
						res.MailboxID, dest).Scan(&destID); err != nil {
						return err
					}
				} else {
					return err
				}
			}
			_, err := store.MoveItem(ctx, tx, res.FolderID, destID, res.UID)
			return err
		}); err != nil {
			fail(fmt.Errorf("move to %s: %w", dest, err))
			return
		}
	}

	_ = d.Store.WithTx(ctx, func(tx store.Tx) error {
		return store.SetMigrationItemState(ctx, tx, item.ID, "migrated", nil)
	})
}

// sourceTargetAddr resolves the job's target mailbox to a deliverable
// address (the mailbox NAME is the RFC 5322 address).
func (d *Deps) sourceTargetAddr(job *store.MigrationJob) string {
	var name string
	if err := d.Store.Pool.QueryRow(context.Background(),
		`select name from mail.mailbox where id=$1`, job.MailboxID).Scan(&name); err != nil {
		return ""
	}
	return name
}

func strPtr(s string) *string { return &s }
