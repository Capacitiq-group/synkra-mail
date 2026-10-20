// API view types + admin portal handlers (Section 11.1).
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// ---- views ----

func userView(u *model.User) map[string]any {
	return map[string]any{
		"id":           ids.Format(ids.User, u.ID),
		"org_id":       ids.Format(ids.Org, u.OrgID),
		"email":        u.Email,
		"display_name": u.DisplayName,
		"role":         u.Role,
		"status":       u.Status,
		"is_staff":     u.IsPlatformAdmin,
		"created_at":   u.CreatedAt,
	}
}

func orgView(o *model.Org) map[string]any {
	return map[string]any{
		"id":              ids.Format(ids.Org, o.ID),
		"name":            o.Name,
		"status":          o.Status,
		"tier":            o.Tier,
		"sending_enabled": o.SendingEnabled,
		"review_status":   o.ReviewStatus,
		"created_at":      o.CreatedAt,
	}
}

func mailboxViews(mbs []model.Mailbox) []map[string]any {
	out := make([]map[string]any, 0, len(mbs))
	for _, m := range mbs {
		out = append(out, map[string]any{
			"id":          ids.Format(ids.Mailbox, m.ID),
			"kind":        m.Kind,
			"name":        m.Name,
			"send_status": m.SendStatus,
			"owner":       m.OwnerUserID != nil,
		})
	}
	return out
}

func folderViews(fs []model.Folder) []map[string]any {
	out := make([]map[string]any, 0, len(fs))
	for _, f := range fs {
		out = append(out, map[string]any{
			"id":           ids.Format(ids.Folder, f.ID),
			"name":         f.Name,
			"special_use":  f.SpecialUse,
			"uid_next":     f.UIDNext,
			"uid_validity": f.UIDValidity,
			"total":        f.TotalMessages,
			"unread":       f.Unread,
		})
	}
	return out
}

func itemView(i *model.MailboxItem) map[string]any {
	return map[string]any{
		"id":         ids.Format(ids.Item, i.ID),
		"folder_id":  ids.Format(ids.Folder, i.FolderID),
		"message_id": ids.Format(ids.Message, i.MessageID),
		"uid":        i.UID,
		"flags":      i.Flags,
		"created_at": i.CreatedAt,
	}
}

func itemViewList(items []store.ItemWithMessage) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, map[string]any{
			"item":    itemView(&items[i].Item),
			"message": messageView(&items[i].Message),
		})
	}
	return out
}

func messageView(m *model.Message) map[string]any {
	return map[string]any{
		"id":               ids.Format(ids.Message, m.ID),
		"thread_id":        optID(ids.Thread, m.ThreadID),
		"direction":        m.Direction,
		"from":             map[string]string{"name": m.FromName, "email": m.FromEmail},
		"to":               m.ToAddrs,
		"cc":               m.CcAddrs,
		"subject":          m.Subject,
		"snippet":          m.Snippet,
		"has_attachments":  m.HasAttachments,
		"size_bytes":       m.SizeBytes,
		"internal_date":    m.InternalDate,
		"spam_score":       m.SpamScore,
		"tracking_enabled": m.TrackingEnabled,
		"auth_results":     m.AuthResults,
	}
}

func optID(prefix string, u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return ids.Format(prefix, *u)
}

func domainViews(ds []model.Domain) []map[string]any {
	out := make([]map[string]any, 0, len(ds))
	for _, d := range ds {
		out = append(out, map[string]any{
			"id":            ids.Format(ids.Domain, d.ID),
			"name":          d.Name,
			"status":        d.Status,
			"is_platform":   d.IsPlatform,
			"dkim_selector": d.DKIMSelector,
			"verified_at":   d.VerifiedAt,
		})
	}
	return out
}

// ---- admin portal (staff only — Section 11.1) ----

func (s *Server) handleAdminListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := store.ListOrgs(r.Context(), s.Store.Pool, 200)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(orgs))
	for i := range orgs {
		o := &orgs[i]
		usage, _ := store.OrgUsageBytes(r.Context(), s.Store.Pool, o.ID)
		jobs, _ := store.CountSentSince(r.Context(), s.Store.Pool, o.ID, nil, time.Now().Add(-24*time.Hour))
		out = append(out, map[string]any{
			"org":             orgView(o),
			"usage_bytes":     usage,
			"sent_24h":        jobs,
			"sending_enabled": o.SendingEnabled,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": out})
}

func (s *Server) handleAdminCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		OwnerEmail string `json:"owner_email"`
		OwnerName  string `json:"owner_name"`
		Password   string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.OwnerEmail == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "name, owner_email, password required")
		return
	}
	plan, err := store.GetPlanBySlug(r.Context(), s.Store.Pool, "business")
	if err != nil {
		mapError(w, err)
		return
	}
	org, user, err := s.Auth.ProvisionOrg(r.Context(), req.Name, strings.ToLower(req.OwnerEmail),
		req.OwnerName, krypto.HashPassword(req.Password), s.Cfg.PlatformDomain, plan)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"org": orgView(org), "user": userView(user)})
}

// handleAdminApproveOrg: the review-queue action (Section 7.5) — approves the
// org and enables sending (payment recorded externally at v1).
func (s *Server) handleAdminApproveOrg(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Org)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
		Paid   bool   `json:"paid"`
	}
	_ = decodeJSON(r, &req)
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetOrgReviewStatus(r.Context(), tx, id, "approved"); err != nil {
			return err
		}
		if err := store.SetOrgTier(r.Context(), tx, id, "verified"); err != nil {
			return err
		}
		if req.Paid {
			sub, err := store.GetActiveSubscription(r.Context(), tx, id)
			if err == nil {
				now := time.Now().AddDate(0, 1, 0)
				if err := store.SetSubscriptionStatus(r.Context(), tx, sub.ID, "active", &now); err != nil {
					return err
				}
			}
		}
		if err := store.SetOrgSending(r.Context(), tx, id, true, ""); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, nil, &ac.User.ID, false,
			"org.approve", "org", id.String(), req.Reason, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminSuspendOrg(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Org)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &req)
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetOrgStatus(r.Context(), tx, id, "suspended"); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, nil, &ac.User.ID, false,
			"org.suspend", "org", id.String(), req.Reason, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminResumeOrg(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Org)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &req)
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetOrgStatus(r.Context(), tx, id, "active"); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, nil, &ac.User.ID, false,
			"org.resume", "org", id.String(), req.Reason, nil)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminImpersonate: 60-minute audited session with guardrails (Section 11.2).
func (s *Server) handleAdminImpersonate(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		UserID string `json:"user_id"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Reason == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "user_id and reason required")
		return
	}
	uid, ok := parseIDParam(req.UserID, ids.User)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad user_id")
		return
	}
	token, err := s.Auth.Impersonate(r.Context(), ac.User, uid, req.Reason, remoteIP(r), r.UserAgent())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":       token,
		"ttl_minutes": 60,
		"guardrails":  []string{"no credential changes", "mail-body reads audited", "banner shown in webmail"},
	})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	limit := 200
	rows, err := store.ListAudit(r.Context(), s.Store.Pool, limit)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":           ids.Format(ids.Audit, row.ID),
			"actor":        optID(ids.User, row.ActorUserID),
			"admin":        optID(ids.User, row.AdminUserID),
			"impersonated": row.Impersonated,
			"action":       row.Action,
			"target_type":  row.TargetType,
			"target_id":    row.TargetID,
			"reason":       row.Reason,
			"meta":         json.RawMessage(row.Meta),
			"created_at":   row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}

func (s *Server) handleAdminQueue(w http.ResponseWriter, r *http.Request) {
	depth, err := store.QueueDepth(r.Context(), s.Store.Pool)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue_depth": depth})
}

func (s *Server) handleAdminOrgJobs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Org)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad id")
		return
	}
	jobs, err := store.ListJobsByOrg(r.Context(), s.Store.Pool, id, 100)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, map[string]any{
			"id":              ids.Format(ids.Delivery, j.ID),
			"envelope_from":   j.EnvelopeFrom,
			"envelope_to":     j.EnvelopeTo,
			"status":          j.Status,
			"attempts":        j.Attempts,
			"next_attempt_at": j.NextAttemptAt,
			"last_code":       j.LastCode,
			"last_msg":        j.LastMsg,
			"is_dsn":          j.IsDSN,
			"created_at":      j.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	depth, _ := store.QueueDepth(r.Context(), s.Store.Pool)
	writeJSON(w, http.StatusOK, map[string]any{
		"queue_depth": depth,
		"ts":          time.Now(),
	})
}
