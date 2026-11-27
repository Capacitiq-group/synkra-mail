// Mail + account handlers (Section 15.1 surface).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/outbound"
	"synkra.email/mail/internal/store"
)

// ---- auth ----

type loginReq struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	UserAgent string `json:"-"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "email and password required")
		return
	}
	ac, token, err := s.Auth.Login(r.Context(), req.Email, req.Password, remoteIP(r), r.UserAgent())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  userView(ac.User),
		"org":   orgView(ac.Org),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if err := s.Auth.Logout(r.Context(), ac); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbs, _ := store.MailboxesForUser(r.Context(), s.Store.Pool, ac.User.ID)
	out := map[string]any{
		"user":            userView(ac.User),
		"org":             orgView(ac.Org),
		"impersonated":    ac.Impersonating(),
		"impersonated_by": ac.Session.ImpersonatedBy,
		"mailboxes":       mailboxViews(mbs),
	}
	writeJSON(w, http.StatusOK, out)
}

func remoteIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.Split(fwd, ",")[0]
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}

// ---- app passwords (owner decision: max 10, 180-day expiry) ----

func (s *Server) handleListAppPasswords(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	list, err := store.ListAppPasswords(r.Context(), s.Store.Pool, ac.User.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, ap := range list {
		out = append(out, map[string]any{
			"id":           ids.Format(ids.AppPassword, ap.ID),
			"name":         ap.Name,
			"prefix":       ap.Prefix,
			"expires_at":   ap.ExpiresAt,
			"last_used_at": ap.LastUsedAt,
			"revoked_at":   ap.RevokedAt,
			"created_at":   ap.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_passwords": out})
}

func (s *Server) handleCreateAppPassword(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}
	secret, ap, err := s.Auth.CreateAppPassword(r.Context(), ac, req.Name)
	if err != nil {
		mapError(w, err)
		return
	}
	// The secret is shown exactly once (Section 7.1).
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         ids.Format(ids.AppPassword, ap.ID),
		"name":       ap.Name,
		"prefix":     ap.Prefix,
		"secret":     secret,
		"expires_at": ap.ExpiresAt,
	})
}

func (s *Server) handleRevokeAppPassword(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.AppPassword)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	if err := s.Auth.RevokeAppPassword(r.Context(), ac, id); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- folders ----

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mb, ok, err := s.resolveMailboxParam(r, ac)
	if err != nil {
		mapError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "mailbox_id required")
		return
	}
	folders, err := store.ListFolders(r.Context(), s.Store.Pool, mb.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folderViews(folders)})
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		MailboxID string `json:"mailbox_id"`
		Name      string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}
	mbID, ok := parseIDParam(req.MailboxID, ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox_id")
		return
	}
	if acc, err := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, mbID); err != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access to mailbox")
		return
	}
	f := &model.Folder{ID: uuid.New(), MailboxID: mbID, Name: req.Name}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateFolder(r.Context(), tx, f)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"folder": folderViews([]model.Folder{*f})[0]})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Folder)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	folder, err := store.GetFolder(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, folder.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.DeleteFolder(r.Context(), tx, id)
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "forbidden", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- messages ----

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	folderID, ok := parseIDParam(r.URL.Query().Get("folder_id"), ids.Folder)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "folder_id required")
		return
	}
	folder, err := store.GetFolder(r.Context(), s.Store.Pool, folderID)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, folder.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	// Cursor pagination (register §1.8): `limit` (default 50, max 200) newest
	// items; the next page is fetched with before_uid = the smallest uid of
	// this page. The folder is never loaded whole just to keep the last 50.
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be 1..200")
			return
		}
		limit = n
	}
	var beforeUID uint32
	if v := r.URL.Query().Get("before_uid"); v != "" {
		n, perr := strconv.ParseUint(v, 10, 32)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "before_uid must be a uid")
			return
		}
		beforeUID = uint32(n)
	}
	items, err := store.ListItemsPage(r.Context(), s.Store.Pool, folderID, beforeUID, limit)
	if err != nil {
		mapError(w, err)
		return
	}
	out := map[string]any{
		"folder":   folderViews([]model.Folder{*folder})[0],
		"items":    itemViewList(items),
		"limit":    limit,
		"has_more": len(items) == limit,
	}
	if len(items) > 0 {
		out["next_before_uid"] = items[0].Item.UID // smallest uid of the page
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	// Mail-body access during impersonation is audited separately (Section 11.2).
	if ac.Impersonating() {
		_ = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			adminID := ac.Session.ImpersonatedBy
			return store.RecordAudit(r.Context(), tx, &ac.User.ID, adminID, true,
				"message.read", "message", id.String(), "", nil)
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": messageView(&full.Message),
		"item":    itemView(&full.Item),
		"folder":  folderViews([]model.Folder{full.Folder})[0],
	})
}

func (s *Server) handleGetMessageRaw(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	rc, err := s.Pipeline.Blobs.Open(r.Context(), full.Message.BlobSHA256)
	if err != nil {
		mapError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "message/rfc822")
	_, _ = w.Write([]byte{}) // headers flushed by WriteHeader via Content-Type
	if _, err := w.Write(readAllB(rc)); err != nil {
		return
	}
}

func (s *Server) handleSetFlags(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	var req struct {
		Flags []string `json:"flags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	if full.Item.ID == uuid.Nil {
		writeError(w, http.StatusNotFound, "not_found", "message not in any folder")
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.UpdateItemFlags(r.Context(), tx, full.Item.FolderID, full.Item.UID, req.Flags)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": req.Flags})
}

func (s *Server) handleMoveMessage(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	var req struct {
		FolderID string `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	destID, ok := parseIDParam(req.FolderID, ids.Folder)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad folder_id")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	dest, err := store.GetFolder(r.Context(), s.Store.Pool, destID)
	if err != nil {
		mapError(w, err)
		return
	}
	if dest.MailboxID != full.Message.MailboxID {
		writeError(w, http.StatusBadRequest, "invalid_request", "destination folder in another mailbox")
		return
	}
	var newUID uint32
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		var err error
		newUID, err = store.MoveItem(r.Context(), tx, full.Item.FolderID, dest.ID, full.Item.UID)
		return err
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder_id": ids.Format(ids.Folder, dest.ID), "uid": newUID})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	if full.Item.ID == uuid.Nil {
		writeError(w, http.StatusNotFound, "not_found", "message not in any folder")
		return
	}

	// Trash semantics (Section 4.4): first delete → Trash; delete in Trash → expunge.
	var err2 error
	if full.Folder.SpecialUse == model.FolderTrash {
		err2 = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			_, err := store.ExpungeItem(r.Context(), tx, full.Item.FolderID, full.Item.UID)
			return err
		})
	} else {
		trash, terr := store.GetFolderBySpecialUse(r.Context(), s.Store.Pool, full.Message.MailboxID, model.FolderTrash)
		if terr != nil {
			mapError(w, terr)
			return
		}
		err2 = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			_, err := store.MoveItem(r.Context(), tx, full.Item.FolderID, trash.ID, full.Item.UID)
			return err
		})
	}
	if err2 != nil {
		mapError(w, err2)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- send (Section 5.2 submission via API) ----

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	// Cap the request one message above the plan's message cap: the JSON
	// envelope and its attachments must fit both here and on the wire.
	r.Body = http.MaxBytesReader(w, r.Body, s.Cfg.MaxMessageBytes+(1<<20))
	body, err := readBody(r)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "message_too_large", "request body exceeds the message size limit")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		From        string          `json:"from"`
		To          []mimeutil.Addr `json:"to"`
		Cc          []mimeutil.Addr `json:"cc"`
		Bcc         []mimeutil.Addr `json:"bcc"`
		Subject     string          `json:"subject"`
		Text        string          `json:"text"`
		HTML        string          `json:"html"`
		InReplyTo   string          `json:"in_reply_to"`
		Track       bool            `json:"track"`
		Newsletter  bool            `json:"newsletter"`
		Attachments []mimeutil.Att  `json:"attachments"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	// Idempotency-Key (register §1.9): a retried send replays the first
	// response instead of queueing the message twice.
	key := r.Header.Get("Idempotency-Key")
	if status, resp, replay, ierr := s.idempotentBegin(r.Context(), ac, key, "send", body); ierr != nil {
		mapError(w, ierr)
		return
	} else if replay {
		s.replayJSON(w, status, resp)
		return
	}
	if req.From == "" {
		// Default: the user's primary address.
		primary, err := s.primaryAddress(r.Context(), ac)
		if err != nil {
			mapError(w, err)
			return
		}
		req.From = primary
	}
	in := outbound.ComposeInput{
		From: mimeutil.Addr{Email: req.From, Name: ac.User.DisplayName},
		To:   req.To, Cc: req.Cc, Bcc: req.Bcc,
		Subject: req.Subject, Text: req.Text, HTML: req.HTML,
		InReplyTo: req.InReplyTo, Attachments: req.Attachments, Track: req.Track,
		Newsletter: req.Newsletter,
	}
	msgID, recipients, err := s.Pipeline.ComposeAndQueue(r.Context(), ac.User.ID, ac.User.OrgID, in)
	if err != nil {
		mapError(w, err)
		return
	}
	resp := map[string]any{
		"message_id": ids.Format(ids.Message, msgID),
		"recipients": recipients,
	}
	if ferr := s.idempotentFinish(r.Context(), ac, key, "send", body, http.StatusAccepted, resp); ferr != nil {
		mapError(w, ferr)
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) primaryAddress(ctx interface {
	Deadline() (time.Time, bool)
	Done() <-chan struct{}
	Err() error
	Value(any) any
}, ac *authn.AuthContext) (string, error) {
	addrs, err := store.ListAddressesByOrg(ctx, s.Store.Pool, ac.User.OrgID)
	if err != nil {
		return "", err
	}
	domains, err := store.ListDomainsByOrg(ctx, s.Store.Pool, ac.User.OrgID)
	if err != nil {
		return "", err
	}
	domName := map[uuid.UUID]string{}
	for _, d := range domains {
		domName[d.ID] = d.Name
	}
	// Platform-domain addresses (org_id NULL) resolve through the platform row.
	if pd, perr := store.GetPlatformDomain(ctx, s.Store.Pool); perr == nil {
		domName[pd.ID] = pd.Name
	}
	for _, a := range addrs {
		mb, merr := store.GetMailbox(ctx, s.Store.Pool, a.MailboxID)
		if merr == nil && mb.OwnerUserID != nil && *mb.OwnerUserID == ac.User.ID && a.IsPrimary {
			return a.Local + "@" + domName[a.DomainID], nil
		}
	}
	// Fall back to the login email's local part on the platform domain.
	return ac.User.Email, nil
}

// ---- search / storage / tracking view ----

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	q := r.URL.Query().Get("q")
	mailboxes, err := store.MailboxesForUser(r.Context(), s.Store.Pool, ac.User.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	var all []store.ItemWithMessage
	for _, mb := range mailboxes {
		if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, mb.ID); aerr != nil || !acc {
			continue
		}
		items, serr := store.SearchMessages(r.Context(), s.Store.Pool, mb.ID, q, 25)
		if serr != nil {
			continue
		}
		all = append(all, items...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": itemViewList(all)})
}

func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	qd := &ingest.Deps{Store: s.Store, Blobs: s.Pipeline.Blobs, Cfg: s.Cfg}
	state, err := qd.CheckQuota(r.Context(), ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"used_bytes":       state.UsedBytes,
		"included_bytes":   state.IncludedBytes,
		"grace_bytes":      state.GraceBytes,
		"used_percent":     state.UsedPercent,
		"full":             state.Full,
		"beyond_grace":     state.BeyondGrace,
		"never_autodelete": true,
	})
}

func (s *Server) handleMessageTracking(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	full, err := store.GetMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, full.Message.MailboxID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	v, err := store.TrackingEventsForMessage(r.Context(), s.Store.Pool, id)
	if err != nil {
		mapError(w, err)
		return
	}
	// The three-label honesty model (Section 9.2): every number ships with its label.
	writeJSON(w, http.StatusOK, map[string]any{
		"tracking_enabled": full.Message.TrackingEnabled,
		"pixels":           v.Pixels,
		"opens":            v.OpenEvents,
		"links":            v.Links,
		"clicks":           v.Clicks,
	})
}

// ---- threads (shared inbox: internal note + assignment — Section 10.3) ----

// threadActorCanAccess checks membership of the mailboxes the thread's messages
// live in (register §1.4): org membership alone is a privilege leak across
// mailboxes within one org.
func (s *Server) threadActorCanAccess(ctx context.Context, ac *authn.AuthContext, threadID uuid.UUID) (bool, error) {
	mbs, err := store.ThreadMailboxIDs(ctx, s.Store.Pool, threadID)
	if err != nil {
		return false, err
	}
	for _, mbID := range mbs {
		if acc, aerr := store.MailboxAccess(ctx, s.Store.Pool, ac.User.ID, mbID); aerr == nil && acc {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) handleThreadNote(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Thread)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	acc, err := s.threadActorCanAccess(r.Context(), ac, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access to this thread's mailbox")
		return
	}
	// Internal notes are never transmitted — they live on the thread row only.
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		_, err := tx.Exec(r.Context(), `update mail.thread set note=$2, updated_at=now() where id=$1 and org_id=$3`,
			id, req.Note, ac.User.OrgID)
		return err
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleThreadAssign(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Thread)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	var req struct {
		AssigneeID string `json:"assignee_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	assignee, ok := parseIDParam(req.AssigneeID, ids.User)
	if req.AssigneeID != "" && !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad assignee_id")
		return
	}
	acc, err := s.threadActorCanAccess(r.Context(), ac, id)
	if err != nil {
		mapError(w, err)
		return
	}
	if !acc {
		writeError(w, http.StatusForbidden, "forbidden", "no access to this thread's mailbox")
		return
	}
	// The assignee must be able to see the thread too (register §1.4).
	if req.AssigneeID != "" {
		mbs, merr := store.ThreadMailboxIDs(r.Context(), s.Store.Pool, id)
		if merr != nil {
			mapError(w, merr)
			return
		}
		ok := false
		for _, mbID := range mbs {
			if aacc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, assignee, mbID); aerr == nil && aacc {
				ok = true
				break
			}
		}
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", "assignee is not a member of this thread's mailbox")
			return
		}
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		_, err := tx.Exec(r.Context(), `update mail.thread set assignee_id=$2, updated_at=now() where id=$1 and org_id=$3`,
			id, optUUIDPtr(assignee, ok && req.AssigneeID != ""), ac.User.OrgID)
		return err
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- addresses / domains ----

func (s *Server) handleListAddresses(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	addrs, err := store.ListAddressesByOrg(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	domains, _ := store.ListDomainsByOrg(r.Context(), s.Store.Pool, ac.User.OrgID)
	domName := map[uuid.UUID]string{}
	for _, d := range domains {
		domName[d.ID] = d.Name
	}
	out := make([]map[string]any, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, map[string]any{
			"id":             ids.Format(ids.Address, a.ID),
			"address":        a.Local + "@" + domName[a.DomainID],
			"mailbox_id":     ids.Format(ids.Mailbox, a.MailboxID),
			"kind":           a.Kind,
			"is_primary":     a.IsPrimary,
			"allow_send_as":  a.AllowSendAs,
			"forward_target": a.ForwardTarget,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": out})
}

func (s *Server) handleCreateAddress(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		Local         string `json:"local"`
		DomainID      string `json:"domain_id"`
		MailboxID     string `json:"mailbox_id"`
		Kind          string `json:"kind"` // alias | forward | user
		ForwardTarget string `json:"forward_target"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Local == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "local required")
		return
	}
	// Idempotency-Key (register §1.9): alias creation replays instead of
	// double-creating on a retried submit.
	key := r.Header.Get("Idempotency-Key")
	if status, resp, replay, ierr := s.idempotentBegin(r.Context(), ac, key, "addresses", body); ierr != nil {
		mapError(w, ierr)
		return
	} else if replay {
		s.replayJSON(w, status, resp)
		return
	}
	if req.Kind == "" {
		req.Kind = "alias"
	}
	// Plan limits (aliases are address rows, Section 4.1).
	plan, err := store.GetPlanByID(r.Context(), s.Store.Pool, ac.Org.PlanID)
	if err != nil {
		mapError(w, err)
		return
	}
	if req.Kind == "alias" {
		n, _ := store.CountAddressesByOrg(r.Context(), s.Store.Pool, ac.User.OrgID, "alias")
		if n >= plan.AliasLimit {
			writeError(w, http.StatusBadRequest, "limit_exceeded", "alias limit reached")
			return
		}
	}
	domID, ok := parseIDParam(req.DomainID, ids.Domain)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad domain_id")
		return
	}
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, domID)
	if err != nil || dom.OrgID == nil || *dom.OrgID != ac.User.OrgID {
		writeError(w, http.StatusForbidden, "forbidden", "domain not owned by org")
		return
	}
	mbID, ok := parseIDParam(req.MailboxID, ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox_id")
		return
	}
	if acc, aerr := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, mbID); aerr != nil || !acc {
		writeError(w, http.StatusForbidden, "forbidden", "mailbox not accessible")
		return
	}
	a := &model.Address{
		ID: uuid.New(), OrgID: ac.User.OrgID, DomainID: domID, MailboxID: mbID,
		Local: req.Local, Kind: req.Kind, ForwardTarget: req.ForwardTarget,
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateAddress(r.Context(), tx, a)
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "limit_exceeded", "address already exists")
			return
		}
		mapError(w, err)
		return
	}
	resp := map[string]any{"id": ids.Format(ids.Address, a.ID)}
	if ferr := s.idempotentFinish(r.Context(), ac, key, "addresses", body, http.StatusCreated, resp); ferr != nil {
		mapError(w, ferr)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleDeleteAddress(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Address)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.DeleteAddress(r.Context(), tx, id, ac.User.OrgID)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	domains, err := store.ListDomainsByOrg(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domainViews(domains)})
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &req); err != nil || !strings.Contains(req.Name, ".") {
		writeError(w, http.StatusBadRequest, "invalid_request", "valid domain name required")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if status, resp, replay, ierr := s.idempotentBegin(r.Context(), ac, key, "domains", body); ierr != nil {
		mapError(w, ierr)
		return
	} else if replay {
		s.replayJSON(w, status, resp)
		return
	}
	d := &model.Domain{ID: uuid.New(), OrgID: &ac.User.OrgID, Name: strings.ToLower(strings.TrimSpace(req.Name)), Status: "pending"}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateDomain(r.Context(), tx, d)
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "limit_exceeded", "domain already registered")
			return
		}
		mapError(w, err)
		return
	}
	resp := map[string]any{
		"domain":          domainViews([]model.Domain{*d})[0],
		"ownership_token": d.OwnershipToken,
		"dkim_selector":   d.DKIMSelector,
		"dkim_public_txt": d.DKIMPublicTXT,
	}
	if ferr := s.idempotentFinish(r.Context(), ac, key, "domains", body, http.StatusCreated, resp); ferr != nil {
		mapError(w, ferr)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleVerifyDomain(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Domain)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, id)
	if err != nil || dom.OrgID == nil || *dom.OrgID != ac.User.OrgID {
		writeError(w, http.StatusForbidden, "forbidden", "domain not owned by org")
		return
	}
	// Ownership check: the customer publishes the token as a TXT record on the domain.
	txts, lerr := netLookupTXT(r.Context(), dom.Name)
	verified := false
	for _, t := range txts {
		if strings.TrimSpace(t) == dom.OwnershipToken {
			verified = true
			break
		}
	}
	if lerr != nil || !verified {
		// DNS doctor honesty (register §1.10): say what is wrong, why, and how
		// to fix it — not just a code.
		what := "ownership TXT record not found on " + dom.Name
		if lerr != nil {
			what = "TXT lookup for " + dom.Name + " failed: " + lerr.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"verified": false,
			"problem": map[string]string{
				"what": what,
				"why":  "domain ownership cannot be proven without the token record, so the platform cannot sign or receive for this domain",
				"fix":  "publish TXT @ " + dom.OwnershipToken + " on " + dom.Name + " (TTL 300-600s), wait for propagation, then retry",
			},
			"hint": "publish TXT " + dom.OwnershipToken + " on " + dom.Name + " then retry",
		})
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.VerifyDomain(r.Context(), tx, dom.ID)
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true})
}

// handleDomainDNS returns expected DNS records — the DNS-doctor data layer (Section 11.3).
func (s *Server) handleDomainDNS(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Domain)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, id)
	if err != nil || (dom.OrgID != nil && *dom.OrgID != ac.User.OrgID && !ac.User.IsPlatformAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": dom.Name,
		"records": []map[string]string{
			{"type": "MX", "name": "@", "value": "10 " + s.Cfg.Hostname, "purpose": "receive"},
			{"type": "TXT", "name": "@", "value": "v=spf1 include:spf." + s.Cfg.PlatformDomain + " ~all", "purpose": "SPF"},
			{"type": "TXT", "name": dom.DKIMSelector + "._domainkey", "value": dom.DKIMPublicTXT, "purpose": "DKIM"},
			{"type": "TXT", "name": "_dmarc", "value": "v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@" + s.Cfg.PlatformDomain, "purpose": "DMARC"},
			// RFC 7489 7.1: reports to an external receiver must be authorised
			// by this record in the customer's zone, or every report is dropped.
			{"type": "TXT", "name": s.Cfg.PlatformDomain + "._domainkey", "value": "v=DMARC1", "purpose": "DMARC report authorisation"},
			{"type": "TXT", "name": "@", "value": dom.OwnershipToken, "purpose": "ownership verification"},
		},
	})
}

// ---- org settings ----

func (s *Server) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	org, err := store.GetOrg(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	sub, _ := store.GetActiveSubscription(r.Context(), s.Store.Pool, org.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"org":          orgView(org),
		"subscription": sub,
	})
}

// handleSetTracking: the per-org toggle — off by default (owner decision).
func (s *Server) handleSetTracking(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		// tracking_started_at: first enable wins — auditable from when
		// tracking actually began (Section 9.4).
		_, err := tx.Exec(r.Context(), `update mail.org set tracking_enabled=$2,
			tracking_started_at = case when $2 and tracking_started_at is null then now() else tracking_started_at end,
			updated_at=now() where id=$1`, ac.User.OrgID, req.Enabled)
		return err
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"tracking_enabled": req.Enabled})
}

// ---- users (org admin invites teammates) ----

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email and password required")
		return
	}
	plan, err := store.GetPlanByID(r.Context(), s.Store.Pool, ac.Org.PlanID)
	if err != nil {
		mapError(w, err)
		return
	}
	n, _ := store.CountMailboxesByOrg(r.Context(), s.Store.Pool, ac.User.OrgID, "user")
	if n >= plan.MailboxLimit {
		writeError(w, http.StatusBadRequest, "limit_exceeded", "mailbox limit reached")
		return
	}
	u := &model.User{
		ID: uuid.New(), OrgID: ac.User.OrgID, Email: strings.ToLower(req.Email),
		PasswordHash: krypto.HashPassword(req.Password), DisplayName: req.DisplayName, Role: "member",
	}
	var mb model.Mailbox
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.CreateUser(r.Context(), tx, u); err != nil {
			return err
		}
		mb = model.Mailbox{ID: uuid.New(), OrgID: ac.User.OrgID, OwnerUserID: &u.ID, Kind: "user", Name: req.Email}
		if err := store.CreateMailbox(r.Context(), tx, &mb); err != nil {
			return err
		}
		// Address on the platform domain (custom-domain addresses are admin-managed).
		pd, derr := store.GetPlatformDomain(r.Context(), tx)
		if derr != nil {
			return derr
		}
		local := req.Email
		if i := strings.Index(local, "@"); i > 0 {
			local = local[:i]
		}
		return store.CreateAddress(r.Context(), tx, &model.Address{
			ID: uuid.New(), OrgID: ac.User.OrgID, DomainID: pd.ID, MailboxID: mb.ID,
			Local: local, Kind: "user", IsPrimary: true,
		})
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "limit_exceeded", "user already exists")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":       userView(u),
		"mailbox_id": ids.Format(ids.Mailbox, mb.ID),
	})
}

// ---- helpers shared by handlers ----

func (s *Server) resolveMailboxParam(r *http.Request, ac *authn.AuthContext) (*model.Mailbox, bool, error) {
	mbIDStr := r.URL.Query().Get("mailbox_id")
	if mbIDStr == "" {
		// Default: the user's own mailbox.
		mbs, err := store.MailboxesForUser(r.Context(), s.Store.Pool, ac.User.ID)
		if err != nil {
			return nil, false, err
		}
		for i := range mbs {
			mb := &mbs[i]
			if mb.Kind == "user" && mb.OwnerUserID != nil && *mb.OwnerUserID == ac.User.ID {
				return mb, true, nil
			}
		}
		return nil, false, errors.New("no mailbox")
	}
	mbID, ok := parseIDParam(mbIDStr, ids.Mailbox)
	if !ok {
		return nil, false, errors.New("bad mailbox_id")
	}
	acc, err := store.MailboxAccess(r.Context(), s.Store.Pool, ac.User.ID, mbID)
	if err != nil || !acc {
		return nil, false, nil
	}
	mb, err := store.GetMailbox(r.Context(), s.Store.Pool, mbID)
	return mb, err == nil, err
}

// parseIDParam enforces the route's expected id type (register §1.1): a msg_ id
// passed where a folder id is required is a bad request, not a lookup miss.
func parseIDParam(s, prefix string) (uuid.UUID, bool) {
	return ids.ParseTyped(s, prefix)
}

func optUUIDPtr(u uuid.UUID, use bool) any {
	if use {
		return u
	}
	return nil
}

// netLookupTXT resolves TXT records with a short timeout (domain verification).
func netLookupTXT(ctx context.Context, domain string) ([]string, error) {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	return resolver.LookupTXT(qctx, domain)
}

// readAllB drains a reader into bytes (raw message download).
func readAllB(rc io.Reader) []byte {
	b, _ := io.ReadAll(rc)
	return b
}

// handleListMailboxes: mailboxes the user can access (own + shared membership).
func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	mbs, err := store.MailboxesForUser(r.Context(), s.Store.Pool, ac.User.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": mailboxViews(mbs)})
}
