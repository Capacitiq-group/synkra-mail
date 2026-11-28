// handlers_migration.go — IMAP migration assistant endpoints (phase 11):
//
//	POST /org/migrations/source-folders   live folder probe (no persistence)
//	POST /org/migrations                  create the job (password sealed)
//	GET  /org/migrations                  job list
//	GET  /org/migrations/{id}             progress + attention list
//	POST /org/migrations/{id}/pause|resume|cancel
//	DELETE /org/migrations/{id}           delete a terminal job
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/migrator"
	"synkra.email/mail/internal/store"
)

func (s *Server) migrationAccess(r *http.Request, idParam string) (*store.MigrationJob, bool) {
	id, ok := parseIDParam(idParam, ids.Migration)
	if !ok {
		return nil, false
	}
	ac := authFrom(r.Context())
	job, err := store.GetMigrationJob(r.Context(), s.Store.Pool, id)
	if err != nil || job.OrgID != ac.User.OrgID {
		return nil, false
	}
	return job, true
}

// handleMigrationProbe: the wizard's folder-mapping step — connect live to
// the source, list folders + counts + suggested destinations. Nothing is
// persisted; the source password is read from the request body only.
func (s *Server) handleMigrationProbe(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Security string `json:"security"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if req.Host == "" || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "host, username and password are required")
		return
	}
	if req.Security == "" {
		req.Security = "starttls"
	}
	if req.Port == 0 {
		req.Port = 993
	}
	folders, err := migrator.ProbeSource(r.Context(), &migrator.SourceConfig{
		Host: req.Host, Port: req.Port, Security: req.Security,
		Username: req.Username, Password: req.Password,
	})
	if err != nil {
		writeErrorHint(w, http.StatusBadGateway, "source_unreachable", "Could not reach the source mailbox: "+err.Error(),
			"check the host, port, security mode and credentials")
		return
	}
	_ = ac
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

// handleCreateMigration: create the job. The source password is sealed at
// rest with the platform KMS key (same cipher as DKIM keys, Section 7.6).
func (s *Server) handleCreateMigration(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	var req struct {
		MailboxID string            `json:"mailbox_id"`
		Source    json.RawMessage   `json:"source"`
		FolderMap map[string]string `json:"folder_map"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	mbID, ok := parseIDParam(req.MailboxID, ids.Mailbox)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad mailbox id")
		return
	}
	var src struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(req.Source, &src); err != nil || src.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "source.password required")
		return
	}
	sealed, err := krypto.SealedString(s.Cfg.KMSKeyBytes(), src.Password)
	if err != nil {
		writeErrorHint(w, http.StatusInternalServerError, "internal", "could not seal source credentials", err.Error())
		return
	}
	var srcMap map[string]any
	_ = json.Unmarshal(req.Source, &srcMap)
	srcMap["password_sealed"] = sealed
	delete(srcMap, "password")
	srcJSON, _ := json.Marshal(srcMap)

	job := &store.MigrationJob{
		ID:        uuid.New(),
		OrgID:     ac.User.OrgID,
		MailboxID: mbID,
		CreatedBy: ac.User.ID,
		Source:    srcJSON,
		State:     "queued",
		FolderMap: req.FolderMap,
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.CreateMigrationJob(r.Context(), tx, job); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false, "migration.created", "migration_job", job.ID.String(), req.MailboxID, jsonBytes(map[string]any{"host": srcMap["host"]}))
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": migrationJobView(job)})
}

func (s *Server) handleListMigrations(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	jobs, err := store.ListMigrationJobs(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, migrationJobView(j))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleGetMigration(w http.ResponseWriter, r *http.Request) {
	job, ok := s.migrationAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	state := r.URL.Query().Get("state")
	limit := atoiOr(r.URL.Query().Get("limit"), 50)
	offset := atoiOr(r.URL.Query().Get("offset"), 0)
	items, err := store.ListMigrationItems(r.Context(), s.Store.Pool, job.ID, state, limit, offset)
	if err != nil {
		mapError(w, err)
		return
	}
	itemsOut := make([]map[string]any, 0, len(items))
	for _, it := range items {
		itemsOut = append(itemsOut, map[string]any{
			"id": ids.Format(ids.MigrationItem, it.ID), "folder": it.Folder, "uid": it.UID,
			"subject": it.Subject, "from": it.FromAddr, "size_bytes": it.Size,
			"state": it.State, "attempts": it.Attempts, "error": it.Error,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": migrationJobView(job), "items": itemsOut})
}

func migrationTransition(from []string, to string, audit string) func(*Server, http.ResponseWriter, *http.Request) {
	return func(s *Server, w http.ResponseWriter, r *http.Request) {
		job, ok := s.migrationAccess(r, chi.URLParam(r, "id"))
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "job not found")
			return
		}
		ac := authFrom(r.Context())
		if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			if err := store.TransitionMigrationJob(r.Context(), tx, job.ID, from, to); err != nil {
				return err
			}
			return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false, audit, "migration_job", job.ID.String(), "", nil)
		}); err != nil {
			mapError(w, err)
			return
		}
		fresh, err := store.GetMigrationJob(r.Context(), s.Store.Pool, job.ID)
		if err != nil {
			mapError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": migrationJobView(fresh)})
	}
}

func (s *Server) handleMigrationPause(w http.ResponseWriter, r *http.Request) {
	migrationTransition([]string{"running", "queued"}, "paused", "migration.paused")(s, w, r)
}

func (s *Server) handleMigrationResume(w http.ResponseWriter, r *http.Request) {
	migrationTransition([]string{"paused"}, "running", "migration.resumed")(s, w, r)
}

func (s *Server) handleMigrationCancel(w http.ResponseWriter, r *http.Request) {
	migrationTransition([]string{"running", "paused", "queued"}, "cancelled", "migration.cancelled")(s, w, r)
}

func (s *Server) handleDeleteMigration(w http.ResponseWriter, r *http.Request) {
	job, ok := s.migrationAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	switch job.State {
	case "running", "queued":
		writeError(w, http.StatusConflict, "conflict", "pause or cancel the job before deleting it")
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		_, e := tx.Exec(r.Context(), `delete from mail.migration_job where id=$1`, job.ID)
		return e
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func migrationJobView(j *store.MigrationJob) map[string]any {
	return map[string]any{
		"id": ids.Format(ids.Migration, j.ID), "mailbox_id": ids.Format(ids.Mailbox, j.MailboxID),
		"state": j.State, "total_messages": j.Total, "migrated": j.Migrated, "failed": j.Failed,
		"folder_map": j.FolderMap, "last_error": j.LastError, "heartbeat_at": j.HeartbeatAt,
		"started_at": j.StartedAt, "completed_at": j.CompletedAt, "created_at": j.CreatedAt,
	}
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}
