// Pipeline admission semantics at the e2e level: a suppressed recipient is
// skipped and reported, malformed input rejects the whole message, oversize
// input is refused before anything is stored, tracking names its sole
// recipient, and SMTP submission files exactly what the client sent.
package e2e

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/outbound"
	"synkra.email/mail/internal/store"
)

// storedSentRaw fetches the raw bytes of a stored message.
func (s *stack) storedSentRaw(t *testing.T, msgID uuid.UUID) string {
	t.Helper()
	full, err := store.GetMessage(context.Background(), s.st.Pool, msgID)
	if err != nil {
		t.Fatalf("stored message: %v", err)
	}
	rc, err := s.blobs.Open(context.Background(), full.Message.BlobSHA256)
	if err != nil {
		t.Fatalf("stored blob: %v", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stored blob: %v", err)
	}
	return string(raw)
}

func TestSuppressedRecipientIsSkippedNotFatal(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "supskip")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.Suppress(ctx, tx, org.ID, "gone@nowhere.test", "hard_bounce")
	}); err != nil {
		t.Fatalf("suppress: %v", err)
	}
	in := outbound.ComposeInput{
		From:    mimeutil.Addr{Email: primary},
		To:      []mimeutil.Addr{{Email: primary}, {Email: "gone@nowhere.test"}},
		Subject: "one good one held",
		Text:    "x",
	}
	msgID, views, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in)
	if err != nil {
		t.Fatalf("compose with a suppressed recipient must succeed: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views: %+v", views)
	}
	for _, v := range views {
		if v.Email == "gone@nowhere.test" && (!v.Suppressed || v.Queued) {
			t.Fatalf("suppressed view wrong: %+v", v)
		}
	}
	raw := s.storedSentRaw(t, msgID)
	if strings.Contains(raw, "gone@nowhere.test") {
		t.Fatal("suppressed address leaked into the message headers")
	}
	s.drainWorker(t)
}

func TestMalformedRecipientRejectsTheWholeMessage(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "malformed")
	primary, _ := s.primaryAddressFor(user)
	in := outbound.ComposeInput{
		From:    mimeutil.Addr{Email: primary},
		To:      []mimeutil.Addr{{Email: "not an address"}},
		Subject: "bad",
		Text:    "x",
	}
	_, _, err := s.pipeline.ComposeAndQueue(context.Background(), user.ID, user.OrgID, in)
	if !errors.Is(err, outbound.ErrInvalidAddress) {
		t.Fatalf("want ErrInvalidAddress, got: %v", err)
	}
}

func TestOversizeMessageIsRejectedBeforeAnythingIsStored(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "oversize")
	primary, _ := s.primaryAddressFor(user)
	orig := s.pipeline.Cfg.MaxMessageBytes
	s.pipeline.Cfg.MaxMessageBytes = 2000
	defer func() { s.pipeline.Cfg.MaxMessageBytes = orig }()
	in := outbound.ComposeInput{
		From:    mimeutil.Addr{Email: primary},
		To:      []mimeutil.Addr{{Email: "x@outside.test"}},
		Subject: "big",
		Text:    strings.Repeat("A", 5000),
	}
	_, _, err := s.pipeline.ComposeAndQueue(context.Background(), user.ID, user.OrgID, in)
	if !errors.Is(err, outbound.ErrMessageTooLarge) {
		t.Fatalf("want ErrMessageTooLarge, got: %v", err)
	}
}

func TestTrackedMessageNamesItsSoleRecipient(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "pixelname")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		_, err := tx.Exec(ctx, `update mail.org set tracking_enabled=true where id=$1`, org.ID)
		return err
	}); err != nil {
		t.Fatalf("tracking toggle: %v", err)
	}
	in := outbound.ComposeInput{
		From:    mimeutil.Addr{Email: primary},
		To:      []mimeutil.Addr{{Email: "solo@outside.test"}},
		Subject: "attributed pixel",
		Text:    "x",
		Track:   true,
	}
	msgID, _, err := s.pipeline.ComposeAndQueue(ctx, user.ID, user.OrgID, in)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	var recipient string
	if err := s.st.Pool.QueryRow(ctx, `select recipient from mail.tracking_pixel where message_id=$1`, msgID).Scan(&recipient); err != nil {
		t.Fatalf("pixel row: %v", err)
	}
	if recipient != "solo@outside.test" {
		t.Fatalf("pixel recipient = %q, want solo@outside.test", recipient)
	}
	s.drainWorker(t)
}

func TestSubmissionFilesWhatTheClientSent(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "filed")
	secret := s.newAppPassword(t, user, "fidelity")
	primary, _ := s.primaryAddressFor(user)
	raw := "From: " + primary + "\r\nSubject: filed as sent\r\n\r\nno message-id, no date, no to header\r\n"
	code, err := smtpSubmission(s.subAddr, login, secret, primary, primary, raw)
	if err != nil || code != 250 {
		t.Fatalf("submission: code=%d err=%v", code, err)
	}
	mb, _ := s.ownMailbox(user)
	sent, _ := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderSent)
	items, _ := store.ListItems(context.Background(), s.st.Pool, sent.ID, 0, 0xffffffff)
	if len(items) != 1 {
		t.Fatalf("sent items = %d, want 1", len(items))
	}
	m := items[0].Message
	if m.MessageID == "" || !strings.HasPrefix(m.MessageID, "<") {
		t.Fatalf("message-id not minted: %q", m.MessageID)
	}
	stored := s.storedSentRaw(t, m.ID)
	if !strings.Contains(stored, "Message-ID:") || !strings.Contains(stored, "Date:") {
		t.Fatal("Message-ID/Date not supplied to the submitted message")
	}
	if len(m.ToAddrs) != 0 {
		t.Fatalf("envelope-only recipients must not be filed as To: %+v", m.ToAddrs)
	}
	s.drainWorker(t)
}
