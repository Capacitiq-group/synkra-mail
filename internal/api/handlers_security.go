// handlers_security.go: the 27 November 2026 audit remediation surface —
// MFA enrolment, session management, e-mail verification, suppression
// management, owner-side mailbox resume, and staged DKIM rotation.
package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/store"
)

// ---- small helpers for the security surface ----

// sealPrivatePEM seals DKIM signing material with the configured at-rest key
// (audit item 4). Refuses to store plaintext in prod.
func sealPrivatePEM(s *Server, privPEM string) (string, error) {
	return krypto.SealedString(s.Cfg.KMSKeyBytes(), privPEM)
}

func nowStamp() string { return time.Now().Format("20060102") }

// ---- MFA (TOTP) — audit item 3 ----

func (s *Server) handleMFAEnrol(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	secret, url, err := s.Auth.EnrolTOTP(r.Context(), ac)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"secret":       secret,
		"otpauth_url":  url,
		"instructions": "add the secret to your authenticator app, then POST /me/mfa/activate with a current code to enable it",
	})
}

func (s *Server) handleMFAActivate(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "code required")
		return
	}
	if err := s.Auth.ActivateTOTP(r.Context(), ac, strings.TrimSpace(req.Code)); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"mfa_enabled": true})
}

func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "password required")
		return
	}
	if err := s.Auth.DisableTOTP(r.Context(), ac, req.Password); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"mfa_enabled": false})
}

// ---- sessions (missing-features audit: list + revoke) ----

func sessionView(v *store.SessionView, isCurrent bool) map[string]any {
	s := &v.Session
	return map[string]any{
		"id":         ids.Format(ids.Session, s.ID),
		"kind":       s.Kind,
		"ip":         s.IP,
		"user_agent": s.UserAgent,
		"created_at": s.CreatedAt,
		"expires_at": s.ExpiresAt,
		"current":    isCurrent,
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	rows, err := store.ListSessionsByUser(r.Context(), s.Store.Pool, ac.User.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, sessionView(&rows[i], rows[i].Session.ID == ac.Session.ID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Session)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad session id")
		return
	}
	if id == ac.Session.ID {
		// Revoking the current session is logout — use the logout route.
		writeError(w, http.StatusBadRequest, "invalid_request", "use /auth/logout to end the current session")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.RevokeSessionForUser(r.Context(), tx, ac.User.ID, id)
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such session")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- e-mail verification (audit item 13) ----

func (s *Server) handleRequestEmailVerification(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	token, err := s.Auth.CreateEmailVerification(r.Context(), ac)
	if err != nil {
		mapError(w, err)
		return
	}
	resp := map[string]any{"ok": true}
	if s.Cfg.Env != "prod" {
		// Dev/test honesty: there is no outbound mail in dev, so the token is
		// returned directly. Prod delivers it through the platform sender.
		resp["token"] = token
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token required")
		return
	}
	uid, err := s.Auth.VerifyEmailToken(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErrorHint(w, http.StatusBadRequest, "invalid_token", "verification token is unknown, used or expired",
				"request a new verification e-mail")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user_id": ids.Format(ids.User, uid)})
}

// ---- suppression management (audit item 18) ----

func (s *Server) handleListSuppression(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	rows, err := store.ListSuppression(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"email":      row.Email,
			"reason":     row.Reason,
			"created_at": row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"suppression": out})
}

func (s *Server) handleDeleteSuppression(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	email := strings.TrimSpace(chi.URLParam(r, "email"))
	if email == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email required")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.DeleteSuppression(r.Context(), tx, ac.User.OrgID, email); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"suppression.delete", "org", ac.User.OrgID.String(), email, nil)
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "address not on the suppression list")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- owner-side mailbox resume (audit item 18: only platform staff could
// unpause, so a breaker trip waited on a support ticket) ----

func (s *Server) handleResumeOwnMailbox(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.Impersonating() {
		// Resuming a paused mailbox while impersonating would bypass the
		// breaker the platform relies on.
		writeError(w, http.StatusForbidden, "forbidden", "impersonated sessions cannot resume sending")
		return
	}
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	mbID, ok := parseIDParam(chi.URLParam(r, "id"), ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox id")
		return
	}
	mb, err := store.GetMailbox(r.Context(), s.Store.Pool, mbID)
	if err != nil || mb.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such mailbox in this organisation")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &req)
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetMailboxSendStatus(r.Context(), tx, mbID, "active", ""); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false,
			"mailbox.owner_resume", "mailbox", mbID.String(),
			"was "+mb.SendStatus+"; "+req.Reason, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "send_status": "active"})
}

// ---- DKIM rotation (audit item 4: no rotation existed) ----

// handleStageDKIMRotation: generate the next key OUTSIDE any transaction,
// seal it, stage it. The response carries the DNS TXT record the customer
// must publish; activation refuses until it resolves.
func (s *Server) handleStageDKIMRotation(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Domain)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad domain id")
		return
	}
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, id)
	if err != nil || dom.OrgID == nil || *dom.OrgID != ac.User.OrgID {
		writeError(w, http.StatusForbidden, "forbidden", "domain not owned by org")
		return
	}
	if dom.NextSelector != "" {
		writeError(w, http.StatusConflict, "invalid_request", "a rotation is already staged; activate it first")
		return
	}
	privPEM, pubTXT, baseSelector, err := store.GenerateDKIMKeyPair()
	if err != nil {
		mapError(w, err)
		return
	}
	selector := baseSelector + "-" + nowStamp()
	sealed, err := sealPrivatePEM(s, privPEM)
	if err != nil {
		mapError(w, err)
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.StageDKIMRotation(r.Context(), tx, dom.ID, selector, sealed, pubTXT); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"dkim.rotation_staged", "domain", dom.ID.String(), selector, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"selector":        selector,
		"dkim_public_txt": pubTXT,
		"dns_record":      map[string]string{"type": "TXT", "name": selector + "._domainkey." + dom.Name, "value": pubTXT},
		"next":            "publish the TXT record, wait for propagation, then POST /domains/{id}/activate-dkim",
	})
}

// handleActivateDKIMRotation: verify the new selector resolves, then swap.
// Signing never leads DNS, so mail stays verifiable throughout.
func (s *Server) handleActivateDKIMRotation(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Domain)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad domain id")
		return
	}
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, id)
	if err != nil || dom.OrgID == nil || *dom.OrgID != ac.User.OrgID {
		writeError(w, http.StatusForbidden, "forbidden", "domain not owned by org")
		return
	}
	if dom.NextSelector == "" {
		writeError(w, http.StatusConflict, "invalid_request", "no staged rotation")
		return
	}
	txts, lerr := netLookupTXT(r.Context(), dom.NextSelector+"._domainkey."+dom.Name)
	found := false
	if lerr == nil {
		for _, t := range txts {
			if strings.Contains(t, dom.NextPublicTXT[len("v=DKIM1; k=rsa; p="):]) {
				found = true
				break
			}
		}
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{
			"activated": false,
			"problem": map[string]string{
				"what": "the staged selector's TXT record is not visible yet",
				"why":  "activating before DNS propagation would sign mail receivers cannot verify",
				"fix":  "publish TXT " + dom.NextSelector + "._domainkey." + dom.Name + " with the staged value, then retry",
			},
		})
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.ActivateDKIMRotation(r.Context(), tx, dom.ID); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"dkim.rotation_activated", "domain", dom.ID.String(), dom.NextSelector, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activated": true, "selector": dom.NextSelector})
}
