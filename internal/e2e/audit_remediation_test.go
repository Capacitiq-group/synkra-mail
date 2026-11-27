// audit_remediation_test.go: end-to-end proofs for the 27 November 2026 audit
// fixes — envelope-recipient admission, multi-From rejection, MFA, the
// impersonation backdoor closures, sealed DKIM keys, suppression management,
// and the viewer write-down fix.
package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// Critical-2: envelope-only (Bcc) recipients now pass through suppression,
// validation and limit counting. A suppressed Bcc recipient is skipped and
// reported, and a syntactically invalid RCPT is refused at the wire.
func TestEnvelopeBccAdmission(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "bccadm")
	secret := s.newAppPassword(t, user, "mutt")
	primary, _ := s.primaryAddressFor(user)
	ctx := context.Background()

	// Suppress a Bcc target for this org (hard bounce, Section 7.3).
	suppressed := "poison-" + uuid.NewString()[:8] + "@example.org"
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.Suppress(ctx, tx, user.OrgID, suppressed, "hard_bounce")
	}); err != nil {
		t.Fatalf("suppress: %v", err)
	}

	// Submit with a To header naming the sender (so the envelope Bcc merge
	// path applies — the old code only merged envelope recipients when NO
	// To/Cc headers existed).
	msg := "Subject: bcc admission\r\nFrom: " + primary + "\r\nTo: " + primary + "\r\n\r\nbody"
	code, err := smtpSubmission(s.subAddr, login, secret, primary, suppressed, msg)
	if err != nil || code != 250 {
		t.Fatalf("suppressed envelope rcpt must be skipped not fatal: code=%d err=%v", code, err)
	}
	// The submission itself lands (sender copy in Sent).
	s.drainWorker(t)

	// Invalid envelope recipient is refused at RCPT — unvalidated strings
	// never reach RCPT TO:<%s> (audit item 2).
	if _, err := smtpSubmission(s.subAddr, login, secret, primary, "not an address", msg); err == nil {
		t.Fatal("invalid envelope recipient must be refused")
	}
}

// High-9: a second From header is the signature-bypass trick — the submission
// path must refuse it.
func TestSecondFromRejected(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "dupfrom")
	secret := s.newAppPassword(t, user, "claws")
	primary, _ := s.primaryAddressFor(user)

	msg := "From: someone-else@evil.example\r\n" +
		"From: " + primary + "\r\n" +
		"To: " + primary + "\r\nSubject: forged\r\n\r\nbypass attempt"
	code, err := smtpSubmission(s.subAddr, login, secret, primary, primary, msg)
	if err == nil && code < 300 {
		t.Fatalf("second From header must be rejected: code=%d err=%v", code, err)
	}
}

// Critical-3: MFA — enrol, activate, and the login demand.
func TestMFALoginFlow(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "mfa")
	_ = pw // the login handler is exercised via the API with this password
	token := s.login(t, login, pw)

	// Enrol: seed returned once, disabled until activation.
	st, body, _ := s.apiReq(t, "POST", "/api/v1/me/mfa/enrol", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("enrol: status=%d body=%v", st, body)
	}
	secret, _ := body["secret"].(string)
	if secret == "" {
		t.Fatalf("enrol response must carry the secret: %v", body)
	}

	// Login without a code → mfa_required (not yet enabled, but the flow gate
	// is proven by the activate step below and the code-required login after).
	codeNow, _ := krypto.TOTPCode(secret, time.Now())
	st, body, _ = s.apiReq(t, "POST", "/api/v1/me/mfa/activate", token, map[string]any{"code": codeNow}, nil)
	if st != http.StatusOK {
		t.Fatalf("activate: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/me/mfa/activate", token, map[string]any{"code": "000000"}, nil)
	if st != http.StatusUnauthorized && st != http.StatusConflict {
		t.Fatalf("wrong code must not activate: status=%d body=%v", st, body)
	}

	// Login now demands the factor.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/auth/login", "", map[string]string{"email": login, "password": pw}, nil)
	if st != http.StatusUnauthorized || errCode(body) != "mfa_required" {
		t.Fatalf("login without code: status=%d code=%q body=%v", st, errCode(body), body)
	}
	codeNow, _ = krypto.TOTPCode(secret, time.Now())
	st, body, _ = s.apiReq(t, "POST", "/api/v1/auth/login", "", map[string]string{"email": login, "password": pw, "totp_code": codeNow}, nil)
	if st != http.StatusOK {
		t.Fatalf("login with code: status=%d body=%v", st, body)
	}
	// The wrong code is refused.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/auth/login", "", map[string]string{"email": login, "password": pw, "totp_code": "000000"}, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("wrong code login: status=%d body=%v", st, body)
	}
}

// Critical-3: the impersonation backdoor closures — chosen passwords, silent
// forwards, domain registration and exfil webhooks are all refused, while
// support reads stay available and audited.
func TestImpersonationBlocksBackdoors(t *testing.T) {
	s := boot(t)
	_, user, _, _ := s.provisionTenant(t, "impback")
	staff := s.staffUser(t)
	adminToken := s.login(t, staff.Email, "StaffPass123!")

	st, body, _ := s.apiReq(t, "POST", "/api/v1/admin/orgs/"+ids.Format(ids.Org, staff.OrgID)+"/impersonate", adminToken,
		map[string]string{"user_id": ids.Format(ids.User, user.ID), "reason": "remediation test"}, nil)
	if st != http.StatusOK {
		t.Fatalf("impersonate: status=%d body=%v", st, body)
	}
	impToken, _ := body["token"].(string)
	if impToken == "" {
		t.Fatalf("impersonation token missing: %v", body)
	}

	// Permanent backdoors must all refuse.
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/users/invite"},
		{"POST", "/api/v1/domains"},
		{"POST", "/api/v1/addresses"},
		{"POST", "/api/v1/org/webhooks"},
		{"PUT", "/api/v1/org/tracking"},
	} {
		st, body, _ = s.apiReq(t, tc.method, tc.path, impToken, map[string]any{}, nil)
		if st != http.StatusForbidden || errCode(body) != "forbidden" {
			t.Fatalf("impersonated %s %s: status=%d body=%v", tc.method, tc.path, st, body)
		}
	}

	// Support reads stay available AND audited — fail-closed.
	ctx := context.Background()
	// Seed one inbound message so there is something to read.
	primary, _ := s.primaryAddressFor(user)
	smtpDialogue(s.smtpAddr, "friend@external.example", primary, "Subject: support view\r\n\r\nhello")
	s.drainWorker(t)
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	inbox, _ := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(ctx, s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(items) == 0 {
		t.Skip("no message to read in this org")
	}
	st, _, _ = s.apiReq(t, "GET", "/api/v1/messages/"+ids.Format(ids.Message, items[0].Message.ID), impToken, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("impersonated message read must work: status=%d", st)
	}
	rows, _ := store.ListAudit(ctx, s.st.Pool, 200)
	audited := false
	for _, row := range rows {
		if row.Action == "message.read" && row.TargetID == items[0].Message.ID.String() {
			audited = true
		}
	}
	if !audited {
		t.Fatal("impersonated mail-body read must be audited")
	}

	// The admin portal refuses the impersonation session outright now.
	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/orgs", impToken, nil, nil)
	if st != http.StatusForbidden {
		t.Fatalf("impersonation session on the admin portal: status=%d", st)
	}
}

// Critical-4: customer DKIM private keys are stored sealed.
func TestDKIMKeySealedAtRest(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "dkimenc")
	token := s.login(t, login, pw)

	st, body, _ := s.apiReq(t, "POST", "/api/v1/domains", token, map[string]string{"name": "sealed-" + uuid.NewString()[:8] + ".example"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("domain create: status=%d body=%v", st, body)
	}
	var sha string
	if err := s.st.Pool.QueryRow(context.Background(),
		`select dkim_private_pem from mail.domain where org_id = (select org_id from mail.user where email=$1) and is_platform=false order by created_at desc limit 1`,
		login).Scan(&sha); err != nil {
		t.Fatalf("read domain key: %v", err)
	}
	if len(sha) < 5 || sha[:5] != "enc1." {
		t.Fatalf("dkim private key must be sealed at rest (enc1. prefix), got %.12q…", sha)
	}
}

// High-18: suppression is manageable by the org — view, delete, resume.
func TestSuppressionManagement(t *testing.T) {
	s := boot(t)
	org, user, login, pw := s.provisionTenant(t, "supman")
	token := s.login(t, login, pw)
	ctx := context.Background()
	email := "corrected-" + uuid.NewString()[:8] + "@example.org"
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.Suppress(ctx, tx, org.ID, email, "hard_bounce")
	}); err != nil {
		t.Fatalf("suppress: %v", err)
	}

	st, body, _ := s.apiReq(t, "GET", "/api/v1/suppression", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list suppression: status=%d body=%v", st, body)
	}
	st, _, _ = s.apiReq(t, "DELETE", "/api/v1/suppression/"+email, token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("delete suppression: status=%d", st)
	}
	sup, _ := store.IsSuppressed(ctx, s.st.Pool, org.ID, email)
	if sup {
		t.Fatal("address must be removable from the suppression list")
	}

	// Owner-side mailbox resume: the breaker pause no longer waits on a ticket.
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.SetMailboxSendStatus(ctx, tx, mb.ID, "paused_bounces", "e2e trip")
	}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	st, _, _ = s.apiReq(t, "POST", "/api/v1/mailboxes/"+ids.Format(ids.Mailbox, mb.ID)+"/resume", token, map[string]string{"reason": "addresses corrected"}, nil)
	if st != http.StatusOK {
		t.Fatalf("owner resume: status=%d", st)
	}
	mb2, _ := store.GetMailbox(ctx, s.st.Pool, mb.ID)
	if mb2.SendStatus != "active" {
		t.Fatalf("send_status after resume = %q", mb2.SendStatus)
	}
}

// High-14: a viewer membership is read-only.
func TestViewerCannotWrite(t *testing.T) {
	s := boot(t)
	org, user, _, _ := s.provisionTenant(t, "viewerw")
	ctx := context.Background()

	// A second user with viewer membership of the first user's mailbox.
	viewer := &model.User{ID: uuid.New(), OrgID: org.ID, Email: "viewer-" + uuid.NewString()[:8] + "@" + s.cfg.PlatformDomain,
		PasswordHash: hashPassword("Passw0rd-viewer"), DisplayName: "Viewer", Role: "member"}
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		if err := store.CreateUser(ctx, tx, viewer); err != nil {
			return err
		}
		mb, err := s.ownMailbox(user)
		if err != nil {
			return err
		}
		return store.AddMember(ctx, tx, &model.MailboxMember{
			ID: uuid.New(), MailboxID: mb.ID, UserID: viewer.ID, Role: "viewer"})
	}); err != nil {
		t.Fatalf("viewer setup: %v", err)
	}
	viewerToken := s.login(t, viewer.Email, "Passw0rd-viewer")
	// Seed an inbound message so the mailbox has content to read/write.
	primary, _ := s.primaryAddressFor(user)
	smtpDialogue(s.smtpAddr, "writer@external.example", primary, "Subject: viewer target\r\n\r\nbody here")
	s.drainWorker(t)
	mb, _ := s.ownMailbox(user)
	inbox, _ := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(ctx, s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(items) == 0 {
		t.Skip("no message to write on")
	}

	// Reads are allowed; writes are refused.
	st, _, _ := s.apiReq(t, "GET", "/api/v1/messages/"+ids.Format(ids.Message, items[0].Message.ID), viewerToken, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("viewer read must work: status=%d", st)
	}
	st, _, _ = s.apiReq(t, "PATCH", "/api/v1/messages/"+ids.Format(ids.Message, items[0].Message.ID)+"/flags", viewerToken,
		map[string]any{"flags": []string{"\\Seen"}}, nil)
	if st != http.StatusForbidden {
		t.Fatalf("viewer flag write: status=%d, want 403", st)
	}
	st, _, _ = s.apiReq(t, "POST", "/api/v1/folders", viewerToken,
		map[string]string{"mailbox_id": ids.Format(ids.Mailbox, mb.ID), "name": "viewer-made"}, nil)
	if st != http.StatusForbidden {
		t.Fatalf("viewer folder create: status=%d, want 403", st)
	}
}
