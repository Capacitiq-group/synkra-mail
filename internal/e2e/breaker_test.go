package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/outbound"
	"synkra.email/mail/internal/store"
)

// Two workers hard-bouncing for the same mailbox at the same time must count
// their attempts one after the other. The breaker reads the DSN count inside
// the bouncing transaction, so without the mailbox row lock each worker would
// under READ COMMITTED see one attempt too few and both would skip a trip.
func TestHardBounceAccountingIsSerialisedPerMailbox(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "serial")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()

	// Delivery jobs reference a message row. Borrow one from another tenant so
	// that its own delivery never enters this mailbox's bounce accounting.
	_, sender, _, _ := s.provisionTenant(t, "serialsrc")
	senderAddr, _ := s.primaryAddressFor(sender)
	if _, _, err := s.pipeline.ComposeAndQueue(ctx, sender.ID, sender.OrgID, outbound.ComposeInput{
		From: mimeutil.Addr{Email: senderAddr}, To: []mimeutil.Addr{{Email: senderAddr}},
		Subject: "fixture", Text: "x",
	}); err != nil {
		t.Fatalf("compose fixture: %v", err)
	}
	var msgID uuid.UUID
	if err := s.st.Pool.QueryRow(ctx, `select message_id from mail.delivery_job where org_id=$1 limit 1`, sender.OrgID).Scan(&msgID); err != nil {
		t.Fatalf("fixture message: %v", err)
	}

	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	// Two already-bounced jobs: terminal, so the harness's background worker
	// never claims them, and nothing else writes a DSN for this mailbox.
	jobs := make([]uuid.UUID, 2)
	for i := range jobs {
		jobs[i] = uuid.New()
		_, err := s.st.Pool.Exec(ctx, `insert into mail.delivery_job
			(id, org_id, mailbox_id, message_id, envelope_from, envelope_to, recipient_domain, status)
			values ($1,$2,$3,$4,$5,$6,'nowhere.test','bounced')`,
			jobs[i], org.ID, mb.ID, msgID, primary, fmt.Sprintf("serial%d@nowhere.test", i))
		if err != nil {
			t.Fatalf("job %d: %v", i, err)
		}
	}

	hard := true
	bounce := func(job uuid.UUID) *model.DSN {
		return &model.DSN{ID: uuid.New(), DeliveryJobID: job, Kind: "bounce", IsHard: &hard,
			SMTPCode: "550", Reason: "5.1.1 user unknown"}
	}

	// Worker A records the first hard bounce and holds its transaction open.
	txA, err := s.st.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	defer txA.Rollback(ctx)
	if err := store.LockMailboxRow(ctx, txA, mb.ID); err != nil {
		t.Fatalf("lock A: %v", err)
	}
	if err := store.RecordDSN(ctx, txA, bounce(jobs[0])); err != nil {
		t.Fatalf("record A: %v", err)
	}

	// Worker B starts the second hard bounce while A is still open.
	type outcome struct {
		attempts int
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		txB, err := s.st.Pool.Begin(ctx)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer txB.Rollback(ctx)
		if err := store.LockMailboxRow(ctx, txB, mb.ID); err != nil {
			done <- outcome{err: err}
			return
		}
		if err := store.RecordDSN(ctx, txB, bounce(jobs[1])); err != nil {
			done <- outcome{err: err}
			return
		}
		attempts, _, _, err := store.BounceRatePct(ctx, txB, mb.ID, time.Hour)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		if err := txB.Commit(ctx); err != nil {
			done <- outcome{err: err}
			return
		}
		done <- outcome{attempts: attempts}
	}()

	// Plenty of time for B to run to completion if nothing holds it back.
	time.Sleep(300 * time.Millisecond)
	seenByA, _, _, err := store.BounceRatePct(ctx, txA, mb.ID, time.Hour)
	if err != nil {
		t.Fatalf("count A: %v", err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit A: %v", err)
	}

	var res outcome
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker B never acquired the mailbox lock")
	}
	if res.err != nil {
		t.Fatalf("worker B: %v", res.err)
	}
	if seenByA != 1 || res.attempts != 2 {
		t.Fatalf("completed attempts seen: worker A=%d, worker B=%d; want A=1 and B=2 (B waits for A, then counts both)",
			seenByA, res.attempts)
	}
}
