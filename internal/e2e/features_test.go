// Phases 6+7 of the consolidated build register: the §12 feature-register
// endpoints (signatures, autoresponder, rules, unsubscribe, trace,
// deliverability) and the §13 differentiator endpoints (health, reliability,
// webhooks, API keys).
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func featureMailbox(t *testing.T, s *stack, user *model.User) uuid.UUID {
	t.Helper()
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	return mb.ID
}

// 12b — signatures CRUD with a single default.
func TestSignaturesCRUD(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "sigs")
	token := s.login(t, login, pw)

	st, body, _ := s.apiReq(t, "POST", "/api/v1/me/signatures", token,
		map[string]any{"name": "work", "text": "— Ada", "html": "<p>— Ada</p>", "is_default": true}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %v", st, body)
	}
	st, _, _ = s.apiReq(t, "POST", "/api/v1/me/signatures", token,
		map[string]any{"name": "personal", "text": "Ada (personal)", "is_default": true}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create second: %d", st)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/me/signatures", token, nil, nil)
	sigs, _ := body["signatures"].([]any)
	if st != http.StatusOK || len(sigs) != 2 {
		t.Fatalf("list: %d n=%d", st, len(sigs))
	}
	defaults := 0
	for _, g := range sigs {
		if g.(map[string]any)["is_default"] == true {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("exactly one default allowed, got %d", defaults)
	}
	sigID, _ := sigs[0].(map[string]any)["id"].(string)
	st, _, _ = s.apiReq(t, "DELETE", "/api/v1/me/signatures/"+sigID, token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("delete: %d", st)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/me/signatures", token, nil, nil)
	if sigs, _ = body["signatures"].([]any); len(sigs) != 1 {
		t.Fatalf("after delete: %d signatures", len(sigs))
	}
}

// 12c — the autoresponder answers an inbound message once per sender per day.
func TestAutoresponderRoundTrip(t *testing.T) {
	s := boot(t)
	_, userA, loginA, pwA := s.provisionTenant(t, "oooo")
	orgB, userB, _, _ := s.provisionTenant(t, "bbbb")
	tokenA := s.login(t, loginA, pwA)
	mbA := featureMailbox(t, s, userA)

	primaryB, err := s.primaryAddressFor(userB)
	if err != nil {
		t.Fatalf("primary b: %v", err)
	}
	primaryA, _ := s.primaryAddressFor(userA)

	st, body, _ := s.apiReq(t, "PUT", "/api/v1/mailboxes/"+ids.Format(ids.Mailbox, mbA)+"/autoresponder", tokenA,
		map[string]any{"enabled": true, "subject": "I am away", "body": "Back on Monday."}, nil)
	if st != http.StatusOK {
		t.Fatalf("set autoresponder: %d %v", st, body)
	}

	// B writes to A; A's autoresponder answers B through the normal pipeline.
	code, err := smtpDialogue(s.smtpAddr, primaryB, primaryA, "From: "+primaryB+"\r\nSubject: hello\r\n\r\nare you there?")
	if err != nil || code != 250 {
		t.Fatalf("inbound: code=%d err=%v", code, err)
	}
	s.drainWorker(t)

	mbB := featureMailbox(t, s, userB)
	inboxB, _ := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mbB, model.FolderInbox)
	items, _ := store.ListItems(context.Background(), s.st.Pool, inboxB.ID, 0, 0xffffffff)
	if len(items) != 1 || !strings.Contains(items[0].Message.Subject, "away") {
		t.Fatalf("auto reply missing in B's inbox: %d items", len(items))
	}

	// The same sender again the same day → no second reply.
	code, err = smtpDialogue(s.smtpAddr, primaryB, primaryA, "From: "+primaryB+"\r\nSubject: hello again\r\n\r\nstill there?")
	if err != nil || code != 250 {
		t.Fatalf("inbound 2: code=%d err=%v", code, err)
	}
	s.drainWorker(t)
	items, _ = store.ListItems(context.Background(), s.st.Pool, inboxB.ID, 0, 0xffffffff)
	if len(items) != 1 {
		t.Fatalf("rate limit failed: %d replies", len(items))
	}
	_ = orgB
}

// 12d — an inbound routing rule moves matching mail out of INBOX.
func TestRoutingRuleMovesInbound(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "rules")
	token := s.login(t, login, pw)
	mb := featureMailbox(t, s, user)

	// A folder to move matching mail into.
	st, fbody, _ := s.apiReq(t, "POST", "/api/v1/folders", token,
		map[string]string{"name": "Offers", "mailbox_id": ids.Format(ids.Mailbox, mb)}, nil)
	if st != http.StatusCreated && st != http.StatusOK {
		t.Fatalf("folder: %d %v", st, fbody)
	}
	folderID, _ := fbody["folder"].(map[string]any)["id"].(string)
	if folderID == "" {
		if id, ok := fbody["id"].(string); ok {
			folderID = id
		}
	}

	st, body, _ := s.apiReq(t, "POST", "/api/v1/mailboxes/"+ids.Format(ids.Mailbox, mb)+"/rules", token,
		map[string]any{"match_from": "vendor", "action_folder_id": folderID, "action_mark_read": true}, nil)
	if st != http.StatusCreated {
		t.Fatalf("rule: %d %v", st, body)
	}
	primary, _ := s.primaryAddressFor(user)

	code, err := smtpDialogue(s.smtpAddr, "vendor@shop.test", primary, "From: vendor@shop.test\r\nSubject: buy now\r\n\r\noffer")
	if err != nil || code != 250 {
		t.Fatalf("inbound: code=%d err=%v", code, err)
	}
	// A non-matching message stays in INBOX.
	code, err = smtpDialogue(s.smtpAddr, "friend@home.test", primary, "From: friend@home.test\r\nSubject: hi\r\n\r\nhello")
	if err != nil || code != 250 {
		t.Fatalf("inbound 2: code=%d err=%v", code, err)
	}

	ctx := context.Background()
	offers, _ := store.GetFolderByName(ctx, s.st.Pool, mb, "Offers")
	items, _ := store.ListItems(ctx, s.st.Pool, offers.ID, 0, 0xffffffff)
	if len(items) != 1 || !strings.Contains(items[0].Message.Subject, "buy now") {
		t.Fatalf("rule did not move the message: %d items", len(items))
	}
	if len(items) == 1 && items[0].Item.Flags != nil {
		for _, f := range items[0].Item.Flags {
			if f == model.FlagSeen {
				return // mark_read applied
			}
		}
		t.Fatalf("mark_read not applied: %v", items[0].Item.Flags)
	}
	inbox, _ := store.GetFolderBySpecialUse(ctx, s.st.Pool, mb, model.FolderInbox)
	inboxItems, _ := store.ListItems(ctx, s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(inboxItems) != 1 || !strings.Contains(inboxItems[0].Message.Subject, "hi") {
		t.Fatalf("INBOX wrong: %d items", len(inboxItems))
	}
}

// 12e — one-click unsubscribe: bulk send carries the header, the tracker link
// suppresses the recipient.
func TestUnsubscribeRoundTrip(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "unsub")
	token := s.login(t, login, pw)
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}

	st, body, _ := s.apiReq(t, "POST", "/api/v1/send", token,
		map[string]any{"to": []map[string]string{{"email": primary}},
			"subject": "monthly news", "text": "here is the news", "newsletter": true}, nil)
	if st != http.StatusAccepted {
		t.Fatalf("send: %d %v", st, body)
	}
	s.drainWorker(t)

	// The delivered copy must carry the RFC 2369 headers.
	mb, _ := s.ownMailbox(user)
	inbox, _ := store.GetFolderBySpecialUse(context.Background(), s.st.Pool, mb.ID, model.FolderInbox)
	items, _ := store.ListItems(context.Background(), s.st.Pool, inbox.ID, 0, 0xffffffff)
	if len(items) != 1 {
		t.Fatalf("no delivered copy: %d", len(items))
	}
	raw := storedBlob(t, s, items[0].Message.BlobSHA256)
	var unsubURL string
	for _, line := range strings.Split(string(raw), "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "list-unsubscribe:") {
			unsubURL = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			break
		}
	}
	if unsubURL == "" {
		t.Fatalf("List-Unsubscribe header missing in the delivered message")
	}

	// Click it: the tracker verifies the HMAC and the core suppresses.
	resp, err := http.Get(unsubURL)
	if err != nil {
		t.Fatalf("unsubscribe GET: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Unsubscribed") {
		t.Fatalf("unsubscribe page: %d %s", resp.StatusCode, b)
	}
	// The tracker forwards the event asynchronously; poll briefly.
	suppressed := false
	for i := 0; i < 25; i++ {
		suppressed, _ = store.IsSuppressed(context.Background(), s.st.Pool, user.OrgID, primary)
		if suppressed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !suppressed {
		t.Fatal("recipient not suppressed after the one-click unsubscribe")
	}
}

// 12a/12f + 13a/13b — trace, deliverability, health and reliability read the
// delivery event log and the jobs.
func TestTraceAndDashboards(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "dash")
	token := s.login(t, login, pw)
	primary, _ := s.primaryAddressFor(user)

	st, b1, _ := s.apiReq(t, "POST", "/api/v1/send", token,
		map[string]any{"to": []map[string]string{{"email": primary}}, "subject": "trace me", "text": "x"}, nil)
	if st != http.StatusAccepted {
		t.Fatalf("send: %d %v", st, b1)
	}
	st, b2, _ := s.apiReq(t, "POST", "/api/v1/send", token,
		map[string]any{"to": []map[string]string{{"email": "gone@outside.test"}}, "subject": "bounce me", "text": "x"}, nil)
	if st != http.StatusAccepted {
		t.Fatalf("send: %d %v", st, b2)
	}
	s.drainWorker(t)

	msgID, _ := b1["message_id"].(string)
	st, trace, _ := s.apiReq(t, "GET", "/api/v1/messages/"+msgID+"/trace", token, nil, nil)
	events, _ := trace["events"].([]any)
	if st != http.StatusOK || len(events) < 2 || events[0].(map[string]any)["state"] != "queued" {
		t.Fatalf("trace: %d %v", st, trace)
	}

	st, dash, _ := s.apiReq(t, "GET", "/api/v1/org/deliverability", token, nil, nil)
	if st != http.StatusOK || dash["sent"].(float64) < 2 {
		t.Fatalf("deliverability: %d %v", st, dash)
	}
	st, health, _ := s.apiReq(t, "GET", "/api/v1/org/health", token, nil, nil)
	if st != http.StatusOK || health["score"].(float64) < 0 || health["score"].(float64) > 100 {
		t.Fatalf("health: %d %v", st, health)
	}
	st, rel, _ := s.apiReq(t, "GET", "/api/v1/org/reliability", token, nil, nil)
	days, _ := rel["days"].([]any)
	if st != http.StatusOK || len(days) < 14 {
		t.Fatalf("reliability: %d %d days", st, len(days))
	}
}

// 13f — webhooks: signed dispatch of delivery outcomes.
func TestWebhookDispatch(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "hook")
	token := s.login(t, login, pw)
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}

	type hookCall struct {
		body string
		sig  string
	}
	received := make(chan hookCall, 4)
	var hookSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received <- hookCall{body: string(b), sig: r.Header.Get("X-Synkra-Signature")}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	st, body, _ := s.apiReq(t, "POST", "/api/v1/org/webhooks", token,
		map[string]any{"url": srv.URL, "events": []string{"delivered"}}, nil)
	if st != http.StatusCreated {
		t.Fatalf("webhook create: %d %v", st, body)
	}
	hookSecret, _ = body["secret"].(string)
	if hookSecret == "" {
		t.Fatal("webhook secret must be shown exactly once at creation")
	}

	st, _, _ = s.apiReq(t, "POST", "/api/v1/send", token,
		map[string]any{"to": []map[string]string{{"email": primary}}, "subject": "hooked", "text": "x"}, nil)
	if st != http.StatusAccepted {
		t.Fatalf("send failed")
	}
	s.drainWorker(t)

	select {
	case call := <-received:
		var payload map[string]any
		if err := json.Unmarshal([]byte(call.body), &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if payload["event"] != "delivered" {
			t.Fatalf("event: %v", payload["event"])
		}
		// The signature covers the raw body with the shown-once secret.
		want := "sha256=" + krypto.HMACSign([]byte(hookSecret), call.body)
		if call.sig != want {
			t.Fatalf("signature mismatch: got %q want %q", call.sig, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("webhook was not called")
	}
}

// 13f — API keys: machine access through the same tenancy middleware.
func TestAPIKeys(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "apikey")
	token := s.login(t, login, pw)

	st, body, _ := s.apiReq(t, "POST", "/api/v1/org/api-keys", token,
		map[string]string{"name": "ci"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create key: %d %v", st, body)
	}
	apiToken, _ := body["token"].(string)
	keyID, _ := body["id"].(string)
	if apiToken == "" || !strings.HasPrefix(apiToken, "sk_") {
		t.Fatalf("token format: %q", apiToken)
	}

	// The key calls the API with normal tenancy scoping.
	st, body, _ = s.apiReq(t, "GET", "/api/v1/org/users", apiToken, nil, nil)
	if st != http.StatusOK {
		if users, ok := body["users"].([]any); !ok || len(users) == 0 {
			t.Fatalf("api key call: %d %v", st, body)
		}
	}
	// The key cannot change credentials or manage keys.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/me/password", apiToken,
		map[string]string{"current_password": "x", "new_password": "x"}, nil)
	if st != http.StatusForbidden && st != http.StatusUnauthorized && st != http.StatusBadRequest {
		t.Fatalf("api key must not change credentials: %d %v", st, body)
	}

	// Revocation kills it immediately.
	st, _, _ = s.apiReq(t, "DELETE", "/api/v1/org/api-keys/"+keyID, token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("revoke: %d", st)
	}
	st, _, _ = s.apiReq(t, "GET", "/api/v1/org/users", apiToken, nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("revoked key must be dead: %d", st)
	}
	// And sessions still work.
	if st, _, _ = s.apiReq(t, "GET", "/api/v1/me", token, nil, nil); st != http.StatusOK {
		t.Fatalf("session broken: %d", st)
	}
}
