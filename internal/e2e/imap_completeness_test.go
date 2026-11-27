// Phase 4 of the consolidated build register (§5): IMAP completeness —
// APPEND quota/oversize (5.1), folder semantics incl. \HasChildren (5.2),
// per-user connection cap enforcement (5.3).
package e2e

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"

	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func imapLogin(t *testing.T, s *stack, login, secret string) *imapclient.Client {
	t.Helper()
	c, err := imapclient.DialInsecure(s.imapAddr, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Login(login, secret).Wait(); err != nil {
		t.Fatalf("login: %v", err)
	}
	return c
}

func imapAppend(t *testing.T, c *imapclient.Client, mailbox string, payload []byte) error {
	t.Helper()
	cmd := c.Append(mailbox, int64(len(payload)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}})
	if _, err := cmd.Write(payload); err != nil {
		t.Fatalf("append write: %v", err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatalf("append close: %v", err)
	}
	_, err := cmd.Wait()
	return err
}

func imapInboxCount(t *testing.T, s *stack, user *model.User) int {
	t.Helper()
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	inbox, err := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderInbox)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	items, err := store.ListItems(context.Background(), s.st.Pool, inbox.ID, 0, 0xffffffff)
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	return len(items)
}

// 5.1 — APPEND rejects oversize instead of silently truncating a message.
func TestAppendOversizeRejected(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "bigapp")
	secret := s.newAppPassword(t, user, "migrator")
	c := imapLogin(t, s, login, secret)
	defer c.Close()

	oversize := make([]byte, s.cfg.MaxMessageBytes+1024)
	copy(oversize, "Subject: too big\r\n\r\n")
	if err := imapAppend(t, c, "INBOX", oversize); err == nil {
		t.Fatal("oversize APPEND must be rejected")
	}
	if n := imapInboxCount(t, s, user); n != 0 {
		t.Fatalf("oversize APPEND must store nothing, found %d items", n)
	}
	// A normal APPEND still works.
	if err := imapAppend(t, c, "INBOX", []byte("Subject: small\r\n\r\nok")); err != nil {
		t.Fatalf("normal APPEND broken: %v", err)
	}
	if n := imapInboxCount(t, s, user); n != 1 {
		t.Fatalf("normal APPEND lost: %d items", n)
	}
}

// 5.1 — APPEND obeys the org storage quota like the send path does.
func TestAppendRespectsQuota(t *testing.T) {
	s := boot(t)
	org, user, login, _ := s.provisionTenant(t, "appquota")
	secret := s.newAppPassword(t, user, "migrator")
	c := imapLogin(t, s, login, secret)
	defer c.Close()

	// A dedicated 1 MB plan so the quota is reachable without moving gigabytes.
	planID := uuid.New()
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		if _, err := tx.Exec(ctx, `insert into mail.plan (id, slug, name, price_cents, currency, "interval",
			mailbox_limit, alias_limit, shared_mailbox_limit, storage_total_mb, storage_overage_cents_per_gb)
			values ($1,'tiny-test','Tiny',100,'ZAR','month',100,100,100,1,0) on conflict (slug) do nothing`, planID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update mail.org set plan_id=$2 where id=$1`, org.ID, planID)
		return err
	}); err != nil {
		t.Fatalf("tiny plan: %v", err)
	}

	big := bytes.Repeat([]byte("a"), 2*1024*1024)
	copy(big, "Subject: big append\r\n\r\n")
	// Pre-store check passes (nothing used yet); the 2 MB lands and busts the 1 MB plan.
	if err := imapAppend(t, c, "INBOX", big); err != nil {
		t.Fatalf("first append must land (quota checked before store): %v", err)
	}
	if err := imapAppend(t, c, "INBOX", []byte("Subject: one more\r\n\r\nx")); err == nil {
		t.Fatal("APPEND beyond quota must be rejected like the send path")
	}
}

// 5.2 — CREATE creates intermediate levels; DELETE refuses parents;
// RENAME moves children; LIST reports \HasChildren.
func TestFolderSemantics(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "folders")
	secret := s.newAppPassword(t, user, "thunderbird")
	c := imapLogin(t, s, login, secret)
	defer c.Close()

	if err := c.Create("Work", nil).Wait(); err != nil {
		t.Fatalf("create Work: %v", err)
	}
	// Intermediate "Projects" must be created automatically.
	if err := c.Create("Projects/2026", nil).Wait(); err != nil {
		t.Fatalf("create Projects/2026: %v", err)
	}
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	for _, name := range []string{"Projects", "Projects/2026"} {
		if _, err := store.GetFolderByName(context.Background(), s.st.Pool, mb.ID, name); err != nil {
			t.Fatalf("intermediate folder %q missing", name)
		}
	}

	// A parent with children refuses DELETE.
	if err := c.Delete("Projects").Wait(); err == nil {
		t.Fatal("DELETE of a folder with children must be refused")
	}

	// RENAME moves children with the parent.
	if err := c.Rename("Work", "Business", nil).Wait(); err != nil {
		t.Fatalf("rename Work: %v", err)
	}
	if _, err := store.GetFolderByName(context.Background(), s.st.Pool, mb.ID, "Business"); err != nil {
		t.Fatal("renamed folder missing")
	}
	if err := c.Create("Business/2026", nil).Wait(); err != nil {
		t.Fatalf("create child under renamed parent: %v", err)
	}
	if err := c.Rename("Business", "Corporate", nil).Wait(); err != nil {
		t.Fatalf("rename with children: %v", err)
	}
	if _, err := store.GetFolderByName(context.Background(), s.st.Pool, mb.ID, "Corporate/2026"); err != nil {
		t.Fatal("child did not follow the rename")
	}
	if err := c.Rename("INBOX", "Nope", nil).Wait(); err == nil {
		t.Fatal("INBOX must not be renamable")
	}

	// LIST reports \HasChildren / \HasNoChildren.
	list, err := c.List("", "*", &imap.ListOptions{}).Collect()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	attrs := map[string]imap.MailboxAttr{}
	for _, data := range list {
		attrs[data.Mailbox] = mailAttr(data.Attrs)
	}
	if !containsAttr(attrs["Corporate"], imap.MailboxAttrHasChildren) {
		t.Fatalf("Corporate must report \\HasChildren: %v", attrs["Corporate"])
	}
	if !containsAttr(attrs["Corporate/2026"], imap.MailboxAttrHasNoChildren) {
		t.Fatalf("Corporate/2026 must report \\HasNoChildren: %v", attrs["Corporate/2026"])
	}
}

func mailAttr(attrs []imap.MailboxAttr) imap.MailboxAttr {
	out := ""
	for _, a := range attrs {
		out += string(a) + " "
	}
	return imap.MailboxAttr(out)
}

func containsAttr(joined, want imap.MailboxAttr) bool {
	return strings.Contains(string(joined), string(want))
}

// 5.3 — the per-user IMAP connection cap is enforced.
func TestPerUserConnectionCap(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "conncap")
	secret := s.newAppPassword(t, user, "client")

	orig := s.cfg.HTTP.IMAPMaxConnUser
	s.cfg.HTTP.IMAPMaxConnUser = 2
	defer func() { s.cfg.HTTP.IMAPMaxConnUser = orig }()

	c1 := imapLogin(t, s, login, secret)
	defer c1.Close()
	c2 := imapLogin(t, s, login, secret)
	defer c2.Close()

	c3, err := imapclient.DialInsecure(s.imapAddr, nil)
	if err != nil {
		t.Fatalf("dial3: %v", err)
	}
	defer c3.Close()
	if err := c3.Login(login, secret).Wait(); err == nil {
		t.Fatal("third connection must be refused at the cap")
	}
	// Releasing one slot lets a new client in (give the server-side close a
	// beat to release the cap slot).
	c2.Logout().Wait()
	time.Sleep(150 * time.Millisecond)
	c4 := imapLogin(t, s, login, secret)
	defer c4.Close()
}
