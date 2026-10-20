package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- tracking (Section 9) ----

func CreateTrackingPixel(ctx context.Context, tx pgx.Tx, p *model.TrackingPixel) error {
	_, err := tx.Exec(ctx, `insert into mail.tracking_pixel (id, message_id, recipient) values ($1,$2,$3)`,
		p.ID, p.MessageID, p.Recipient)
	return err
}

func CreateTrackedLink(ctx context.Context, tx pgx.Tx, l *model.TrackedLink) error {
	_, err := tx.Exec(ctx, `insert into mail.tracked_link (id, message_id, url) values ($1,$2,$3)`,
		l.ID, l.MessageID, l.URL)
	return err
}

// GetPixelForEvent resolves a pixel for the tracker's event ingest (token→message+recipient).
func GetPixelForEvent(ctx context.Context, q Querier, pixelID uuid.UUID) (*model.TrackingPixel, error) {
	row := q.QueryRow(ctx, `select id, message_id, recipient, created_at from mail.tracking_pixel where id=$1`, pixelID)
	var p model.TrackingPixel
	err := row.Scan(&p.ID, &p.MessageID, &p.Recipient, &p.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

func GetLinkForEvent(ctx context.Context, q Querier, linkID uuid.UUID) (*model.TrackedLink, error) {
	row := q.QueryRow(ctx, `select id, message_id, url, created_at from mail.tracked_link where id=$1`, linkID)
	var l model.TrackedLink
	err := row.Scan(&l.ID, &l.MessageID, &l.URL, &l.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &l, nil
}

// InsertOpenEvent is called on the core's internal ingest endpoint (the tracker has
// no database credentials — Section 9.1 isolation rule).
func InsertOpenEvent(ctx context.Context, tx pgx.Tx, e *model.OpenEvent) error {
	_, err := tx.Exec(ctx, `insert into mail.open_event (id, pixel_id, occurred_at, confidence, client_family, ip_hash)
                values ($1,$2,$3,$4,$5,$6)`, e.ID, e.PixelID, e.OccurredAt, e.Confidence, optText(e.ClientFamily), optText(e.IPHash))
	return err
}

func InsertClickEvent(ctx context.Context, tx pgx.Tx, e *model.ClickEvent) error {
	_, err := tx.Exec(ctx, `insert into mail.click_event (id, link_id, occurred_at, client_family, ip_hash)
                values ($1,$2,$3,$4,$5)`, e.ID, e.LinkID, e.OccurredAt, optText(e.ClientFamily), optText(e.IPHash))
	return err
}

// MessageTrackingView: per-message analytics (who opened, when, which links — Section 9.3).
type LinkClick struct {
	Link model.TrackedLink
	Evt  model.ClickEvent
}

type MessageTrackingView struct {
	Pixels     []model.TrackingPixel
	OpenEvents []model.OpenEvent
	Links      []model.TrackedLink
	Clicks     []LinkClick
}

func TrackingEventsForMessage(ctx context.Context, q Querier, messageID uuid.UUID) (*MessageTrackingView, error) {
	v := &MessageTrackingView{}
	rows, err := q.Query(ctx, `select id, message_id, recipient, created_at from mail.tracking_pixel where message_id=$1`, messageID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p model.TrackingPixel
		if err := rows.Scan(&p.ID, &p.MessageID, &p.Recipient, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		v.Pixels = append(v.Pixels, p)
	}
	rows.Close()

	rows, err = q.Query(ctx, `select e.id, e.pixel_id, e.occurred_at, e.confidence, coalesce(e.client_family,''), coalesce(e.ip_hash,'')
                from mail.open_event e join mail.tracking_pixel p on p.id = e.pixel_id
                where p.message_id=$1 order by e.occurred_at`, messageID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e model.OpenEvent
		if err := rows.Scan(&e.ID, &e.PixelID, &e.OccurredAt, &e.Confidence, &e.ClientFamily, &e.IPHash); err != nil {
			rows.Close()
			return nil, err
		}
		v.OpenEvents = append(v.OpenEvents, e)
	}
	rows.Close()

	rows, err = q.Query(ctx, `select id, message_id, url, created_at from mail.tracked_link where message_id=$1`, messageID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l model.TrackedLink
		if err := rows.Scan(&l.ID, &l.MessageID, &l.URL, &l.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		v.Links = append(v.Links, l)
	}
	rows.Close()

	rows, err = q.Query(ctx, `select l.id, l.message_id, l.url, l.created_at, e.id, e.link_id, e.occurred_at,
                coalesce(e.client_family,''), coalesce(e.ip_hash,'')
                from mail.click_event e join mail.tracked_link l on l.id = e.link_id
                where l.message_id=$1 order by e.occurred_at`, messageID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l model.TrackedLink
		var e model.ClickEvent
		if err := rows.Scan(&l.ID, &l.MessageID, &l.URL, &l.CreatedAt, &e.ID, &e.LinkID, &e.OccurredAt, &e.ClientFamily, &e.IPHash); err != nil {
			rows.Close()
			return nil, err
		}
		v.Clicks = append(v.Clicks, LinkClick{Link: l, Evt: e})
	}
	rows.Close()
	return v, rows.Err()
}

// ---- audit (Sections 7.1, 11.1, 11.2: everything audited) ----

func RecordAudit(ctx context.Context, tx pgx.Tx, actorUserID, adminUserID *uuid.UUID, impersonated bool,
	action, targetType, targetID, reason string, metaJSON []byte) error {
	var meta any
	if metaJSON != nil {
		meta = metaJSON
	}
	_, err := tx.Exec(ctx, `insert into mail.audit_event (id, actor_user_id, admin_user_id, impersonated, action, target_type, target_id, reason, meta)
                values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.New(), optUUID(actorUserID), optUUID(adminUserID), impersonated,
		action, optText(targetType), optText(targetID), optText(reason), meta)
	return err
}

func ListAudit(ctx context.Context, q Querier, limit int) ([]AuditRow, error) {
	rows, err := q.Query(ctx, `select id, actor_user_id, admin_user_id, impersonated, action, coalesce(target_type,''),
                coalesce(target_id,''), coalesce(reason,''), meta, created_at from mail.audit_event order by created_at desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.ActorUserID, &r.AdminUserID, &r.Impersonated, &r.Action,
			&r.TargetType, &r.TargetID, &r.Reason, &r.Meta, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type AuditRow struct {
	ID           uuid.UUID
	ActorUserID  *uuid.UUID
	AdminUserID  *uuid.UUID
	Impersonated bool
	Action       string
	TargetType   string
	TargetID     string
	Reason       string
	Meta         []byte
	CreatedAt    time.Time
}

// quarantine ----

func QuarantineMessage(ctx context.Context, tx pgx.Tx, qrt *model.Quarantine) error {
	_, err := tx.Exec(ctx, `insert into mail.quarantine (id, org_id, mailbox_id, message_id, reason, score, expires_at)
                values ($1,$2,$3,$4,$5,$6,$7)`,
		qrt.ID, qrt.OrgID, optUUID(qrt.MailboxID), optUUID(qrt.MessageID), qrt.Reason, qrt.Score, qrt.ExpiresAt)
	return err
}
