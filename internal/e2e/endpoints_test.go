// Phase 2 of the consolidated build register (§2): the store capabilities that
// had no route — regression tests for every new endpoint.
package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// apiRaw fetches a non-JSON response body (attachment downloads).
func (s *stack) apiRaw(t *testing.T, method, path, token string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, s.apiURL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp.StatusCode, raw, resp.Header
}

// 2.2 — end impersonation: the store method existed, the route was missing.
func TestEndImpersonationEndpoint(t *testing.T) {
	s := boot(t)
	staff := s.staffUser(t)
	_, user, _, _ := s.provisionTenant(t, "endimp")
	orgToken := s.login(t, staff.Email, "StaffPass123!")
	st, body, _ := s.apiReq(t, "POST", "/api/v1/admin/orgs/"+ids.Format(ids.Org, user.OrgID)+"/impersonate", orgToken,
		map[string]string{"user_id": ids.Format(ids.User, user.ID), "reason": "e2e end test"}, nil)
	if st != http.StatusOK {
		t.Fatalf("impersonate: status=%d body=%v", st, body)
	}
	impTok, _ := body["token"].(string)
	var sessID uuid.UUID
	if err := s.st.Pool.QueryRow(context.Background(),
		`select id from mail.session where token_hash=$1`, krypto.SHA256Hex(impTok)).Scan(&sessID); err != nil {
		t.Fatalf("session row: %v", err)
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/admin/impersonation/"+ids.Format(ids.Session, sessID)+"/end", orgToken,
		map[string]string{"reason": "done"}, nil)
	if st != http.StatusOK {
		t.Fatalf("end impersonation: status=%d body=%v", st, body)
	}
	if _, err := s.auth.VerifyToken(context.Background(), impTok); err == nil {
		t.Fatal("impersonation session must be revoked after the end call")
	}
}

// 2.3 — resume a paused mailbox: the breaker must be operable by humans.
func TestResumeMailboxAfterPause(t *testing.T) {
	s := boot(t)
	staff := s.staffUser(t)
	_, user, login, pw := s.provisionTenant(t, "resume")
	ownerTok := s.login(t, login, pw)
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.SetMailboxSendStatus(ctx, tx, mb.ID, "paused_admin", "e2e pause")
	}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	st, body, _ := s.apiReq(t, "POST", "/api/v1/send", ownerTok,
		map[string]any{"to": []map[string]string{{"email": "dest@outside.test"}}, "subject": "paused", "text": "x"}, nil)
	if st != http.StatusForbidden || errCode(body) != "mailbox_paused" {
		t.Fatalf("paused mailbox send: status=%d body=%v", st, body)
	}
	staffTok := s.login(t, staff.Email, "StaffPass123!")
	st, body, _ = s.apiReq(t, "POST", "/api/v1/admin/mailboxes/"+ids.Format(ids.Mailbox, mb.ID)+"/resume", staffTok,
		map[string]string{"reason": "e2e resume"}, nil)
	if st != http.StatusOK {
		t.Fatalf("resume: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/send", ownerTok,
		map[string]any{"to": []map[string]string{{"email": "dest@outside.test"}}, "subject": "resumed", "text": "x"}, nil)
	if st != http.StatusAccepted {
		t.Fatalf("send after resume: status=%d body=%v", st, body)
	}
}

// 2.4 — the suppression list, org-scoped and across orgs.
func TestSuppressionListEndpoint(t *testing.T) {
	s := boot(t)
	staff := s.staffUser(t)
	org, _, _, _ := s.provisionTenant(t, "supp")
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.Suppress(ctx, tx, org.ID, "bounced-hard@x.test", "hard_bounce")
	}); err != nil {
		t.Fatalf("suppress: %v", err)
	}
	staffTok := s.login(t, staff.Email, "StaffPass123!")
	st, body, _ := s.apiReq(t, "GET", "/api/v1/admin/suppression?org_id="+ids.Format(ids.Org, org.ID), staffTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("scoped list: status=%d body=%v", st, body)
	}
	rows, _ := body["suppression"].([]any)
	if len(rows) != 1 {
		t.Fatalf("scoped suppression rows = %d, want 1", len(rows))
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/suppression", staffTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("full list: status=%d body=%v", st, body)
	}
	if rows, _ = body["suppression"].([]any); len(rows) < 1 {
		t.Fatal("full suppression list must not be empty")
	}
}

// 2.5 — change own password: current password checked, other sessions revoked.
func TestChangeOwnPassword(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "chpw")
	tok1 := s.login(t, login, pw)
	tok2 := s.login(t, login, pw)

	st, body, _ := s.apiReq(t, "POST", "/api/v1/me/password", tok1,
		map[string]string{"current_password": "wrong-current", "new_password": "NewPassw0rd-9"}, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("wrong current password: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/me/password", tok1,
		map[string]string{"current_password": pw, "new_password": "NewPassw0rd-9"}, nil)
	if st != http.StatusOK {
		t.Fatalf("change: status=%d body=%v", st, body)
	}
	if st, _, _ = s.apiReq(t, "GET", "/api/v1/me", tok1, nil, nil); st != http.StatusOK {
		t.Fatalf("acting session must survive: status=%d", st)
	}
	if st, _, _ = s.apiReq(t, "GET", "/api/v1/me", tok2, nil, nil); st != http.StatusUnauthorized {
		t.Fatalf("other sessions must be revoked: status=%d", st)
	}
	if _, _, err := s.auth.Login(context.Background(), login, pw, "127.0.0.2", "e2e", ""); err == nil {
		t.Fatal("old password must stop working")
	}
	if _, _, err := s.auth.Login(context.Background(), login, "NewPassw0rd-9", "127.0.0.2", "e2e", ""); err != nil {
		t.Fatalf("new password must work: %v", err)
	}
}

// 2.6 + 2.7 + 2.8 — org users: list, suspend/resume, revoke sessions.
func TestOrgUsersSuspendRevoke(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "users")
	ownerTok := s.login(t, login, pw)
	teammateEmail := "teammate-users@" + s.cfg.PlatformDomain
	st, body, _ := s.apiReq(t, "POST", "/api/v1/users/invite", ownerTok,
		map[string]string{"email": teammateEmail, "display_name": "Mate", "password": "Passw0rd-users"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("invite: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/org/users", ownerTok, nil, nil)
	users, _ := body["users"].([]any)
	if st != http.StatusOK || len(users) < 2 {
		t.Fatalf("org users list: status=%d n=%d", st, len(users))
	}
	var teammateID string
	for _, u := range users {
		m := u.(map[string]any)
		if m["email"] == teammateEmail {
			teammateID, _ = m["id"].(string)
		}
	}
	if teammateID == "" {
		t.Fatalf("teammate missing from list: %v", body)
	}

	// Suspend: the teammate cannot log in any more.
	st, _, _ = s.apiReq(t, "POST", "/api/v1/org/users/"+teammateID+"/suspend", ownerTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("suspend: status=%d", st)
	}
	if _, _, err := s.auth.Login(context.Background(), teammateEmail, "Passw0rd-users", "127.0.0.2", "e2e", ""); err == nil {
		t.Fatal("suspended user must not log in")
	}
	st, _, _ = s.apiReq(t, "POST", "/api/v1/org/users/"+teammateID+"/resume", ownerTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("resume: status=%d", st)
	}
	tmTok := s.login(t, teammateEmail, "Passw0rd-users")

	// A non-admin cannot suspend anyone.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/org/users/"+teammateID+"/suspend", tmTok, nil, nil)
	if st != http.StatusForbidden {
		t.Fatalf("member suspend must be forbidden: status=%d body=%v", st, body)
	}

	// Revoke sessions: the teammate's token dies.
	st, _, _ = s.apiReq(t, "POST", "/api/v1/org/users/"+teammateID+"/revoke-sessions", ownerTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("revoke-sessions: status=%d", st)
	}
	if st, _, _ = s.apiReq(t, "GET", "/api/v1/me", tmTok, nil, nil); st != http.StatusUnauthorized {
		t.Fatalf("teammate token must be dead after revoke: status=%d", st)
	}
}

// 2.9 — shared-mailbox members management (model now, feature P2).
func TestSharedMailboxMembers(t *testing.T) {
	s := boot(t)
	_, owner, login, pw := s.provisionTenant(t, "shmbx")
	ownerTok := s.login(t, login, pw)
	mb, err := s.ownMailbox(owner)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	teammateEmail := "teammate-shmbx@" + s.cfg.PlatformDomain
	st, body, _ := s.apiReq(t, "POST", "/api/v1/users/invite", ownerTok,
		map[string]string{"email": teammateEmail, "display_name": "Mate", "password": "Passw0rd-shmbx"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("invite: %d", st)
	}
	st, users, _ := s.apiReq(t, "GET", "/api/v1/org/users", ownerTok, nil, nil)
	var teammateID string
	for _, u := range users["users"].([]any) {
		m := u.(map[string]any)
		if m["email"] == teammateEmail {
			teammateID, _ = m["id"].(string)
		}
	}
	mbPath := "/api/v1/org/shared-mailboxes/" + ids.Format(ids.Mailbox, mb.ID)
	st, body, _ = s.apiReq(t, "POST", mbPath+"/members", ownerTok,
		map[string]string{"user_id": teammateID, "role": "member"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("add member: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", mbPath+"/members", ownerTok, nil, nil)
	members, _ := body["members"].([]any)
	if st != http.StatusOK || len(members) != 1 {
		t.Fatalf("members list: status=%d n=%d", st, len(members))
	}
	st, _, _ = s.apiReq(t, "DELETE", mbPath+"/members?user_id="+teammateID, ownerTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("remove member: status=%d", st)
	}
	st, body, _ = s.apiReq(t, "GET", mbPath+"/members", ownerTok, nil, nil)
	if members, _ = body["members"].([]any); len(members) != 0 {
		t.Fatalf("member not removed: %v", body)
	}
}

// 2.10 — attachment list + download: the webmail's hard prerequisite.
func TestAttachmentDownload(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "att")
	ownerTok := s.login(t, login, pw)
	content := "hello attachment payload"
	st, body, _ := s.apiReq(t, "POST", "/api/v1/send", ownerTok,
		map[string]any{
			"to":      []map[string]string{{"email": "dest@outside.test"}},
			"subject": "with attachment", "text": "x",
			"attachments": []map[string]any{{
				"filename": "hello.txt", "content_type": "text/plain",
				"content": []byte(content),
			}},
		}, nil)
	if st != http.StatusAccepted {
		t.Fatalf("send: status=%d body=%v", st, body)
	}
	msgID, _ := body["message_id"].(string)
	st, body, _ = s.apiReq(t, "GET", "/api/v1/messages/"+msgID+"/attachments", ownerTok, nil, nil)
	atts, _ := body["attachments"].([]any)
	if st != http.StatusOK || len(atts) != 1 {
		t.Fatalf("attachments list: status=%d n=%d", st, len(atts))
	}
	attID, _ := atts[0].(map[string]any)["id"].(string)
	path, _ := atts[0].(map[string]any)["download_path"].(string)
	if !strings.Contains(path, attID) {
		t.Fatalf("download_path %q must reference the attachment id", path)
	}
	st, raw, hdr := s.apiRaw(t, "GET", path, ownerTok)
	if st != http.StatusOK || string(raw) != content {
		t.Fatalf("download: status=%d body=%q", st, raw)
	}
	if ct := hdr.Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("content type: %q", ct)
	}
}

// 2.11 — quarantine list and release.
func TestQuarantineListRelease(t *testing.T) {
	s := boot(t)
	org, owner, login, pw := s.provisionTenant(t, "quar")
	ownerTok := s.login(t, login, pw)
	mb, err := s.ownMailbox(owner)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	qid := uuid.New()
	err = s.st.WithTx(context.Background(), func(tx store.Tx) error {
		return store.QuarantineMessage(context.Background(), tx, &model.Quarantine{
			ID: qid, OrgID: org.ID, MailboxID: &mb.ID, Reason: "spam",
			Score: 8.5, ExpiresAt: time.Now().Add(48 * time.Hour),
		})
	})
	if err != nil {
		t.Fatalf("quarantine insert: %v", err)
	}
	st, body, _ := s.apiReq(t, "GET", "/api/v1/org/quarantine", ownerTok, nil, nil)
	rows, _ := body["quarantine"].([]any)
	if st != http.StatusOK || len(rows) != 1 {
		t.Fatalf("quarantine list: status=%d n=%d", st, len(rows))
	}
	if first := rows[0].(map[string]any); first["reason"] != "spam" {
		t.Fatalf("row: %v", first)
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/org/quarantine/"+ids.Format(ids.Quarantine, qid)+"/release", ownerTok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("release: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/org/quarantine", ownerTok, nil, nil)
	if rows, _ = body["quarantine"].([]any); len(rows) != 0 {
		t.Fatalf("quarantine not emptied: %v", body)
	}
}

// 2.1 — decline the review application and filter the queue.
func TestDeclineOrgAndReviewFilter(t *testing.T) {
	s := boot(t)
	staff := s.staffUser(t)
	staffTok := s.login(t, staff.Email, "StaffPass123!")
	st, body, _ := s.apiReq(t, "POST", "/api/v1/admin/orgs", staffTok,
		map[string]string{"name": "DeclineMe", "owner_email": "owner-decline@" + s.cfg.PlatformDomain,
			"owner_name": "D", "password": "Passw0rd-decline"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create org: status=%d body=%v", st, body)
	}
	orgID, _ := body["org"].(map[string]any)["id"].(string)

	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/orgs?review_status=pending", staffTok, nil, nil)
	found := false
	for _, o := range body["orgs"].([]any) {
		if o.(map[string]any)["org"].(map[string]any)["id"] == orgID {
			found = true
		}
	}
	if !found {
		t.Fatal("new org must be in the pending queue")
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/admin/orgs/"+orgID+"/decline", staffTok,
		map[string]string{"reason": "e2e decline"}, nil)
	if st != http.StatusOK {
		t.Fatalf("decline: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/orgs?review_status=pending", staffTok, nil, nil)
	for _, o := range body["orgs"].([]any) {
		if o.(map[string]any)["org"].(map[string]any)["id"] == orgID {
			t.Fatal("declined org must leave the pending queue")
		}
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/orgs?review_status=declined", staffTok, nil, nil)
	found = false
	for _, o := range body["orgs"].([]any) {
		m := o.(map[string]any)["org"].(map[string]any)
		if m["id"] == orgID {
			found = true
			if m["sending_enabled"] != false {
				t.Fatalf("declined org must not send: %v", m)
			}
		}
	}
	if !found {
		t.Fatal("declined org must be in the declined filter")
	}
}

// 2.13 — the delivery-queue inspection shapes the console consumes.
func TestAdminQueueShapes(t *testing.T) {
	s := boot(t)
	staff := s.staffUser(t)
	org, _, _, _ := s.provisionTenant(t, "qshape")
	staffTok := s.login(t, staff.Email, "StaffPass123!")
	st, body, _ := s.apiReq(t, "GET", "/api/v1/admin/queue", staffTok, nil, nil)
	if st != http.StatusOK {
		if _, ok := body["queue_depth"]; !ok {
			t.Fatalf("queue shape: status=%d body=%v", st, body)
		}
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/orgs/"+ids.Format(ids.Org, org.ID)+"/jobs", staffTok, nil, nil)
	if st != http.StatusOK {
		if _, ok := body["jobs"]; !ok {
			t.Fatalf("org jobs shape: status=%d body=%v", st, body)
		}
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/admin/stats", staffTok, nil, nil)
	if st != http.StatusOK {
		if _, ok := body["queue_depth"]; !ok {
			t.Fatalf("stats shape: status=%d body=%v", st, body)
		}
	}
}
