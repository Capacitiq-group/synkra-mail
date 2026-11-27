// Org-surface and admin-console endpoints that were store capabilities without
// routes (build register §2.1-2.11). Every handler lands on an existing store
// function or a new one in the same package.
package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func orgAdminOnly(ac *authn.AuthContext) bool {
	return ac.User.Role == "owner" || ac.User.Role == "admin"
}

// 2.5 — change own password (webmail settings).
func (s *Server) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "current_password and new_password required")
		return
	}
	if err := s.Auth.ChangeOwnPassword(r.Context(), ac, req.CurrentPassword, req.NewPassword); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "other_sessions_revoked": true})
}

// 2.6 — list the org's users (admin console + webmail).
func (s *Server) handleListOrgUsers(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	users, err := store.ListUsersByOrg(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(users))
	for i := range users {
		out = append(out, userView(&users[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) orgUserTarget(w http.ResponseWriter, r *http.Request, ac *authn.AuthContext) (*model.User, uuid.UUID, bool) {
	uid, ok := parseIDParam(chi.URLParam(r, "id"), ids.User)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad user id")
		return nil, uuid.Nil, false
	}
	target, err := store.GetUser(r.Context(), s.Store.Pool, uid)
	if err != nil || target.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such user in this organisation")
		return nil, uuid.Nil, false
	}
	return target, uid, true
}

// 2.7 — suspend (and resume) a user of the org.
func (s *Server) handleSuspendOrgUser(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	target, uid, ok := s.orgUserTarget(w, r, ac)
	if !ok {
		return
	}
	if target.ID == ac.User.ID {
		writeError(w, http.StatusBadRequest, "invalid_request", "cannot suspend yourself")
		return
	}
	if target.IsPlatformAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "staff accounts are managed by the platform")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetUserStatus(r.Context(), tx, uid, "suspended"); err != nil {
			return err
		}
		if err := store.RevokeAllUserSessions(r.Context(), tx, uid); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"user.suspend", "user", uid.String(), "", nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "suspended"})
}

func (s *Server) handleResumeOrgUser(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	_, uid, ok := s.orgUserTarget(w, r, ac)
	if !ok {
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetUserStatus(r.Context(), tx, uid, "active"); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"user.resume", "user", uid.String(), "", nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "active"})
}

// 2.8 — revoke all sessions of a user (compromised-account playbook).
func (s *Server) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	target, uid, ok := s.orgUserTarget(w, r, ac)
	if !ok {
		return
	}
	_ = target
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.RevokeAllUserSessions(r.Context(), tx, uid); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"user.revoke_sessions", "user", uid.String(), "", nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// 2.9 — shared-mailbox members (model now; the shared-mailbox feature is P2).
func (s *Server) sharedMailboxGuarded(w http.ResponseWriter, r *http.Request, ac *authn.AuthContext) (*model.Mailbox, uuid.UUID, bool) {
	mbID, ok := parseIDParam(chi.URLParam(r, "id"), ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox id")
		return nil, uuid.Nil, false
	}
	mb, err := store.GetMailbox(r.Context(), s.Store.Pool, mbID)
	if err != nil || mb.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such mailbox in this organisation")
		return nil, uuid.Nil, false
	}
	return mb, mbID, true
}

func (s *Server) handleListSharedMailboxMembers(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	_, mbID, ok := s.sharedMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	members, err := store.ListMembers(r.Context(), s.Store.Pool, mbID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{
			"user_id":    ids.Format(ids.User, m.User.ID),
			"email":      m.User.Email,
			"role":       m.Member.Role,
			"created_at": m.Member.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

func (s *Server) handleAddSharedMailboxMember(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	mb, mbID, ok := s.sharedMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	uid, uok := parseIDParam(req.UserID, ids.User)
	if !uok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad user_id")
		return
	}
	target, err := store.GetUser(r.Context(), s.Store.Pool, uid)
	if err != nil || target.OrgID != ac.User.OrgID {
		writeError(w, http.StatusNotFound, "not_found", "no such user in this organisation")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "owner" && req.Role != "member" && req.Role != "viewer" {
		writeError(w, http.StatusBadRequest, "invalid_request", "role must be owner, member or viewer")
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.AddMember(r.Context(), tx, &model.MailboxMember{
			ID: uuid.New(), MailboxID: mbID, UserID: uid, Role: req.Role,
		}); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"mailbox.member_add", "mailbox", mbID.String(), req.Role, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"mailbox_id": ids.Format(ids.Mailbox, mb.ID),
		"user_id":    ids.Format(ids.User, uid),
		"role":       req.Role,
	})
}

func (s *Server) handleRemoveSharedMailboxMember(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	_, mbID, ok := s.sharedMailboxGuarded(w, r, ac)
	if !ok {
		return
	}
	uid, uok := parseIDParam(r.URL.Query().Get("user_id"), ids.User)
	if !uok {
		writeError(w, http.StatusBadRequest, "invalid_request", "user_id query parameter required")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.RemoveMember(r.Context(), tx, mbID, uid); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"mailbox.member_remove", "mailbox", mbID.String(), "", nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// 2.11 — list and release quarantine.
func (s *Server) handleListQuarantine(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	rows, err := store.ListQuarantine(r.Context(), s.Store.Pool, ac.User.OrgID, 200)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, v := range rows {
		out = append(out, map[string]any{
			"id":         ids.Format(ids.Quarantine, v.Row.ID),
			"reason":     v.Row.Reason,
			"score":      v.Row.Score,
			"subject":    v.Subject,
			"snippet":    v.Snippet,
			"expires_at": v.Row.ExpiresAt,
			"created_at": v.Row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"quarantine": out})
}

func (s *Server) handleReleaseQuarantine(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if !orgAdminOnly(ac) {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	qid, ok := parseIDParam(chi.URLParam(r, "id"), ids.Quarantine)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad quarantine id")
		return
	}
	var hadMessage bool
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		var err error
		hadMessage, err = store.ReleaseQuarantine(r.Context(), tx, qid, ac.User.OrgID)
		if err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"quarantine.release", "quarantine", qid.String(), "", nil)
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such quarantine row")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"released":    true,
		"had_message": hadMessage,
		"note":        "message re-injection lands with the spam verdict engines (hook points only, disabled by design for the pilot)",
	})
}

// 2.10 — attachments: the list plus the download route.
func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
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
	atts, err := store.ListAttachments(r.Context(), s.Store.Pool, msgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(atts))
	for _, a := range atts {
		out = append(out, map[string]any{
			"id":            ids.Format(ids.Attachment, a.ID),
			"filename":      a.Filename,
			"content_type":  a.ContentType,
			"size_bytes":    a.SizeBytes,
			"is_inline":     a.IsInline,
			"content_id":    a.ContentID,
			"download_path": "/api/v1/messages/" + ids.Format(ids.Message, msgID) + "/attachments/" + ids.Format(ids.Attachment, a.ID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": out})
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	msgID, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad message id")
		return
	}
	attID, ok := parseIDParam(chi.URLParam(r, "att_id"), ids.Attachment)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad attachment id")
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
	att, err := store.GetAttachment(r.Context(), s.Store.Pool, attID)
	if err != nil || att.MessageID != msgID {
		writeError(w, http.StatusNotFound, "not_found", "no such attachment on this message")
		return
	}
	rc, err := s.Pipeline.Blobs.Open(r.Context(), att.BlobSHA256)
	if err != nil {
		mapError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", att.ContentType)
	if att.Filename != "" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(att.Filename, "\"", "")+"\"")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(readAllB(rc))
}
