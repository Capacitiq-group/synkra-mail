// The register's §12 feature-register and §13 differentiator endpoints:
// signatures, autoresponder, routing rules, mail trace, deliverability,
// health, reliability, webhooks and API keys (handlers_features.go).
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/store"
)

// ---- signatures (§12b) ----

func (s *Server) handleListSignatures(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	sigs, err := store.ListSignatures(r.Context(), s.Store.Pool, ac.User.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(sigs))
	for _, g := range sigs {
		out = append(out, map[string]any{
			"id":         ids.Format(ids.Signature, g.ID),
			"name":       g.Name,
			"html":       g.HTML,
			"text":       g.Text,
			"is_default": g.IsDefault,
			"created_at": g.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"signatures": out})
}

func (s *Server) handleCreateSignature(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		Name      string `json:"name"`
		HTML      string `json:"html"`
		Text      string `json:"text"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}
	sig := &store.Signature{
		ID: uuid.New(), UserID: ac.User.ID, Name: req.Name,
		HTML: req.HTML, Text: req.Text, IsDefault: req.IsDefault,
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateSignature(r.Context(), tx, sig)
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "limit_exceeded", "a signature with that name exists")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": ids.Format(ids.Signature, sig.ID)})
}

func (s *Server) handleDeleteSignature(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Signature)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.DeleteSignature(r.Context(), tx, id, ac.User.ID)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- autoresponder (§12c) ----

func (s *Server) handleGetAutoresponder(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbID, ok := s.featureMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	ar, err := store.GetAutoresponder(r.Context(), s.Store.Pool, mbID)
	if err != nil {
		// None configured yet: an empty default, not an error.
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false, "subject": "", "body": "",
			"starts_at": nil, "ends_at": nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    ar.Enabled,
		"subject":    ar.Subject,
		"body":       ar.Body,
		"starts_at":  ar.StartsAt,
		"ends_at":    ar.EndsAt,
		"updated_at": ar.UpdatedAt,
	})
}

func (s *Server) handleSetAutoresponder(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbID, ok := s.featureMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		Enabled  bool       `json:"enabled"`
		Subject  string     `json:"subject"`
		Body     string     `json:"body"`
		StartsAt *time.Time `json:"starts_at"`
		EndsAt   *time.Time `json:"ends_at"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if req.Enabled && (req.Subject == "" || req.Body == "") {
		writeError(w, http.StatusBadRequest, "invalid_request", "enabled autoresponders need subject and body")
		return
	}
	ar := &store.Autoresponder{
		ID: uuid.New(), MailboxID: mbID, Enabled: req.Enabled,
		Subject: req.Subject, Body: req.Body, StartsAt: req.StartsAt, EndsAt: req.EndsAt,
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.UpsertAutoresponder(r.Context(), tx, ar)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": req.Enabled})
}

// featureMailboxGuarded: own mailbox, or org admin for shared ones.
func (s *Server) featureMailboxGuarded(w http.ResponseWriter, r *http.Request, ac *authn.AuthContext) (uuid.UUID, bool) {
	mbID, ok := parseIDParam(chi.URLParam(r, "id"), ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox id")
		return uuid.Nil, false
	}
	mb, err := store.GetMailbox(r.Context(), s.Store.Pool, mbID)
	if err != nil || mb.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such mailbox in this organisation")
		return uuid.Nil, false
	}
	if ac.Session.Kind != "api_key" && mb.OwnerUserID != nil && *mb.OwnerUserID != ac.User.ID && !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "own the mailbox or be an org admin")
		return uuid.Nil, false
	}
	return mbID, true
}

// ---- routing rules (§12d) ----

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbID, ok := s.ruleMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	rules, err := store.ListRules(r.Context(), s.Store.Pool, mbID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rules))
	for _, g := range rules {
		out = append(out, ruleView(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func ruleView(g store.MailboxRule) map[string]any {
	var folderID any
	if g.ActionFolderID != nil {
		folderID = ids.Format(ids.Folder, *g.ActionFolderID)
	}
	return map[string]any{
		"id":               ids.Format(ids.Rule, g.ID),
		"position":         g.Position,
		"enabled":          g.Enabled,
		"match_from":       g.MatchFrom,
		"match_subject":    g.MatchSubject,
		"action_folder_id": folderID,
		"action_mark_read": g.ActionMarkRead,
		"created_at":       g.CreatedAt,
	}
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbID, ok := s.ruleMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		MatchFrom      string `json:"match_from"`
		MatchSubject   string `json:"match_subject"`
		ActionFolderID string `json:"action_folder_id"`
		ActionMarkRead bool   `json:"action_mark_read"`
		Enabled        *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if req.MatchFrom == "" && req.MatchSubject == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one condition is required")
		return
	}
	rule := &store.MailboxRule{
		ID: uuid.New(), MailboxID: mbID,
		Enabled:   req.Enabled == nil || *req.Enabled,
		MatchFrom: req.MatchFrom, MatchSubject: req.MatchSubject,
		ActionMarkRead: req.ActionMarkRead,
	}
	if req.ActionFolderID != "" {
		fid, ok := parseIDParam(req.ActionFolderID, ids.Folder)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "bad action_folder_id")
			return
		}
		folder, err := store.GetFolder(r.Context(), s.Store.Pool, fid)
		if err != nil || folder.MailboxID != mbID {
			writeError(w, http.StatusNotFound, "not_found", "no such folder in this mailbox")
			return
		}
		rule.ActionFolderID = &fid
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateRule(r.Context(), tx, rule)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ruleView(*rule))
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbID, ok := s.ruleMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "rid"), ids.Rule)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad rule id")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.DeleteRule(r.Context(), tx, id, mbID)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ruleMailboxGuarded: own mailbox, or org admin for shared ones.
func (s *Server) ruleMailboxGuarded(w http.ResponseWriter, r *http.Request, ac *authn.AuthContext) (uuid.UUID, bool) {
	mbID, ok := parseIDParam(chi.URLParam(r, "id"), ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox id")
		return uuid.Nil, false
	}
	mb, err := store.GetMailbox(r.Context(), s.Store.Pool, mbID)
	if err != nil || mb.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such mailbox in this organisation")
		return uuid.Nil, false
	}
	if ac.Session.Kind != "api_key" && mb.OwnerUserID != nil && *mb.OwnerUserID != ac.User.ID && !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "own the mailbox or be an org admin")
		return uuid.Nil, false
	}
	return mbID, true
}

// ---- mail trace (§12a) ----

func (s *Server) handleMessageTrace(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	msgID, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad message id")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, msgID)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	events, err := store.DeliveryEventsForMessage(r.Context(), s.Store.Pool, msgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		var detail map[string]any
		_ = json.Unmarshal(e.Detail, &detail)
		out = append(out, map[string]any{
			"state":       e.State,
			"occurred_at": e.OccurredAt,
			"detail":      detail,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": ids.Format(ids.Message, msgID),
		"events":     out,
	})
}

// ---- deliverability (§12f), health (§13a), reliability (§13b) ----

func (s *Server) handleOrgDeliverability(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	stats, err := store.GetDeliverabilityStats(r.Context(), s.Store.Pool, ac.User.OrgID, 30)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleOrgHealth(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	h, err := store.GetOrgHealth(r.Context(), s.Store.Pool, ac.User.OrgID, 30)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleOrgReliability(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	rows, err := store.GetReliability(r.Context(), s.Store.Pool, ac.User.OrgID, 14)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": rows, "window_days": 14})
}

// ---- webhooks (§13f) ----

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	hooks, err := store.ListWebhooks(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, webhookView(h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": out})
}

func webhookView(h store.Webhook) map[string]any {
	return map[string]any{
		"id":         ids.Format(ids.Webhook, h.ID),
		"url":        h.URL,
		"events":     h.Events,
		"active":     h.Active,
		"created_at": h.CreatedAt,
	}
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.URL) < 8 ||
		(!startsWith(req.URL, "https://") && !startsWith(req.URL, "http://127.0.0.1") && !startsWith(req.URL, "http://localhost")) {
		writeError(w, http.StatusBadRequest, "invalid_request", "an https webhook url is required (http only for loopback testing)")
		return
	}
	if len(req.Events) == 0 {
		req.Events = []string{"delivered", "bounced", "deferred"}
	}
	for _, e := range req.Events {
		if e != "delivered" && e != "bounced" && e != "deferred" {
			writeError(w, http.StatusBadRequest, "invalid_request", "events must be delivered, bounced or deferred")
			return
		}
	}
	secret := newSecret()
	h := &store.Webhook{
		ID: uuid.New(), OrgID: ac.User.OrgID, URL: req.URL,
		Secret: secret, Events: req.Events, Active: true,
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateWebhook(r.Context(), tx, h)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     ids.Format(ids.Webhook, h.ID),
		"url":    h.URL,
		"events": h.Events,
		// The signing secret is shown exactly once (Section 7.1 discipline).
		"secret": secret,
	})
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Webhook)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad webhook id")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.DeleteWebhook(r.Context(), tx, id, ac.User.OrgID)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleTestWebhook: a signed ping the console uses to verify the endpoint.
func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Webhook)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad webhook id")
		return
	}
	hook, err := store.GetWebhookByID(r.Context(), s.Store.Pool, id)
	if err != nil || hook.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such webhook")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"event": "test", "org_id": ac.User.OrgID, "message_id": nil,
		"sent_at": time.Now().UTC(),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"queued": true,
		"note":   "the test ping is delivered by the worker with X-Synkra-Signature: sha256=<hmac of the body with your secret>",
		"body":   json.RawMessage(payload),
	})
}

// ---- API keys (§13f) ----

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	keys, err := store.ListAPIKeys(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"id":           ids.Format(ids.APIKey, k.ID),
			"name":         k.Name,
			"prefix":       k.Prefix,
			"last_used_at": k.LastUsedAt,
			"expires_at":   k.ExpiresAt,
			"revoked_at":   k.RevokedAt,
			"created_at":   k.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": out})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Expires string `json:"expires_in_days"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}
	ttl := time.Duration(0)
	if days := parseInt(req.Expires); days > 0 {
		ttl = time.Duration(days) * 24 * time.Hour
	}
	token, k, err := s.Auth.CreateAPIKey(r.Context(), ac, req.Name, ttl)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     ids.Format(ids.APIKey, k.ID),
		"name":   k.Name,
		"prefix": k.Prefix,
		"token":  token, // shown exactly once
	})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.APIKey)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad key id")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.RevokeAPIKey(r.Context(), tx, id, ac.User.OrgID)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func newSecret() string { return krypto.NewToken() }

func startsWith(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }

func parseInt(s string) int { n, _ := strconv.Atoi(s); return n }
