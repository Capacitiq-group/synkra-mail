// Phase 3 of the consolidated build register (§3): the janitor sweep, the
// bare-LF wire normalisation (3.7), and the CI-portable harness.
package e2e

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/janitor"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func TestAdminURLFor(t *testing.T) {
	got := adminURLFor("postgres://postgres:pw@db.example:5432/synkra_mail_test?sslmode=disable")
	if want := "postgres://postgres:pw@db.example:5432/postgres?sslmode=disable"; got != want {
		t.Fatalf("adminURLFor = %q, want %q", got, want)
	}
}

// 3.1 — the janitor actually cleans every table it owns.
func TestJanitorSweep(t *testing.T) {
	s := boot(t)
	s.provisionTenant(t, "janitor")
	ctx := context.Background()

	// An expired session, a stale login attempt, and a purged blob with a file.
	var realUserID string
	if err := s.st.Pool.QueryRow(ctx, `select id from mail.user limit 1`).Scan(&realUserID); err != nil {
		t.Fatalf("user: %v", err)
	}
	var sessID uuid.UUID
	if err := s.st.Pool.QueryRow(ctx,
		`insert into mail.session (id, user_id, token_hash, kind, expires_at) values ($1,$2,$3,'web', now() - interval '1 hour') returning id`,
		uuid.New(), realUserID, "janitor-sess-"+uuid.NewString()).Scan(&sessID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := s.st.Pool.Exec(ctx,
		`insert into mail.login_attempt (email, ip, ok, created_at) values ('gone@x.test', '', false, now() - interval '60 days')`); err != nil {
		t.Fatalf("seed attempt: %v", err)
	}
	sha, _, err := s.blobs.Put(ctx, strings.NewReader("purge me"))
	if err != nil {
		t.Fatalf("blob put: %v", err)
	}
	if _, err := s.st.Pool.Exec(ctx,
		`insert into mail.blob (sha256, size_bytes, refcount, purge_after) values ($1, 8, 0, now() - interval '1 day')`, sha); err != nil {
		t.Fatalf("seed blob row: %v", err)
	}

	jan := &janitor.Janitor{Store: s.st, Blobs: s.blobs, Cfg: janitor.Config{
		Interval: time.Hour, LoginAttemptRetentionDays: 30, RateEventRetentionDays: 90,
		IdempotencyRetentionDays: 7, BlobBatch: 100,
	}}
	jan.Tick(ctx)

	var revoked int
	if err := s.st.Pool.QueryRow(ctx,
		`select count(*) from mail.session where id=$1 and revoked_at is not null`, sessID).Scan(&revoked); err != nil {
		t.Fatalf("session scan: %v", err)
	}
	if revoked != 1 {
		t.Fatal("expired session must be marked revoked")
	}
	var attempts int
	if err := s.st.Pool.QueryRow(ctx,
		`select count(*) from mail.login_attempt where email='gone@x.test'`).Scan(&attempts); err != nil {
		t.Fatalf("attempt scan: %v", err)
	}
	if attempts != 0 {
		t.Fatal("stale login attempt must be pruned")
	}
	if _, err := s.blobs.Stat(ctx, sha); err == nil {
		t.Fatal("purged blob file must be deleted")
	}
	var rows int
	if err := s.st.Pool.QueryRow(ctx,
		`select count(*) from mail.blob where sha256=$1`, sha).Scan(&rows); err != nil {
		t.Fatalf("blob scan: %v", err)
	}
	if rows != 0 {
		t.Fatal("purged blob row must be deleted")
	}
}

// 3.7 — bare LF in DATA is normalised to CRLF on both mail paths.
func TestBareLFNormalisationOnTheWire(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "barelf")
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	ctx := context.Background()

	// Inbound MX: body lines with bare LF inside DATA.
	data := "Subject: bare lf inbound\r\n\r\nline one\nline two\n"
	code, err := smtpDialogue(s.smtpAddr, "peer@example.net", primary, data)
	if err != nil || code != 250 {
		t.Fatalf("inbound with bare LF: code=%d err=%v", code, err)
	}
	mb, _ := s.ownMailbox(user)
	inbox, _ := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(ctx, s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(items) != 1 {
		t.Fatalf("inbound items = %d, want 1", len(items))
	}
	raw := storedBlob(t, s, items[0].Message.BlobSHA256)
	assertCRFLOnly(t, raw)

	// Submission: bare LF on the authenticated path too.
	secret := s.newAppPassword(t, user, "barelf")
	subData := "Subject: bare lf submitted\r\n\r\nsub one\nsub two\n"
	code, err = smtpSubmission(s.subAddr, login, secret, primary, primary, subData)
	if err != nil || code != 250 {
		t.Fatalf("submission with bare LF: code=%d err=%v", code, err)
	}
	sent, _ := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb.ID, model.FolderSent)
	sentItems, _ := store.ListItems(ctx, s.st.Pool, sent.ID, 0, 0xffffffff)
	if len(sentItems) != 1 {
		t.Fatalf("sent items = %d, want 1", len(sentItems))
	}
	raw = storedBlob(t, s, sentItems[0].Message.BlobSHA256)
	assertCRFLOnly(t, raw)
}

func storedBlob(t *testing.T, s *stack, sha string) []byte {
	t.Helper()
	rc, err := s.blobs.Open(context.Background(), sha)
	if err != nil {
		t.Fatalf("blob open: %v", err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatalf("blob read: %v", err)
	}
	return buf.Bytes()
}

func assertCRFLOnly(t *testing.T, raw []byte) {
	t.Helper()
	for i, b := range raw {
		if b == '\n' && (i == 0 || raw[i-1] != '\r') {
			t.Fatalf("bare LF stored at offset %d (context %q)", i, contextAround(raw, i))
		}
	}
}

func contextAround(raw []byte, i int) string {
	lo := i - 20
	if lo < 0 {
		lo = 0
	}
	hi := i + 20
	if hi > len(raw) {
		hi = len(raw)
	}
	return string(raw[lo:hi])
}
