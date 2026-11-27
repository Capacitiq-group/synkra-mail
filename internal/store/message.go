package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- blobs (content-addressed, refcounted — Section 4.4) ----

func PutBlob(ctx context.Context, tx pgx.Tx, sha string, size int64) error {
	_, err := tx.Exec(ctx, `
                insert into mail.blob (sha256, size_bytes, refcount) values ($1,$2,0)
                on conflict (sha256) do nothing`, sha, size)
	return err
}

func AddBlobRef(ctx context.Context, tx pgx.Tx, sha string) error {
	_, err := tx.Exec(ctx, `update mail.blob set refcount = refcount + 1, purge_after = null where sha256=$1`, sha)
	return err
}

func ReleaseBlobRef(ctx context.Context, tx pgx.Tx, sha string) error {
	_, err := tx.Exec(ctx, `
                update mail.blob set refcount = refcount - 1,
                  purge_after = case when refcount - 1 <= 0 then now() + interval '7 days' else null end
                where sha256=$1`, sha)
	return err
}

// ListPurgeableBlobs returns zero-ref blobs whose 7-day grace has elapsed (GC, Section 4.4).
func ListPurgeableBlobs(ctx context.Context, q Querier, limit int) ([]string, error) {
	rows, err := q.Query(ctx, `select sha256 from mail.blob where refcount <= 0 and purge_after is not null and purge_after < now() limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}

// ---- threads ----

func FindOrCreateThread(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, refs []string, inReplyTo, subjectNorm string) (uuid.UUID, error) {
	// Threading v1 (Section 4.2): References/In-Reply-To chain first.
	var threadID uuid.UUID
	if inReplyTo != "" {
		err := tx.QueryRow(ctx, `select t.id from mail.thread t
                        join mail.message m on m.thread_id = t.id and m.org_id = $1
                        where m.message_id = $2 limit 1`, orgID, inReplyTo).Scan(&threadID)
		if err == nil {
			return threadID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
		if len(refs) > 0 {
			err = tx.QueryRow(ctx, `select t.id from mail.thread t
                                join mail.message m on m.thread_id = t.id and m.org_id = $1
                                where m.message_id = any($2) limit 1`, orgID, refs).Scan(&threadID)
			if err == nil {
				return threadID, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return uuid.Nil, err
			}
		}
	}
	if subjectNorm != "" {
		// Normalized-subject fallback within a 14-day window.
		err := tx.QueryRow(ctx, `select id from mail.thread
                        where org_id=$1 and subject_norm=$2 and updated_at > now() - interval '14 days'
                        order by updated_at desc limit 1`, orgID, subjectNorm).Scan(&threadID)
		if err == nil {
			_, err = tx.Exec(ctx, `update mail.thread set updated_at=now() where id=$1`, threadID)
			return threadID, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
	}
	threadID = uuid.New()
	_, err := tx.Exec(ctx, `insert into mail.thread (id, org_id, subject_norm) values ($1,$2,$3)`,
		threadID, orgID, optText(subjectNorm))
	return threadID, err
}

// ---- message + mailbox_item ----

// InsertMessage stores the parsed message in one transaction: blob ref, message row,
// mailbox_item with a UID allocated under the folder lock. Counters are trigger-maintained.
func InsertMessage(ctx context.Context, tx pgx.Tx, msg *model.Message, folderID uuid.UUID, flags []string) (itemID uuid.UUID, uid uint32, err error) {
	if err = PutBlob(ctx, tx, msg.BlobSHA256, msg.SizeBytes); err != nil {
		return uuid.Nil, 0, err
	}
	if err = AddBlobRef(ctx, tx, msg.BlobSHA256); err != nil {
		return uuid.Nil, 0, err
	}

	toJSON := mustJSON(msg.ToAddrs)
	ccJSON := mustJSON(msg.CcAddrs)
	var arJSON any
	if msg.AuthResults != nil {
		arJSON = mustJSON(msg.AuthResults)
	}

	_, err = tx.Exec(ctx, `
                insert into mail.message (id, org_id, mailbox_id, blob_sha256, direction, message_id, in_reply_to, refs,
                        thread_id, from_email, from_name, to_addrs, cc_addrs, subject, snippet, body_text, has_attachments,
                        size_bytes, internal_date, auth_results, spam_score, tracking_enabled)
                values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		msg.ID, msg.OrgID, msg.MailboxID, msg.BlobSHA256, msg.Direction,
		optText(msg.MessageID), optText(msg.InReplyTo), msg.Refs,
		optUUID(msg.ThreadID), optText(msg.FromEmail), optText(msg.FromName),
		toJSON, ccJSON, optText(msg.Subject), msg.Snippet, optText(msg.BodyText),
		msg.HasAttachments, msg.SizeBytes, msg.InternalDate, arJSON, msg.SpamScore, msg.TrackingEnabled)
	if err != nil {
		return uuid.Nil, 0, err
	}

	// UID allocation: serialized on the folder row.
	err = tx.QueryRow(ctx, `
                update mail.folder set uid_next = uid_next + 1 where id=$1 returning uid_next - 1`, folderID).Scan(&uid)
	if err != nil {
		return uuid.Nil, 0, err
	}
	itemID = uuid.New()
	if len(flags) == 0 {
		flags = []string{}
	}
	_, err = tx.Exec(ctx, `
                insert into mail.mailbox_item (id, folder_id, message_id, uid, flags) values ($1,$2,$3,$4,$5)`,
		itemID, folderID, msg.ID, uid, flags)
	if err != nil {
		return uuid.Nil, 0, err
	}
	return itemID, uid, nil
}

func InsertAttachment(ctx context.Context, tx pgx.Tx, att *model.Attachment) error {
	if err := PutBlob(ctx, tx, att.BlobSHA256, att.SizeBytes); err != nil {
		return err
	}
	if err := AddBlobRef(ctx, tx, att.BlobSHA256); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into mail.attachment (id, message_id, blob_sha256, filename, content_type, size_bytes, is_inline, content_id)
                values ($1,$2,$3,$4,$5,$6,$7,$8)`,
		att.ID, att.MessageID, att.BlobSHA256, optText(att.Filename), att.ContentType, att.SizeBytes, att.IsInline, optText(att.ContentID))
	return err
}

type MessageFull struct {
	Message model.Message
	Item    model.MailboxItem
	Folder  model.Folder
}

func GetMessage(ctx context.Context, q Querier, messageID uuid.UUID) (*MessageFull, error) {
	msg, err := scanMessage(q.QueryRow(ctx, `select
                id, org_id, mailbox_id, blob_sha256, direction, coalesce(message_id::text,''), coalesce(in_reply_to::text,''),
                refs, thread_id, coalesce(from_email::text,''), coalesce(from_name,''), to_addrs, cc_addrs,
                coalesce(subject,''), snippet, coalesce(body_text,''), has_attachments, size_bytes, internal_date,
                auth_results, coalesce(spam_score,0), tracking_enabled, created_at
                from mail.message where id=$1`, messageID))
	if err != nil {
		return nil, err
	}
	full := &MessageFull{Message: *msg}
	var item model.MailboxItem
	err = q.QueryRow(ctx, `select id, folder_id, message_id, uid, flags, deleted_at, created_at
                from mail.mailbox_item where message_id=$1 order by created_at limit 1`, messageID).
		Scan(&item.ID, &item.FolderID, &item.MessageID, &item.UID, &item.Flags, &item.DeletedAt, &item.CreatedAt)
	if err == nil {
		full.Item = item
		f, ferr := GetFolder(ctx, q, item.FolderID)
		if ferr != nil {
			return nil, ferr
		}
		full.Folder = *f
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return full, nil
}

func scanMessage(row pgx.Row) (*model.Message, error) {
	var m model.Message
	var authResults []byte
	var threadID *uuid.UUID
	err := row.Scan(&m.ID, &m.OrgID, &m.MailboxID, &m.BlobSHA256, &m.Direction, &m.MessageID, &m.InReplyTo,
		&m.Refs, &threadID, &m.FromEmail, &m.FromName, &m.ToAddrs, &m.CcAddrs,
		&m.Subject, &m.Snippet, &m.BodyText, &m.HasAttachments, &m.SizeBytes, &m.InternalDate,
		&authResults, &m.SpamScore, &m.TrackingEnabled, &m.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	m.ThreadID = threadID
	if len(authResults) > 0 {
		m.AuthResults = map[string]any{}
		if err := mustUnmarshalJSON(authResults, &m.AuthResults); err != nil {
			m.AuthResults = nil
		}
	}
	return &m, nil
}

// ListItems lists live (non-deleted) items in a folder, newest UID last. Range is an
// IMAP sequence set expressed as uid ranges (1..uidNext).
func ListItems(ctx context.Context, q Querier, folderID uuid.UUID, sinceUID, untilUID uint32) ([]ItemWithMessage, error) {
	rows, err := q.Query(ctx, `select i.id, i.folder_id, i.message_id, i.uid, i.flags, i.deleted_at, i.created_at,
                m.id, m.org_id, m.mailbox_id, m.blob_sha256, m.direction, coalesce(m.message_id::text,''), coalesce(m.in_reply_to::text,''),
                m.refs, m.thread_id, coalesce(m.from_email::text,''), coalesce(m.from_name,''), m.to_addrs, m.cc_addrs,
                coalesce(m.subject,''), m.snippet, coalesce(m.body_text,''), m.has_attachments, m.size_bytes, m.internal_date,
                coalesce(m.spam_score,0), m.tracking_enabled
                from mail.mailbox_item i join mail.message m on m.id = i.message_id
                where i.folder_id=$1 and i.deleted_at is null and i.uid >= $2 and i.uid <= $3
                order by i.uid`, folderID, sinceUID, untilUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemWithMessage
	for rows.Next() {
		var iwm ItemWithMessage
		if err := scanItemWithMessage(rows, &iwm); err != nil {
			return nil, err
		}
		out = append(out, iwm)
	}
	return out, rows.Err()
}

type ItemWithMessage struct {
	Item    model.MailboxItem
	Message model.Message
}

// ListItemsPage returns the newest `limit` live items in a folder, older pages
// fetched by passing the smallest uid of the previous page as beforeUID (0 =
// from the top). Returned in ascending uid order, like ListItems (register §1.8:
// the API must not load the whole folder to keep the last 50).
func ListItemsPage(ctx context.Context, q Querier, folderID uuid.UUID, beforeUID uint32, limit int) ([]ItemWithMessage, error) {
	rows, err := q.Query(ctx, `select i.id, i.folder_id, i.message_id, i.uid, i.flags, i.deleted_at, i.created_at,
                m.id, m.org_id, m.mailbox_id, m.blob_sha256, m.direction, coalesce(m.message_id::text,''), coalesce(m.in_reply_to::text,''),
                m.refs, m.thread_id, coalesce(m.from_email::text,''), coalesce(m.from_name,''), m.to_addrs, m.cc_addrs,
                coalesce(m.subject,''), m.snippet, coalesce(m.body_text,''), m.has_attachments, m.size_bytes, m.internal_date,
                coalesce(m.spam_score,0), m.tracking_enabled
                from mail.mailbox_item i join mail.message m on m.id = i.message_id
                where i.folder_id=$1 and i.deleted_at is null and ($2::bigint = 0 or i.uid < $2)
                order by i.uid desc limit $3`, folderID, beforeUID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemWithMessage
	for rows.Next() {
		var iwm ItemWithMessage
		if err := scanItemWithMessage(rows, &iwm); err != nil {
			return nil, err
		}
		out = append(out, iwm)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Newest-first from SQL; the API surface is ascending-uid (oldest of the
	// page first), matching the previous whole-folder behaviour.
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out, nil
}

// ThreadMailboxIDs lists the mailboxes that hold messages of a thread. Thread
// notes and assignment must be guarded by membership of those mailboxes, not
// just org membership (register §1.4).
func ThreadMailboxIDs(ctx context.Context, q Querier, threadID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `select distinct mailbox_id from mail.message where thread_id=$1`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type itemScanner interface{ Scan(dest ...any) error }

func scanItemWithMessage(row itemScanner, out *ItemWithMessage) error {
	var threadID *uuid.UUID
	var authResults []byte
	err := row.Scan(&out.Item.ID, &out.Item.FolderID, &out.Item.MessageID, &out.Item.UID, &out.Item.Flags, &out.Item.DeletedAt, &out.Item.CreatedAt,
		&out.Message.ID, &out.Message.OrgID, &out.Message.MailboxID, &out.Message.BlobSHA256, &out.Message.Direction,
		&out.Message.MessageID, &out.Message.InReplyTo, &out.Message.Refs, &threadID,
		&out.Message.FromEmail, &out.Message.FromName, &out.Message.ToAddrs, &out.Message.CcAddrs,
		&out.Message.Subject, &out.Message.Snippet, &out.Message.BodyText, &out.Message.HasAttachments,
		&out.Message.SizeBytes, &out.Message.InternalDate, &out.Message.SpamScore, &out.Message.TrackingEnabled)
	if err != nil {
		return err
	}
	out.Message.ThreadID = threadID
	if len(authResults) > 0 {
		out.Message.AuthResults = map[string]any{}
		_ = mustUnmarshalJSON(authResults, &out.Message.AuthResults)
	}
	return nil
}

func GetItemByUID(ctx context.Context, q Querier, folderID uuid.UUID, uid uint32) (*ItemWithMessage, error) {
	row := q.QueryRow(ctx, `select i.id, i.folder_id, i.message_id, i.uid, i.flags, i.deleted_at, i.created_at,
                m.id, m.org_id, m.mailbox_id, m.blob_sha256, m.direction, coalesce(m.message_id::text,''), coalesce(m.in_reply_to::text,''),
                m.refs, m.thread_id, coalesce(m.from_email::text,''), coalesce(m.from_name,''), m.to_addrs, m.cc_addrs,
                coalesce(m.subject,''), m.snippet, coalesce(m.body_text,''), m.has_attachments, m.size_bytes, m.internal_date,
                coalesce(m.spam_score,0), m.tracking_enabled
                from mail.mailbox_item i join mail.message m on m.id = i.message_id
                where i.folder_id=$1 and i.uid=$2 and i.deleted_at is null`, folderID, uid)
	var iwm ItemWithMessage
	if err := scanItemWithMessage(row, &iwm); err != nil {
		return nil, notFound(err)
	}
	return &iwm, nil
}

func UpdateItemFlags(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, uid uint32, flags []string) error {
	_, err := tx.Exec(ctx, `update mail.mailbox_item set flags=$3 where folder_id=$1 and uid=$2 and deleted_at is null`, folderID, uid, flags)
	return err
}

// MoveItem repoints an item to another folder with a fresh UID (COPY semantics for MOVE).
func MoveItem(ctx context.Context, tx pgx.Tx, folderID, destFolderID uuid.UUID, uid uint32) (newUID uint32, err error) {
	var msgID uuid.UUID
	var flags []string
	err = tx.QueryRow(ctx, `select message_id, flags from mail.mailbox_item
                where folder_id=$1 and uid=$2 and deleted_at is null for update`, folderID, uid).Scan(&msgID, &flags)
	if err != nil {
		return 0, notFound(err)
	}
	err = tx.QueryRow(ctx, `update mail.folder set uid_next = uid_next + 1 where id=$1 returning uid_next - 1`, destFolderID).Scan(&newUID)
	if err != nil {
		return 0, err
	}
	// Move = delete old row (triggers fix counters + notify) then insert new one.
	if _, err = tx.Exec(ctx, `delete from mail.mailbox_item where folder_id=$1 and uid=$2`, folderID, uid); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `insert into mail.mailbox_item (id, folder_id, message_id, uid, flags) values ($1,$2,$3,$4,$5)`,
		uuid.New(), destFolderID, msgID, newUID, flags)
	return newUID, err
}

// CopyItem duplicates the item into another folder (message row is shared).
func CopyItem(ctx context.Context, tx pgx.Tx, folderID, destFolderID uuid.UUID, uid uint32) (newUID uint32, err error) {
	var msgID uuid.UUID
	var flags []string
	err = tx.QueryRow(ctx, `select message_id, flags from mail.mailbox_item
                where folder_id=$1 and uid=$2 and deleted_at is null`, folderID, uid).Scan(&msgID, &flags)
	if err != nil {
		return 0, notFound(err)
	}
	err = tx.QueryRow(ctx, `update mail.folder set uid_next = uid_next + 1 where id=$1 returning uid_next - 1`, destFolderID).Scan(&newUID)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `insert into mail.mailbox_item (id, folder_id, message_id, uid, flags) values ($1,$2,$3,$4,$5)`,
		uuid.New(), destFolderID, msgID, newUID, flags)
	return newUID, err
}

// ExpungeItem hard-deletes an item flagged \Deleted. Message rows are purged when
// no items reference them; blob refcounts decrement accordingly.
func ExpungeItem(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, uid uint32) (messageID uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `delete from mail.mailbox_item
                where folder_id=$1 and uid=$2 and '\Deleted' = any(flags) returning message_id`, folderID, uid).Scan(&messageID)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	var cnt int
	err = tx.QueryRow(ctx, `select count(*) from mail.mailbox_item where message_id=$1`, messageID).Scan(&cnt)
	if err != nil {
		return uuid.Nil, err
	}
	if cnt == 0 {
		if err := purgeMessageRow(ctx, tx, messageID); err != nil {
			return uuid.Nil, err
		}
	}
	return messageID, nil
}

// ExpungeItemNow hard-deletes an item without requiring the \Deleted flag:
// the API delete-from-Trash path never had a way to set it, so the expunge
// always returned "not found" (audit item 25). The IMAP path keeps the strict
// flag check above.
func ExpungeItemNow(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, uid uint32) (messageID uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `delete from mail.mailbox_item
                where folder_id=$1 and uid=$2 returning message_id`, folderID, uid).Scan(&messageID)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	var cnt int
	err = tx.QueryRow(ctx, `select count(*) from mail.mailbox_item where message_id=$1`, messageID).Scan(&cnt)
	if err != nil {
		return uuid.Nil, err
	}
	if cnt == 0 {
		if err := purgeMessageRow(ctx, tx, messageID); err != nil {
			return uuid.Nil, err
		}
	}
	return messageID, nil
}

// purgeMessageRow removes a message row and every child that references it,
// releasing each blob reference on the way out (audit item 25: deleting the
// message row alone hit foreign-key violations from attachments and delivery
// jobs, and attachment blobs were never released).
func purgeMessageRow(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) error {
	// Tracking leaves (reference the message directly).
	if _, err := tx.Exec(ctx, `delete from mail.click_event ce using mail.tracked_link tl
		where ce.link_id = tl.id and tl.message_id=$1`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from mail.tracked_link where message_id=$1`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from mail.open_event oe using mail.tracking_pixel tp
		where oe.pixel_id = tp.id and tp.message_id=$1`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from mail.tracking_pixel where message_id=$1`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from mail.delivery_event where message_id=$1`, messageID); err != nil {
		return err
	}
	// Delivery jobs + their DSNs (jobs reference the message row).
	if _, err := tx.Exec(ctx, `delete from mail.dsn d using mail.delivery_job j
		where d.delivery_job_id = j.id and j.message_id=$1`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from mail.delivery_job where message_id=$1`, messageID); err != nil {
		return err
	}
	// Attachments: release every blob ref, then drop the rows.
	attRows, err := tx.Query(ctx, `delete from mail.attachment where message_id=$1 returning blob_sha256`, messageID)
	if err != nil {
		return err
	}
	var attSHAs []string
	for attRows.Next() {
		var sha string
		if err := attRows.Scan(&sha); err != nil {
			attRows.Close()
			return err
		}
		attSHAs = append(attSHAs, sha)
	}
	attRows.Close()
	if err := attRows.Err(); err != nil {
		return err
	}
	for _, sha := range attSHAs {
		if err := ReleaseBlobRef(ctx, tx, sha); err != nil {
			return err
		}
	}
	// The message row itself.
	var sha string
	if err := tx.QueryRow(ctx, `delete from mail.message where id=$1 returning blob_sha256`, messageID).Scan(&sha); err != nil {
		return err
	}
	return ReleaseBlobRef(ctx, tx, sha)
}

// SearchMessages: metadata + FTS (Section 4.4 hot indexes; target p95 < 1 s).
// The ILIKE pattern escapes LIKE metacharacters (audit item 27: a query of
// "100%" previously matched everything and could never match a literal "%").
func SearchMessages(ctx context.Context, q Querier, mailboxID uuid.UUID, query string, limit int) ([]ItemWithMessage, error) {
	if query == "" || mailboxID == uuid.Nil {
		return nil, nil
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	pattern := "%" + esc + "%"
	rows, err := q.Query(ctx, `select i.id, i.folder_id, i.message_id, i.uid, i.flags, i.deleted_at, i.created_at,
                m.id, m.org_id, m.mailbox_id, m.blob_sha256, m.direction, coalesce(m.message_id::text,''), coalesce(m.in_reply_to::text,''),
                m.refs, m.thread_id, coalesce(m.from_email::text,''), coalesce(m.from_name,''), m.to_addrs, m.cc_addrs,
                coalesce(m.subject,''), m.snippet, coalesce(m.body_text,''), m.has_attachments, m.size_bytes, m.internal_date,
                coalesce(m.spam_score,0), m.tracking_enabled
                from mail.mailbox_item i join mail.message m on m.id = i.message_id
                where m.mailbox_id=$1 and i.deleted_at is null
                  and (m.fts @@ plainto_tsquery('simple', $2) or m.subject ilike $3 or m.from_email::text ilike $3)
                order by m.internal_date desc limit $4`, mailboxID, query, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemWithMessage
	for rows.Next() {
		var iwm ItemWithMessage
		if err := scanItemWithMessage(rows, &iwm); err != nil {
			return nil, err
		}
		out = append(out, iwm)
	}
	return out, rows.Err()
}

// ---- quota (org shared pool — Section 4.4) ----

func OrgUsageBytes(ctx context.Context, q Querier, orgID uuid.UUID) (int64, error) {
	var total int64
	err := q.QueryRow(ctx, `select coalesce(sum(size_bytes),0) from mail.message where org_id=$1`, orgID).Scan(&total)
	return total, err
}

func RecordQuotaEvent(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, level int, note string) error {
	// Unique (org_id, level): one warning per crossing, re-armable by deleting the row.
	_, err := tx.Exec(ctx, `insert into mail.quota_event (id, org_id, level, note) values ($1,$2,$3,$4)
                on conflict (org_id, level) do nothing`, uuid.New(), orgID, level, optText(note))
	return err
}

func ClearQuotaEvent(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, level int) error {
	_, err := tx.Exec(ctx, `delete from mail.quota_event where org_id=$1 and level=$2`, orgID, level)
	return err
}

func RecordStorageLedger(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, month time.Time, overageGB int, note string) error {
	_, err := tx.Exec(ctx, `insert into mail.storage_ledger (id, org_id, month, overage_gb, note) values ($1,$2,$3,$4,$5)
                on conflict (org_id, month) do update set overage_gb=excluded.overage_gb, note=excluded.note`,
		uuid.New(), orgID, month, overageGB, optText(note))
	return err
}

// ---- attachments (register §2.10: blobs are stored and referenced; these are
// the routes that serve them) ----

func ListAttachments(ctx context.Context, q Querier, messageID uuid.UUID) ([]model.Attachment, error) {
	rows, err := q.Query(ctx, `select id, message_id, blob_sha256, coalesce(filename,''), content_type, size_bytes, is_inline, coalesce(content_id,'')
		from mail.attachment where message_id=$1 order by id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Attachment
	for rows.Next() {
		var a model.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.BlobSHA256, &a.Filename, &a.ContentType, &a.SizeBytes, &a.IsInline, &a.ContentID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func GetAttachment(ctx context.Context, q Querier, attID uuid.UUID) (*model.Attachment, error) {
	row := q.QueryRow(ctx, `select id, message_id, blob_sha256, coalesce(filename,''), content_type, size_bytes, is_inline, coalesce(content_id,'')
		from mail.attachment where id=$1`, attID)
	var a model.Attachment
	err := row.Scan(&a.ID, &a.MessageID, &a.BlobSHA256, &a.Filename, &a.ContentType, &a.SizeBytes, &a.IsInline, &a.ContentID)
	if err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}
