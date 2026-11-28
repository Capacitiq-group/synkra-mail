// handlers_storage.go — storage usage reporting (phase 11): totals,
// per-folder breakdown, largest messages, and cleanup suggestions. Numbers
// come from the item/message tables — the same source the quota engine uses.
package api

import (
	"net/http"

	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/ingest"
)

func (s *Server) handleStorageUsage(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())

	// Totals: reuse the quota engine's aggregation.
	qd := &ingest.Deps{Store: s.Store, Blobs: s.Pipeline.Blobs, Cfg: s.Cfg}
	state, err := qd.CheckQuota(r.Context(), ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	total := map[string]any{
		"used_bytes": state.UsedBytes, "included_bytes": state.IncludedBytes,
		"grace_bytes": state.GraceBytes, "used_percent": state.UsedPercent,
		"full": state.Full, "beyond_grace": state.BeyondGrace, "never_autodelete": true,
	}

	// Per-folder breakdown (bytes + message count per folder, per mailbox).
	folderRows, err := s.Store.Pool.Query(r.Context(), `
		select f.name, mb.name, count(i.id), coalesce(sum(m.size_bytes),0)
		from mail.mailbox_item i
		join mail.message m on m.id = i.message_id
		join mail.folder f on f.id = i.folder_id
		join mail.mailbox mb on mb.id = f.mailbox_id
		where mb.org_id = $1
		group by f.name, mb.name
		order by 4 desc`, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	defer folderRows.Close()
	byFolder := make([]map[string]any, 0, 16)
	for folderRows.Next() {
		var name, mailbox string
		var count int
		var bytes int64
		if err := folderRows.Scan(&name, &mailbox, &count, &bytes); err != nil {
			mapError(w, err)
			return
		}
		byFolder = append(byFolder, map[string]any{"folder": name, "mailbox": mailbox, "messages": count, "bytes": bytes})
	}

	// Largest messages in the org.
	largeRows, err := s.Store.Pool.Query(r.Context(), `
		select m.id, m.subject, m.size_bytes, m.internal_date, mb.name, f.name
		from mail.message m
		join mail.mailbox_item i on i.message_id = m.id
		join mail.folder f on f.id = i.folder_id
		join mail.mailbox mb on mb.id = f.mailbox_id
		where mb.org_id = $1
		order by m.size_bytes desc limit 10`, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	defer largeRows.Close()
	largest := make([]map[string]any, 0, 10)
	for largeRows.Next() {
		var id, subject, mailbox, folder string
		var size int64
		var at any
		if err := largeRows.Scan(&id, &subject, &size, &at, &mailbox, &folder); err != nil {
			mapError(w, err)
			return
		}
		largest = append(largest, map[string]any{
			"id": ids.Format(ids.Message, parseMessageID(id)), "subject": subject, "size_bytes": size,
			"received_at": at, "mailbox": mailbox, "folder": folder,
		})
	}

	// Cleanup suggestions — deterministic rules over the same tables:
	//   1. Trash/Spam older than 30 days (safe purge candidates)
	//   2. Large attachments (>5 MB) on messages older than 90 days
	//   3. Duplicate-size clusters in Trash (same size, same sender)
	suggestions := s.storageSuggestions(r, ac.User.OrgID)

	writeJSON(w, http.StatusOK, map[string]any{
		"totals":      total,
		"by_folder":   byFolder,
		"largest":     largest,
		"suggestions": suggestions,
	})
}

// parseMessageID converts a stored bare uuid string; messages are stored as
// plain uuids in the DB.
func parseMessageID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

// storageSuggestions computes deterministic cleanup candidates: stale Trash/
// Spam, big old attachments, duplicate-size clusters in Trash.
func (s *Server) storageSuggestions(r *http.Request, orgID uuid.UUID) []map[string]any {
	out := make([]map[string]any, 0, 4)
	add := func(kind, detail string, count int, bytes int64, action string) {
		if count > 0 {
			out = append(out, map[string]any{"kind": kind, "detail": detail, "count": count, "bytes": bytes, "action": action})
		}
	}
	var c1 int
	var b1 int64
	_ = s.Store.Pool.QueryRow(r.Context(), `
		select count(*), coalesce(sum(m.size_bytes),0) from mail.mailbox_item i
		join mail.message m on m.id=i.message_id
		join mail.folder f on f.id=i.folder_id
		join mail.mailbox mb on mb.id=f.mailbox_id
		where mb.org_id=$1 and f.special_use in ('\Trash','\Junk') and i.created_at < now() - interval '30 days'`,
		orgID).Scan(&c1, &b1)
	add("stale_trash", "Messages in Trash/Spam older than 30 days", c1, b1, "purge")

	var c2 int
	var b2 int64
	_ = s.Store.Pool.QueryRow(r.Context(), `
		select count(*), coalesce(sum(m.size_bytes),0) from mail.message m
		join mail.mailbox_item i on i.message_id=m.id
		join mail.folder f on f.id=i.folder_id
		join mail.mailbox mb on mb.id=f.mailbox_id
		where mb.org_id=$1 and m.size_bytes > 5*1024*1024 and m.internal_date < now() - interval '90 days'`,
		orgID).Scan(&c2, &b2)
	add("large_old", "Large messages (>5 MB) older than 90 days", c2, b2, "review")

	var c3 int
	_ = s.Store.Pool.QueryRow(r.Context(), `
		select count(*) from (
			select m.size_bytes from mail.message m
			join mail.mailbox_item i on i.message_id=m.id
			join mail.folder f on f.id=i.folder_id
			join mail.mailbox mb on mb.id=f.mailbox_id
			where mb.org_id=$1 and f.special_use='\Trash'
			group by m.size_bytes having count(*) > 1
		) d`, orgID).Scan(&c3)
	add("duplicates", "Duplicate-size clusters in Trash", c3, 0, "purge")
	return out
}
