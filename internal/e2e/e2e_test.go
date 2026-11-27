// Package e2e boots the full Synkra Email stack in-process (API, inbound MX,
// submission, IMAP, worker, tracker) against a disposable test database and
// exercises the customer-visible flows end to end.
//
// Run: go test ./internal/e2e/ -count=1
// Requires the dev Postgres on 127.0.0.1:5433 (tooling/pg).
package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"synkra.email/mail/internal/api"
	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/db"
	"synkra.email/mail/internal/events"
	"synkra.email/mail/internal/fsblob"
	"synkra.email/mail/internal/imapd"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/outbound"
	"synkra.email/mail/internal/smtpin"
	"synkra.email/mail/internal/store"
	"synkra.email/mail/internal/trackingd"
)

// ---- harness ----

type stack struct {
	cfg        *config.Config
	st         *store.Store
	auth       *authn.Service
	pipeline   *outbound.Pipeline
	deps       *ingest.Deps
	worker     *outbound.Worker
	apiURL     string
	smtpAddr   string
	subAddr    string
	imapAddr   string
	trackerURL string
	blobs      blob.Store
}

var (
	shared     *stack
	sharedOnce sync.Once
	sharedErr  error
)

func testDBURL() string {
	if u := getenv("SYNKRA_MAIL_TEST_URL"); u != "" {
		return u
	}
	return "postgres://synkra@127.0.0.1:5433/synkra_mail_test?sslmode=disable"
}

func boot(t *testing.T) *stack {
	t.Helper()
	sharedOnce.Do(func() {
		shared, sharedErr = bootStack()
	})
	if sharedErr != nil {
		t.Fatalf("boot stack: %v", sharedErr)
	}
	return shared
}

func bootStack() (*stack, error) {
	ctx := context.Background()
	url := testDBURL()
	if err := db.EnsureDatabase(ctx, url); err != nil {
		return nil, err
	}
	// Wipe: fresh database per test run.
	admin, err := sql.Open("pgx", "postgres://synkra@127.0.0.1:5433/postgres?sslmode=disable")
	if err == nil {
		_, _ = admin.Exec(`select pg_terminate_backend(pid) from pg_stat_activity where datname='synkra_mail_test'`)
		_, _ = admin.Exec(`drop database if exists synkra_mail_test`)
		_, _ = admin.Exec(`create database synkra_mail_test`)
		_ = admin.Close()
	}

	pool, err := db.Connect(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, pool, "../../migrations"); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	st := store.New(pool)

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cfg.Env = "dev"
	cfg.DB.URL = url
	cfg.BlobDir = tTemp()
	cfg.WorkerPollInterval = 50 * time.Millisecond

	blobs, err := fsblob.New(cfg.BlobDir)
	if err != nil {
		return nil, err
	}
	var blobStore blob.Store = blobs

	hub := events.NewHub()
	go hub.Run(ctx, url)

	authSvc := &authn.Service{Store: st, Cfg: cfg}
	deps := &ingest.Deps{Store: st, Blobs: blobStore, Cfg: cfg}

	// Listeners on ephemeral ports.
	apiLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mxLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	subLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	imapLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	trackerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	trackerBase := "http://" + trackerLn.Addr().String()

	pipeline := &outbound.Pipeline{
		Store: st, Blobs: blobStore, Cfg: cfg, Deps: deps,
		TrackEnabled: func(ctx context.Context, orgID uuid.UUID) bool {
			org, err := store.GetOrg(ctx, st.Pool, orgID)
			return err == nil && org.TrackingEnabled
		},
		TrackerURL:  trackerBase,
		TrackingKey: []byte(cfg.TrackingSecret),
	}

	// HTTP API
	apiSrv := &api.Server{Cfg: cfg, Auth: authSvc, Store: st, Pipeline: pipeline}
	httpSrv := &http.Server{Handler: apiSrv.Routes()}
	go httpSrv.Serve(apiLn)

	// MX
	mx := smtpin.New(cfg, deps)
	go mx.Serve(mxLn)

	// Submission
	subAuth := &smtpin.AuthAdapter{
		AuthenticateAppPassword: func(ctx context.Context, username, secret, ip string) (string, string, error) {
			u, _, err := authSvc.AuthenticateAppPassword(ctx, username, secret, ip)
			if err != nil {
				return "", "", err
			}
			return u.ID.String(), u.OrgID.String(), nil
		},
	}
	sub := smtpin.NewSubmission(cfg, deps, subAuth)
	sub.QueueSubmission = func(ctx context.Context, userID, orgID string, raw []byte, from string, rcpts []string) error {
		uid, _ := uuid.Parse(userID)
		oid, _ := uuid.Parse(orgID)
		return pipeline.QueueSubmission(ctx, uid, oid, raw, from, rcpts)
	}
	go sub.Serve(subLn)

	// IMAP
	imapSrv := imapd.New(cfg, st, authSvc, blobStore, hub)
	go imapSrv.ServeListener(imapLn)

	// Worker (loopback transport — deterministic, no network)
	w := outbound.NewWorker(pipeline, &outbound.LoopbackTransport{Deps: deps})
	go w.Run(ctx)

	// Tracker (isolated: no DB credentials, posts to core internal endpoint)
	tracker := trackingd.New("http://"+apiLn.Addr().String()+"/api/v1/internal/tracking/events", []byte(cfg.TrackingSecret))
	trackerHTTP := &http.Server{Handler: tracker.Routes()}
	go trackerHTTP.Serve(trackerLn)

	s := &stack{
		cfg: cfg, st: st, auth: authSvc, pipeline: pipeline, deps: deps, worker: w,
		apiURL:   "http://" + apiLn.Addr().String(),
		smtpAddr: mxLn.Addr().String(), subAddr: subLn.Addr().String(),
		imapAddr: imapLn.Addr().String(), trackerURL: trackerBase,
		blobs: blobStore,
	}

	// Seed the platform domain (addresses live on it).
	err = st.WithTx(ctx, func(tx store.Tx) error {
		_, err := store.EnsurePlatformDomain(ctx, tx, cfg.PlatformDomain)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// provisionTenant creates an approved, sending-enabled org with one owner.
func (s *stack) provisionTenant(t *testing.T, name string) (*model.Org, *model.User, string, string) {
	t.Helper()
	ctx := context.Background()
	plan, err := store.GetPlanBySlug(ctx, s.st.Pool, "business")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	login := "owner-" + name + "@" + s.cfg.PlatformDomain
	password := "Passw0rd-" + name
	org, user, err := s.auth.ProvisionOrg(ctx, name, login, "Owner "+name,
		hashPassword(password), s.cfg.PlatformDomain, plan)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	err = s.st.WithTx(ctx, func(tx store.Tx) error {
		if err := store.SetOrgReviewStatus(ctx, tx, org.ID, "approved"); err != nil {
			return err
		}
		if err := store.SetOrgTier(ctx, tx, org.ID, "verified"); err != nil {
			return err
		}
		sub, err := store.GetActiveSubscription(ctx, tx, org.ID)
		if err != nil {
			return err
		}
		now := time.Now().AddDate(0, 1, 0)
		if err := store.SetSubscriptionStatus(ctx, tx, sub.ID, "active", &now); err != nil {
			return err
		}
		return store.SetOrgSending(ctx, tx, org.ID, true, "")
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	return org, user, login, password
}

func (s *stack) login(t *testing.T, email, password string) string {
	t.Helper()
	_, token, err := s.auth.Login(context.Background(), email, password, "127.0.0.1", "e2e-test")
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}
	return token
}

func (s *stack) newAppPassword(t *testing.T, user *model.User, name string) string {
	t.Helper()
	ctx := context.Background()
	org, err := store.GetOrg(ctx, s.st.Pool, user.OrgID)
	if err != nil {
		t.Fatalf("org: %v", err)
	}
	ac := &authn.AuthContext{
		User:    user,
		Org:     org,
		Session: &model.Session{ID: uuid.New(), Kind: "web"},
	}
	secret, _, err := s.auth.CreateAppPassword(ctx, ac, name)
	if err != nil {
		t.Fatalf("app password: %v", err)
	}
	return secret
}

// drainWorker ticks the harness worker until every delivery job has reached a
// terminal state (nothing queued, deferred or in flight).
func (s *stack) drainWorker(t *testing.T) {
	t.Helper()
	s.drainWith(t, s.worker, uuid.Nil)
}

// drainWith ticks w until no delivery job is queued, deferred or in flight;
// mailboxID narrows that to one mailbox's jobs (uuid.Nil: every job). A job
// another worker has claimed still counts until that worker finishes it, so
// state derived on completion (DSNs, the breaker) is settled when this returns.
func (s *stack) drainWith(t *testing.T, w *outbound.Worker, mailboxID uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := w.Tick(ctx); err != nil {
			t.Fatalf("worker tick: %v", err)
		}
		var n int
		var err error
		if mailboxID == uuid.Nil {
			err = s.st.Pool.QueryRow(ctx, `select count(*) from mail.delivery_job where status in ('queued','deferred','in_flight')`).Scan(&n)
		} else {
			err = s.st.Pool.QueryRow(ctx, `select count(*) from mail.delivery_job where mailbox_id=$1 and status in ('queued','deferred','in_flight')`, mailboxID).Scan(&n)
		}
		if err != nil {
			t.Fatalf("queue depth: %v", err)
		}
		if n == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("queue did not drain")
}

// ---- tests ----

func TestSendDeliverLoop(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "loop")
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary address: %v", err)
	}

	in := outbound.ComposeInput{
		From:    mimeutil.Addr{Email: primary},
		To:      []mimeutil.Addr{{Email: primary}},
		Subject: "E2E loop test",
		Text:    "A message that goes out and comes back.",
	}
	_, recipients, err := s.pipeline.ComposeAndQueue(context.Background(), user.ID, user.OrgID, in)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if len(recipients) != 1 || !recipients[0].Queued {
		t.Fatalf("recipients: %+v", recipients)
	}

	s.drainWorker(t)

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
	if len(items) != 1 {
		t.Fatalf("inbox items = %d, want 1", len(items))
	}
	if items[0].Message.Subject != "E2E loop test" {
		t.Fatalf("subject: %q", items[0].Message.Subject)
	}
	sent, err := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderSent)
	if err != nil {
		t.Fatalf("sent: %v", err)
	}
	sentItems, _ := store.ListItems(context.Background(), s.st.Pool, sent.ID, 0, 0xffffffff)
	if len(sentItems) != 1 {
		t.Fatalf("sent items = %d, want 1", len(sentItems))
	}
}

func TestInboundSMTPRouting(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "inbound")
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}

	// Unknown local part → 550.
	code, _ := smtpDialogue(s.smtpAddr, "peer@example.net", "nobody-xyz@"+s.cfg.PlatformDomain, "test")
	if code != 550 {
		t.Fatalf("unknown local: code=%d", code)
	}
	// Unhosted domain → 554 (never a relay — Section 7.4.1).
	code, _ = smtpDialogue(s.smtpAddr, "peer@example.net", "victim@elsewhere.org", "test")
	if code != 554 {
		t.Fatalf("unhosted: code=%d", code)
	}
	// Valid delivery → 250.
	code, err = smtpDialogue(s.smtpAddr, "peer@example.net", primary, "Subject: from the wire\r\n\r\nhello over SMTP")
	if err != nil || code != 250 {
		t.Fatalf("valid: code=%d err=%v", code, err)
	}

	mb, _ := s.ownMailbox(user)
	inbox, _ := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(context.Background(), s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(items) != 1 || items[0].Message.Subject != "from the wire" {
		t.Fatalf("inbox after SMTP: %d items", len(items))
	}
}

func TestSubmissionSMTPWithAppPassword(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "submit")
	secret := s.newAppPassword(t, user, "thunderbird")
	primary, _ := s.primaryAddressFor(user)

	code, err := smtpSubmission(s.subAddr, login, secret, primary, primary, "Subject: submitted\r\n\r\nvia submission port")
	if err != nil || code != 250 {
		t.Fatalf("submission: code=%d err=%v", code, err)
	}
	// The main account password must be refused on protocol ports.
	if _, err := smtpSubmission(s.subAddr, login, "Passw0rd-submit", primary, primary, "Subject: nope\r\n\r\nx"); err == nil {
		t.Fatal("main password must NOT work on submission (app passwords only)")
	}
	s.drainWorker(t)

	mb, _ := s.ownMailbox(user)
	inbox, _ := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(context.Background(), s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(items) != 1 || items[0].Message.Subject != "submitted" {
		t.Fatalf("inbox after submission: %d items", len(items))
	}
}

func TestIMAPSession(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "imap")
	secret := s.newAppPassword(t, user, "apple-mail")
	primary, _ := s.primaryAddressFor(user)

	smtpDialogue(s.smtpAddr, "a@x.test", primary, "Subject: one\r\n\r\nfirst")
	smtpDialogue(s.smtpAddr, "b@x.test", primary, "Subject: two\r\n\r\nsecond")

	c, err := imapclient.DialInsecure(s.imapAddr, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Login(login, secret).Wait(); err != nil {
		t.Fatalf("login with app password: %v", err)
	}
	// The main account password must be refused (Section 6.1).
	c2, err := imapclient.DialInsecure(s.imapAddr, nil)
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer c2.Close()
	if err := c2.Login(login, pw).Wait(); err == nil {
		t.Fatal("main password must NOT work on IMAP (app passwords only)")
	}

	listData, err := c.List("", "*", &imap.ListOptions{}).Collect()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, m := range listData {
		names[m.Mailbox] = true
	}
	for _, want := range []string{"INBOX", "Sent", "Drafts", "Trash", "Spam", "Archive"} {
		if !names[want] {
			t.Fatalf("missing system folder %q in %v", want, names)
		}
	}

	sel, err := c.Select("INBOX", &imap.SelectOptions{}).Wait()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if sel.NumMessages != 2 {
		t.Fatalf("exists = %d, want 2", sel.NumMessages)
	}

	fetch := c.Fetch(imap.SeqSet{{Start: 1, Stop: 2}}, &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		RFC822Size: true,
	})
	var subjects []string
	for {
		data := fetch.Next()
		if data == nil {
			break
		}
		buf, err := data.Collect()
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		if buf.Envelope != nil {
			subjects = append(subjects, buf.Envelope.Subject)
		}
		if buf.RFC822Size == 0 {
			t.Fatal("RFC822Size missing")
		}
	}
	if err := fetch.Close(); err != nil {
		t.Fatalf("fetch close: %v", err)
	}
	if len(subjects) != 2 {
		t.Fatalf("envelopes: %v", subjects)
	}

	// STORE \Seen then SEARCH UNSEEN must exclude it.
	storeCmd := c.Store(imap.SeqSet{{Start: 1, Stop: 1}}, &imap.StoreFlags{
		Op: imap.StoreFlagsSet, Flags: []imap.Flag{imap.FlagSeen},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Debug: read flags back and check the DB.
	flagFetch := c.Fetch(imap.SeqSet{{Start: 1, Stop: 1}}, &imap.FetchOptions{Flags: true})
	for {
		d := flagFetch.Next()
		if d == nil {
			break
		}
		buf, err := d.Collect()
		if err == nil {
			t.Logf("flags after store: %v", buf.Flags)
		}
	}
	flagFetch.Close()
	var dbFlags []string
	var itemUID int
	if err := s.st.Pool.QueryRow(context.Background(),
		`select i.uid, i.flags from mail.mailbox_item i
                 join mail.folder f on f.id = i.folder_id
                 join mail.mailbox mb on mb.id = f.mailbox_id
                 join mail.user u on u.id = mb.owner_user_id
                 where u.email = $1 and f.name='INBOX' and i.uid = 1`, login).Scan(&itemUID, &dbFlags); err == nil {
		t.Logf("db flags uid=1: %v", dbFlags)
	} else {
		t.Logf("db flags query: %v", err)
	}
	search, err := c.Search(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Old-style * SEARCH responses populate All (Count is ESEARCH-only).
	seqSetOK, ok := search.All.(imap.SeqSet)
	if !ok || len(seqSetOK) == 0 {
		t.Fatalf("search \\Seen = %v, want one match", search.All)
	}

	// BODY[] fetch must return the raw message.
	fetch2 := c.Fetch(imap.SeqSet{{Start: 1, Stop: 1}}, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{{}},
	})
	var gotBody []byte
	for {
		d := fetch2.Next()
		if d == nil {
			break
		}
		buf, err := d.Collect()
		if err != nil {
			t.Fatalf("collect2: %v", err)
		}
		for _, bs := range buf.BodySection {
			gotBody = bs.Bytes
		}
	}
	if err := fetch2.Close(); err != nil {
		t.Fatalf("fetch2: %v", err)
	}
	if !bytes.Contains(gotBody, []byte("Subject: one")) {
		t.Fatalf("body section wrong: %q", string(gotBody[:min(60, len(gotBody))]))
	}

	// APPEND to Drafts (UIDPLUS APPENDUID).
	appendCmd := c.Append("Drafts", int64(len("Subject: drafted\r\n\r\nbody")), &imap.AppendOptions{})
	appendCmd.Write([]byte("Subject: drafted\r\n\r\nbody"))
	if err := appendCmd.Close(); err != nil {
		t.Fatalf("append write: %v", err)
	}
	appData, err := appendCmd.Wait()
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if appData.UID == 0 {
		t.Fatal("APPENDUID missing")
	}

	// COPY 2 → Archive, then MOVE 1 → Trash.
	if _, err := c.Copy(imap.SeqSet{{Start: 2, Stop: 2}}, "Archive").Wait(); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if _, err := c.Move(imap.SeqSet{{Start: 1, Stop: 1}}, "Trash").Wait(); err != nil {
		t.Fatalf("move: %v", err)
	}

	selTrash, err := c.Select("Trash", &imap.SelectOptions{}).Wait()
	if err != nil || selTrash.NumMessages != 1 {
		t.Fatalf("trash select: err=%v n=%d", err, selTrash.NumMessages)
	}
	st2 := c.Store(imap.SeqSet{{Start: 1, Stop: 1}}, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := st2.Close(); err != nil {
		t.Fatalf("store deleted: %v", err)
	}
	if err := c.Expunge().Close(); err != nil {
		t.Fatalf("expunge: %v", err)
	}
	selTrash2, err := c.Select("Trash", &imap.SelectOptions{}).Wait()
	if err != nil || selTrash2.NumMessages != 0 {
		t.Fatalf("trash after expunge: err=%v n=%d", err, selTrash2.NumMessages)
	}
}

func TestIMAPIdleWakeOnDelivery(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "idle")
	secret := s.newAppPassword(t, user, "idle-client")
	primary, _ := s.primaryAddressFor(user)

	c, err := imapclient.DialInsecure(s.imapAddr, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.Login(login, secret).Wait(); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := c.Select("INBOX", &imap.SelectOptions{}).Wait(); err != nil {
		t.Fatalf("select: %v", err)
	}
	idleCmd, err := c.Idle()
	if err != nil {
		t.Fatalf("idle: %v", err)
	}

	// Deliver while the client is idling.
	time.Sleep(300 * time.Millisecond)
	if _, err := smtpDialogue(s.smtpAddr, "wake@x.test", primary, "Subject: wake up\r\n\r\nping"); err != nil {
		t.Fatalf("smtp: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = idleCmd.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("IDLE close timed out")
	}
}

func TestQuotaGraceAnd552(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "quota")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()

	// Shrink the org pool to ~1 MB so the grace buffer is reachable fast.
	plan, _ := store.GetPlanByID(ctx, s.st.Pool, org.PlanID)
	if _, err := s.st.Pool.Exec(ctx, `update mail.plan set storage_total_mb=1 where id=$1`, plan.ID); err != nil {
		t.Fatalf("shrink plan: %v", err)
	}

	// 1 MB included + 10% grace ≈ 1.1 MB. Deliver ~300 KB at a time until 552.
	// Body uses RFC-compliant line lengths (servers enforce the 1000-octet rule).
	longLine := strings.Repeat("x", 900) + "\r\n"
	bigBody := strings.Repeat(longLine, 340) // ~306 KB
	rejected := false
	for i := 0; i < 12 && !rejected; i++ {
		code, derr := smtpDialogue(s.smtpAddr, "big@x.test", primary, "Subject: big "+fmt.Sprint(i)+"\r\nContent-Type: text/plain\r\n\r\n"+bigBody)
		t.Logf("iter %d: code=%d err=%v", i, code, derr)
		if code == 552 {
			rejected = true
			break
		}
		if code != 250 {
			t.Fatalf("unexpected code at %d: %d (err=%v)", i, code, derr)
		}
	}
	if !rejected {
		t.Fatal("expected 552 mailbox-full beyond the grace buffer")
	}

	// Storage API state must report beyond_grace.
	state, err := (&ingest.Deps{Store: s.st, Blobs: s.blobs, Cfg: s.cfg}).CheckQuota(ctx, org.ID)
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if !state.BeyondGrace {
		t.Fatalf("expected beyond_grace, got %+v", state)
	}
}

func TestNewOrgSendingLimits(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "limits")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()

	// Downgrade to the new-org tier (Section 7.3: 20/hour/mailbox).
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.SetOrgTier(ctx, tx, org.ID, "new")
	}); err != nil {
		t.Fatalf("tier: %v", err)
	}

	sent := 0
	var lastErr error
	for i := 0; i < 25; i++ {
		in := outbound.ComposeInput{
			From:    mimeutil.Addr{Email: primary},
			To:      []mimeutil.Addr{{Email: fmt.Sprintf("r%d@outside.test", i)}},
			Subject: "limit test",
			Text:    "x",
		}
		if _, _, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in); err != nil {
			lastErr = err
			break
		}
		sent++
	}
	if sent != s.cfg.Limits.NewMailboxHourly {
		t.Fatalf("sent %d before limit, want %d (err=%v)", sent, s.cfg.Limits.NewMailboxHourly, lastErr)
	}
	if lastErr == nil || !strings.Contains(lastErr.Error(), "limit") {
		t.Fatalf("expected limit error, got %v", lastErr)
	}
}

func TestBounceBreakerAndSuppression(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "breaker")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()

	breakerTransport := &staticTransport{code: 550, msg: "5.1.1 user unknown"}
	w := outbound.NewWorker(s.pipeline, breakerTransport)

	for i := 0; i < s.cfg.Breaker.MinAttempts; i++ {
		in := outbound.ComposeInput{
			From:    mimeutil.Addr{Email: primary},
			To:      []mimeutil.Addr{{Email: fmt.Sprintf("dead%d@nowhere.test", i)}},
			Subject: fmt.Sprintf("bounce %d", i),
			Text:    "x",
		}
		if _, _, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in); err != nil {
			t.Fatalf("compose %d: %v", i, err)
		}
	}
	// The harness's background worker polls every 50 ms and may claim some of
	// these jobs while they are being composed, then still be mid-delivery when
	// Tick returns. The breaker counts completed attempts only, so wait for all
	// of them to finish before reading the mailbox. Either worker's transport
	// is a hard bounce here: the test worker answers 550, the loopback 554.
	mb, _ := s.ownMailbox(user)
	s.drainWith(t, w, mb.ID)

	fresh, _ := store.GetMailbox(ctx, s.st.Pool, mb.ID)
	if fresh.SendStatus != "paused_bounces" {
		t.Fatalf("mailbox send_status = %q, want paused_bounces", fresh.SendStatus)
	}
	sup, err := store.IsSuppressed(ctx, s.st.Pool, org.ID, "dead0@nowhere.test")
	if err != nil || !sup {
		t.Fatalf("suppression missing: %v %v", sup, err)
	}
	in := outbound.ComposeInput{
		From: mimeutil.Addr{Email: primary}, To: []mimeutil.Addr{{Email: "x@outside.test"}},
		Subject: "nope", Text: "x",
	}
	if _, _, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in); err == nil {
		t.Fatal("send must fail while mailbox paused_bounces")
	}
	// A bounce notice (DSN) should have landed in the sender's INBOX.
	inbox, _ := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(ctx, s.st.Pool, inbox.ID, 0, 0xffffffff)
	foundDSN := false
	for _, it := range items {
		if strings.Contains(it.Message.Subject, "Undelivered Mail") {
			foundDSN = true
		}
	}
	if !foundDSN {
		t.Fatalf("no bounce DSN in inbox (%d items)", len(items))
	}
}

func TestImpersonationGuardrails(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "impers")

	// Staff user lives in the Platform org (created on demand).
	ctx := context.Background()
	plan, _ := store.GetPlanBySlug(ctx, s.st.Pool, "business")
	var staffOrgID uuid.UUID
	err := s.st.Pool.QueryRow(ctx, `select id from mail.org where name='Platform' limit 1`).Scan(&staffOrgID)
	if err != nil {
		err = s.st.WithTx(ctx, func(tx store.Tx) error {
			staffOrgID = uuid.New()
			if _, err := tx.Exec(ctx, `insert into mail.org (id, name, plan_id, review_status, sending_enabled)
                                values ($1,'Platform',$2,'approved',true)`, staffOrgID, plan.ID); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatalf("staff org: %v", err)
		}
	}
	admin := &model.User{ID: uuid.New(), OrgID: staffOrgID, Email: "staff-" + uuid.NewString()[:8] + "@synkra.co.za",
		PasswordHash: hashPassword("StaffPass123!"), DisplayName: "Staff", Role: "owner", IsPlatformAdmin: true}
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.CreateUser(ctx, tx, admin)
	}); err != nil {
		t.Fatalf("staff user: %v", err)
	}

	token, err := s.auth.Impersonate(ctx, admin, user.ID, "customer ticket #42", "127.0.0.1", "admin-ui")
	if err != nil {
		t.Fatalf("impersonate: %v", err)
	}
	ac, err := s.auth.VerifyToken(ctx, token)
	if err != nil {
		t.Fatalf("verify impersonation token: %v", err)
	}
	if !ac.Impersonating() {
		t.Fatal("session kind should be impersonation")
	}
	// Guardrail: no credential changes from impersonated sessions (Section 11.2).
	_, _, err = s.auth.CreateAppPassword(ctx, ac, "nope")
	if err == nil {
		t.Fatal("impersonated session must not create app passwords")
	}
	// Audit trail has the start event with the reason.
	rows, _ := store.ListAudit(ctx, s.st.Pool, 50)
	found := false
	for _, r := range rows {
		if r.Action == "impersonation.start" && r.Reason == "customer ticket #42" {
			found = true
		}
	}
	if !found {
		t.Fatal("impersonation.start audit missing")
	}
}

func TestTrackingFlow(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "track")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()

	// Enable the org toggle (owner decision: off by default).
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		_, err := tx.Exec(ctx, `update mail.org set tracking_enabled=true where id=$1`, org.ID)
		return err
	}); err != nil {
		t.Fatalf("tracking toggle: %v", err)
	}

	in := outbound.ComposeInput{
		From:    mimeutil.Addr{Email: primary},
		To:      []mimeutil.Addr{{Email: "reader@outside.test"}},
		Subject: "tracked message",
		Text:    "see link",
		HTML:    `<p>Click <a href="https://example.com/landing">here</a></p>`,
		Track:   true,
	}
	msgID, _, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in)
	if err != nil {
		t.Fatalf("compose tracked: %v", err)
	}
	full, err := store.GetMessage(ctx, s.st.Pool, msgID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	rc, _ := s.blobs.Open(ctx, full.Message.BlobSHA256)
	raw, _ := io.ReadAll(rc)
	rc.Close()
	rawStr := string(raw)
	if !strings.Contains(rawStr, s.trackerURL+"/o/") {
		t.Fatal("pixel URL missing from tracked message")
	}
	if !strings.Contains(rawStr, s.trackerURL+"/c/") {
		t.Fatal("click URL missing from tracked message")
	}

	// Load the pixel → open event flows through the isolated tracker.
	pi := strings.Index(rawStr, "/o/")
	pixelURL := s.trackerURL + rawStr[pi:pi+strings.Index(rawStr[pi:], `"`)]
	resp, err := http.Get(pixelURL)
	if err != nil {
		t.Fatalf("pixel GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !bytes.HasPrefix(body, []byte("GIF8")) {
		t.Fatalf("pixel response: %d %q", resp.StatusCode, body[:min(10, len(body))])
	}
	time.Sleep(500 * time.Millisecond)
	v, err := store.TrackingEventsForMessage(ctx, s.st.Pool, msgID)
	if err != nil {
		t.Fatalf("tracking view: %v", err)
	}
	if len(v.OpenEvents) == 0 {
		t.Fatal("open_event missing after pixel load")
	}
	switch v.OpenEvents[0].Confidence {
	case "opened", "likely_opened", "possibly_automated":
	default:
		t.Fatalf("bad confidence label %q", v.OpenEvents[0].Confidence)
	}

	// Load the click link → 302 to the real target + click event.
	ci := strings.Index(rawStr, "/c/")
	endQuote := strings.Index(rawStr[ci:], `"`)
	linkURL := s.trackerURL + rawStr[ci:ci+endQuote]
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp2, err := client.Get(linkURL)
	if err != nil {
		t.Fatalf("click GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 302 {
		t.Fatalf("click redirect status = %d", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != "https://example.com/landing" {
		t.Fatalf("redirect target = %q", loc)
	}
	time.Sleep(500 * time.Millisecond)
	v2, _ := store.TrackingEventsForMessage(ctx, s.st.Pool, msgID)
	if len(v2.Clicks) == 0 {
		t.Fatal("click_event missing after redirect")
	}
}

func TestTrackingOffByDefault(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "notrack")
	primary, _ := s.primaryAddressFor(user)
	in := outbound.ComposeInput{
		From: mimeutil.Addr{Email: primary}, To: []mimeutil.Addr{{Email: "x@outside.test"}},
		Subject: "plain", Text: "no tracking",
		HTML:  `<a href="https://a.b/">go</a>`,
		Track: true, // per-message opt-in, but the org toggle is OFF
	}
	msgID, _, err := s.pipeline.ComposeAndQueue(context.Background(), user.ID, user.OrgID, in)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	full, _ := store.GetMessage(context.Background(), s.st.Pool, msgID)
	rc, _ := s.blobs.Open(context.Background(), full.Message.BlobSHA256)
	raw, _ := io.ReadAll(rc)
	if strings.Contains(string(raw), "/o/") || strings.Contains(string(raw), "/c/") {
		t.Fatal("tracking URLs present though org toggle is off (must default off)")
	}
}

func TestTrialBlocksSending(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "trial")
	primary, _ := s.primaryAddressFor(user)
	// provisionTenant enables sending; revert to the trial state (no payment, no approval).
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.SetOrgSending(ctx, tx, user.OrgID, false, "payment_required")
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	in := outbound.ComposeInput{
		From: mimeutil.Addr{Email: primary}, To: []mimeutil.Addr{{Email: "x@outside.test"}},
		Subject: "trial", Text: "x",
	}
	_, _, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in)
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("trial org sending must be blocked, got: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
