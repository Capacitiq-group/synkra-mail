// Phase 1 of the consolidated build register (§1 API and authentication
// hardening): regression tests for every hardening item, 1.1 through 1.10.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// apiReq performs a JSON request against the harness API and decodes the body.
func (s *stack) apiReq(t *testing.T, method, path, token string, body any, hdr map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequest(method, s.apiURL+path, rd)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && err.Error() != "EOF" {
		t.Fatalf("%s %s: decode response: %v", method, path, err)
	}
	return resp.StatusCode, out, resp.Header
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	if e == nil {
		return ""
	}
	c, _ := e["code"].(string)
	return c
}

// staffUser creates (or reuses) the Platform org's staff account, like the
// admin portal's own bootstrap.
func (s *stack) staffUser(t *testing.T) *model.User {
	t.Helper()
	ctx := context.Background()
	plan, err := store.GetPlanBySlug(ctx, s.st.Pool, "business")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var staffOrgID uuid.UUID
	err = s.st.Pool.QueryRow(ctx, `select id from mail.org where name='Platform' limit 1`).Scan(&staffOrgID)
	if err != nil {
		err = s.st.WithTx(ctx, func(tx store.Tx) error {
			staffOrgID = uuid.New()
			_, err := tx.Exec(ctx, `insert into mail.org (id, name, plan_id, review_status, sending_enabled)
				values ($1,'Platform',$2,'approved',true)`, staffOrgID, plan.ID)
			return err
		})
		if err != nil {
			t.Fatalf("staff org: %v", err)
		}
	}
	u := &model.User{ID: uuid.New(), OrgID: staffOrgID, Email: "staff-" + uuid.NewString()[:8] + "@synkra.co.za",
		PasswordHash: hashPassword("StaffPass123!"), DisplayName: "Staff", Role: "owner", IsPlatformAdmin: true}
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.CreateUser(ctx, tx, u)
	}); err != nil {
		t.Fatalf("staff user: %v", err)
	}
	return u
}

// 1.1 — a wrong id prefix is a bad request, a bare uuid still resolves.
func TestIDPrefixMismatchRejected(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "prefix")
	token := s.login(t, login, pw)
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	inbox, err := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderInbox)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}

	st, body, _ := s.apiReq(t, "GET", "/api/v1/messages?folder_id="+ids.Format(ids.Message, uuid.New()), token, nil, nil)
	if st != http.StatusBadRequest || errCode(body) != "invalid_request" {
		t.Fatalf("msg_ id as folder id: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/messages/"+ids.Format(ids.Folder, inbox.ID), token, nil, nil)
	if st != http.StatusBadRequest || errCode(body) != "invalid_request" {
		t.Fatalf("fld_ id as message id: status=%d body=%v", st, body)
	}
	// Bare uuid (no prefix) is still accepted; missing message → not_found.
	st, body, _ = s.apiReq(t, "GET", "/api/v1/messages/"+uuid.New().String(), token, nil, nil)
	if st != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("bare uuid: status=%d body=%v", st, body)
	}
	// The correctly typed folder id works.
	st, _, _ = s.apiReq(t, "GET", "/api/v1/messages?folder_id="+ids.Format(ids.Folder, inbox.ID)+"&limit=1", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("correct folder id: status=%d", st)
	}
}

// 1.2 — a login e-mail that exists in two orgs is refused, never resolved by luck.
func TestAmbiguousLoginRefused(t *testing.T) {
	s := boot(t)
	_, _, loginA, pwA := s.provisionTenant(t, "amba")
	ctx := context.Background()
	plan, err := store.GetPlanBySlug(ctx, s.st.Pool, "business")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// A second org whose owner claims the same login e-mail string.
	orgB := &model.Org{ID: uuid.New(), Name: "ambb", PlanID: plan.ID}
	uB := &model.User{ID: uuid.New(), OrgID: orgB.ID, Email: loginA,
		PasswordHash: hashPassword("Passw0rd-ambb"), DisplayName: "Dup Owner", Role: "owner"}
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		if err := store.CreateOrg(ctx, tx, orgB, s.cfg.TrialDays); err != nil {
			return err
		}
		return store.CreateUser(ctx, tx, uB)
	}); err != nil {
		t.Fatalf("org b: %v", err)
	}

	_, _, err = s.auth.Login(ctx, loginA, pwA, "127.0.0.1", "e2e")
	if err == nil {
		t.Fatal("shared login e-mail must not silently resolve to one org")
	}
	st, body, _ := s.apiReq(t, "POST", "/api/v1/auth/login", "", map[string]string{"email": loginA, "password": pwA}, nil)
	if st != http.StatusConflict || errCode(body) != "ambiguous_login" {
		t.Fatalf("ambiguous login via api: status=%d body=%v", st, body)
	}
	// A unique login still works.
	_, _, loginC, pwC := s.provisionTenant(t, "ambc")
	if _, _, err := s.auth.Login(ctx, loginC, pwC, "127.0.0.1", "e2e"); err != nil {
		t.Fatalf("unambiguous login broken: %v", err)
	}
}

// 1.3 — a suspended org keeps logout and the status read; everything else is blocked.
func TestSuspendedOrgKeepsLogoutAndMe(t *testing.T) {
	s := boot(t)
	org, _, login, pw := s.provisionTenant(t, "susp")
	token := s.login(t, login, pw)
	ctx := context.Background()
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.SetOrgStatus(ctx, tx, org.ID, "suspended")
	}); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	st, body, _ := s.apiReq(t, "GET", "/api/v1/me", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("/me must stay available: status=%d body=%v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/mailboxes", token, nil, nil)
	if st != http.StatusForbidden || errCode(body) != "forbidden" {
		t.Fatalf("other routes must be blocked: status=%d body=%v", st, body)
	}
	if e, _ := body["error"].(map[string]any); e == nil || e["hint"] == "" {
		t.Fatalf("suspension error must carry a hint (§1.10): %v", body)
	}
	st, _, _ = s.apiReq(t, "POST", "/api/v1/auth/logout", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("logout must stay available: status=%d", st)
	}
	st, _, _ = s.apiReq(t, "GET", "/api/v1/me", token, nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("session must be revoked after logout: status=%d", st)
	}
}

// 1.4 — thread notes and assignment need mailbox membership, not just org membership.
func TestThreadNoteRequiresMailboxMembership(t *testing.T) {
	s := boot(t)
	org, owner, login, pw := s.provisionTenant(t, "thread")
	ctx := context.Background()
	mb, err := s.ownMailbox(owner)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	// An org member who is not a member of the owner's mailbox.
	teammate := &model.User{ID: uuid.New(), OrgID: org.ID, Email: "teammate-thread@" + s.cfg.PlatformDomain,
		PasswordHash: hashPassword("Passw0rd-thread"), DisplayName: "Teammate", Role: "member"}
	if err := s.st.WithTx(ctx, func(tx store.Tx) error {
		return store.CreateUser(ctx, tx, teammate)
	}); err != nil {
		t.Fatalf("teammate: %v", err)
	}

	threadID := uuid.New()
	sha := "sha-thread-" + threadID.String()
	mid := uuid.New()
	for _, stmt := range []struct{ q string; args []any }{
		{`insert into mail.thread (id, org_id, subject_norm) values ($1,$2,'help please')`, []any{threadID, org.ID}},
		{`insert into mail.blob (sha256, size_bytes, refcount) values ($1,10,1)`, []any{sha}},
		{`insert into mail.message (id, org_id, mailbox_id, blob_sha256, direction, thread_id, subject, snippet, size_bytes, internal_date)
			values ($1,$2,$3,$4,'inbound',$5,'help please','',10,now())`, []any{mid, org.ID, mb.ID, sha, threadID}},
	} {
		if _, err := s.st.Pool.Exec(ctx, stmt.q, stmt.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	ownerTok := s.login(t, login, pw)
	teamTok := s.login(t, string(teammate.Email), "Passw0rd-thread")
	threadPath := "/api/v1/threads/" + ids.Format(ids.Thread, threadID)

	st, _, _ := s.apiReq(t, "POST", threadPath+"/note", teamTok, map[string]string{"note": "sneaky"}, nil)
	if st != http.StatusForbidden {
		t.Fatalf("org member without mailbox membership must be refused: status=%d", st)
	}
	st, _, _ = s.apiReq(t, "POST", threadPath+"/note", ownerTok, map[string]string{"note": "customer is waiting"}, nil)
	if st != http.StatusOK {
		t.Fatalf("mailbox member must be able to note: status=%d", st)
	}
	st, _, _ = s.apiReq(t, "POST", threadPath+"/assign", ownerTok, map[string]string{"assignee_id": ids.Format(ids.User, teammate.ID)}, nil)
	if st != http.StatusForbidden {
		t.Fatalf("assigning a non-member must be refused: status=%d", st)
	}
	st, _, _ = s.apiReq(t, "POST", threadPath+"/assign", ownerTok, map[string]string{"assignee_id": ids.Format(ids.User, owner.ID)}, nil)
	if st != http.StatusOK {
		t.Fatalf("assigning a member must work: status=%d", st)
	}
	var note *string
	var assignee *uuid.UUID
	if err := s.st.Pool.QueryRow(ctx, `select note, assignee_id from mail.thread where id=$1`, threadID).Scan(&note, &assignee); err != nil {
		t.Fatalf("thread row: %v", err)
	}
	if note == nil || *note != "customer is waiting" || assignee == nil || *assignee != owner.ID {
		t.Fatalf("thread not updated: note=%v assignee=%v", note, assignee)
	}
}

// 1.5 — RFC 2142 role addresses cannot be registered on the platform domain.
func TestReservedLocalsOnPlatformDomain(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "resv")
	token := s.login(t, login, pw)
	for _, local := range []string{"postmaster", "abuse", "ADMIN"} {
		email := local + "@" + s.cfg.PlatformDomain
		st, body, _ := s.apiReq(t, "POST", "/api/v1/users/invite", token,
			map[string]string{"email": email, "display_name": "X", "password": "Passw0rd-resv"}, nil)
		if st != http.StatusConflict || errCode(body) != "reserved_address" {
			t.Fatalf("reserved local %q: status=%d body=%v", local, st, body)
		}
	}
	// A normal invite still works — the refusals come from the guard.
	st, body, _ := s.apiReq(t, "POST", "/api/v1/users/invite", token,
		map[string]string{"email": "teammate-resv@" + s.cfg.PlatformDomain, "display_name": "T", "password": "Passw0rd-resv"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("normal invite broken: status=%d body=%v", st, body)
	}
}

// 1.6 — repeated failed app-password logins lock the protocol ports.
func TestAppPasswordThrottleOnSubmission(t *testing.T) {
	s := boot(t)
	_, user, login, _ := s.provisionTenant(t, "thr")
	secret := s.newAppPassword(t, user, "mailer")
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	ctx := context.Background()
	for i := 0; i < s.cfg.HTTP.LoginMaxFails; i++ {
		if _, err := smtpSubmission(s.subAddr, login, "wrong-secret-attempt", primary, primary, "Subject: x\r\n\r\nx"); err == nil {
			t.Fatalf("wrong secret must fail (attempt %d)", i+1)
		}
	}
	if _, err := smtpSubmission(s.subAddr, login, secret, primary, primary, "Subject: y\r\n\r\ny"); err == nil {
		t.Fatal("the correct secret must be locked after repeated failures")
	}
	// The lockout shares the login_attempt window with the web login; clear the
	// records so later tests logging in from 127.0.0.1 are not poisoned.
	if _, err := s.st.Pool.Exec(ctx, `delete from mail.login_attempt`); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	code, err := smtpSubmission(s.subAddr, login, secret, primary, primary, "Subject: after lockout\r\n\r\nok")
	if err != nil || code != 250 {
		t.Fatalf("correct secret must work again after the window: code=%d err=%v", code, err)
	}
	s.drainWorker(t)
}

// 1.7 — staff accounts cannot be impersonated.
func TestStaffImpersonationForbidden(t *testing.T) {
	s := boot(t)
	staff := s.staffUser(t)
	token := s.login(t, staff.Email, "StaffPass123!")
	st, body, _ := s.apiReq(t, "POST", "/api/v1/admin/orgs/"+ids.Format(ids.Org, staff.OrgID)+"/impersonate", token,
		map[string]string{"user_id": ids.Format(ids.User, staff.ID), "reason": "self test"}, nil)
	if st != http.StatusForbidden || errCode(body) != "forbidden" {
		t.Fatalf("staff impersonating staff: status=%d body=%v", st, body)
	}
}

// 1.8 — the message list is cursor-paginated, newest page first.
func TestMessageListPagination(t *testing.T) {
	s := boot(t)
	org, user, login, pw := s.provisionTenant(t, "page")
	token := s.login(t, login, pw)
	ctx := context.Background()
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	inbox, err := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb.ID, model.FolderInbox)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	for i := 1; i <= 7; i++ {
		mid := uuid.New()
		sha := "sha-page-" + mid.String()
		if _, err := s.st.Pool.Exec(ctx, `insert into mail.blob (sha256, size_bytes, refcount) values ($1,10,1)`, sha); err != nil {
			t.Fatalf("blob: %v", err)
		}
		if _, err := s.st.Pool.Exec(ctx, `insert into mail.message (id, org_id, mailbox_id, blob_sha256, direction, subject, snippet, size_bytes, internal_date)
			values ($1,$2,$3,$4,'inbound',$5,'',10,now())`, mid, org.ID, mb.ID, sha, fmt.Sprintf("page msg %d", i)); err != nil {
			t.Fatalf("message: %v", err)
		}
		if _, err := s.st.Pool.Exec(ctx, `insert into mail.mailbox_item (id, folder_id, message_id, uid) values ($1,$2,$3,$4)`,
			uuid.New(), inbox.ID, mid, i); err != nil {
			t.Fatalf("item: %v", err)
		}
	}
	folderQ := ids.Format(ids.Folder, inbox.ID)
	uids := func(body map[string]any) []uint32 {
		raw, _ := body["items"].([]any)
		var out []uint32
		for _, it := range raw {
			m := it.(map[string]any)
			item := m["item"].(map[string]any)
			out = append(out, uint32(item["uid"].(float64)))
		}
		return out
	}

	st, body, _ := s.apiReq(t, "GET", "/api/v1/messages?folder_id="+folderQ+"&limit=3", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("page 1: status=%d", st)
	}
	if got := uids(body); len(got) != 3 || got[0] != 5 || got[1] != 6 || got[2] != 7 {
		t.Fatalf("page 1 uids = %v, want [5 6 7] (ascending, newest page)", got)
	}
	if body["has_more"] != true || body["next_before_uid"].(float64) != 5 {
		t.Fatalf("page 1 cursor: %v", body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/messages?folder_id="+folderQ+"&limit=3&before_uid=5", token, nil, nil)
	if got := uids(body); len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("page 2 uids = %v, want [2 3 4]", got)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/messages?folder_id="+folderQ+"&limit=10&before_uid=2", token, nil, nil)
	if got := uids(body); len(got) != 1 || got[0] != 1 || body["has_more"] != false {
		t.Fatalf("last page: uids=%v has_more=%v", got, body["has_more"])
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/messages?folder_id="+folderQ, token, nil, nil)
	if got := uids(body); len(got) != 7 {
		t.Fatalf("default limit returns the whole small folder: %d items", len(got))
	}
}

// 1.9 — keyed sends and alias creations replay instead of double-executing.
func TestSendIdempotency(t *testing.T) {
	s := boot(t)
	org, _, login, pw := s.provisionTenant(t, "idem")
	token := s.login(t, login, pw)
	sendBody := map[string]any{
		"to": []map[string]string{{"email": "dest@outside.test"}},
		"subject": "idem", "text": "once only",
	}
	hdr := map[string]string{"Idempotency-Key": "k1"}
	st1, b1, h1 := s.apiReq(t, "POST", "/api/v1/send", token, sendBody, hdr)
	if st1 != http.StatusAccepted {
		t.Fatalf("first send: status=%d body=%v", st1, b1)
	}
	if h1.Get("Idempotent-Replay") != "" {
		t.Fatal("first response must not be marked as a replay")
	}
	st2, b2, h2 := s.apiReq(t, "POST", "/api/v1/send", token, sendBody, hdr)
	if st2 != http.StatusAccepted || b2["message_id"] != b1["message_id"] || h2.Get("Idempotent-Replay") != "true" {
		t.Fatalf("replayed send: status=%d id1=%v id2=%v replay=%q", st2, b1["message_id"], b2["message_id"], h2.Get("Idempotent-Replay"))
	}
	var jobs int
	if err := s.st.Pool.QueryRow(context.Background(),
		`select count(*) from mail.delivery_job where org_id=$1`, org.ID).Scan(&jobs); err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if jobs != 1 {
		t.Fatalf("keyed retry must not queue twice: %d jobs", jobs)
	}
	// Same key, different body → explicit rejection.
	other := map[string]any{"to": []map[string]string{{"email": "other@outside.test"}}, "subject": "different", "text": "x"}
	st3, b3, _ := s.apiReq(t, "POST", "/api/v1/send", token, other, hdr)
	if st3 != http.StatusUnprocessableEntity || errCode(b3) != "idempotency_key_reuse" {
		t.Fatalf("key reuse: status=%d body=%v", st3, b3)
	}
}

func TestAliasCreateIdempotency(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "idemaddr")
	token := s.login(t, login, pw)
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	st, dom, _ := s.apiReq(t, "POST", "/api/v1/domains", token,
		map[string]string{"name": "idem-alias.test"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("domain create: status=%d body=%v", st, dom)
	}
	domID, _ := dom["domain"].(map[string]any)["id"].(string)
	addrBody := map[string]string{"local": "sales", "domain_id": domID,
		"mailbox_id": ids.Format(ids.Mailbox, mb.ID), "kind": "alias"}
	hdr := map[string]string{"Idempotency-Key": "ka"}
	st1, b1, _ := s.apiReq(t, "POST", "/api/v1/addresses", token, addrBody, hdr)
	if st1 != http.StatusCreated {
		t.Fatalf("first alias: status=%d body=%v", st1, b1)
	}
	st2, b2, h2 := s.apiReq(t, "POST", "/api/v1/addresses", token, addrBody, hdr)
	if st2 != http.StatusCreated || b2["id"] != b1["id"] || h2.Get("Idempotent-Replay") != "true" {
		t.Fatalf("replayed alias: status=%d id1=%v id2=%v", st2, b1["id"], b2["id"])
	}
}

// 1.10 — the DNS doctor says what is wrong, why, and how to fix it.
func TestDNSDoctorExplainsTheFix(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "dnsdr")
	token := s.login(t, login, pw)
	st, dom, _ := s.apiReq(t, "POST", "/api/v1/domains", token, map[string]string{"name": "doctor.test"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("domain: %d", st)
	}
	domID, _ := dom["domain"].(map[string]any)["id"].(string)
	st, body, _ := s.apiReq(t, "POST", "/api/v1/domains/"+domID+"/verify", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("verify: %d", st)
	}
	if body["verified"] != false {
		t.Fatalf("unverified domain must say so: %v", body)
	}
	problem, _ := body["problem"].(map[string]any)
	for _, key := range []string{"what", "why", "fix"} {
		if v, _ := problem[key].(string); v == "" {
			t.Fatalf("problem.%s missing: %v", key, body)
		}
	}
}
