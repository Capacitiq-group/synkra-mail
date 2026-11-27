package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- delivery queue (Section 5.3: durable truth is delivery_job; SKIP LOCKED claiming) ----

func CreateDeliveryJob(ctx context.Context, tx pgx.Tx, j *model.DeliveryJob) error {
	_, err := tx.Exec(ctx, `
		insert into mail.delivery_job (id, org_id, mailbox_id, message_id, envelope_from, envelope_to, recipient_domain, is_dsn, scheduled_send_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		j.ID, j.OrgID, j.MailboxID, j.MessageID, j.EnvelopeFrom, j.EnvelopeTo, j.RecipientDomain, j.IsDSN, j.ScheduledSendAt)
	return err
}

func GetDeliveryJob(ctx context.Context, q Querier, id uuid.UUID) (*model.DeliveryJob, error) {
	row := q.QueryRow(ctx, `select id, org_id, mailbox_id, message_id, envelope_from, envelope_to, recipient_domain,
		is_dsn, status, attempts, next_attempt_at, lease_until, coalesce(leased_by,''), coalesce(last_code,''),
		coalesce(last_msg,''), scheduled_send_at, created_at, updated_at
		from mail.delivery_job where id=$1`, id)
	return scanDeliveryJob(row)
}

func scanDeliveryJob(row pgx.Row) (*model.DeliveryJob, error) {
	var j model.DeliveryJob
	err := row.Scan(&j.ID, &j.OrgID, &j.MailboxID, &j.MessageID, &j.EnvelopeFrom, &j.EnvelopeTo, &j.RecipientDomain,
		&j.IsDSN, &j.Status, &j.Attempts, &j.NextAttemptAt, &j.LeaseUntil, &j.LeasedBy,
		&j.LastCode, &j.LastMsg, &j.ScheduledSendAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &j, nil
}

// ClaimDueJobs leases up to limit jobs atomically (FOR UPDATE SKIP LOCKED,
// Section 5.3). Jobs still in_flight whose lease expired are reclaimed: the
// leasing worker died mid-delivery, so the job is retried on this worker.
func ClaimDueJobs(ctx context.Context, q Querier, workerID string, lease time.Duration, limit int) ([]model.DeliveryJob, error) {
	rows, err := q.Query(ctx, `
		with due as (
			select id from mail.delivery_job
			where status in ('queued','deferred','in_flight')
			  and next_attempt_at <= now()
			  and (lease_until is null or lease_until < now())
			order by next_attempt_at
			limit $1
			for update skip locked
		)
		update mail.delivery_job j
		set status='in_flight', lease_until = now() + make_interval(secs => $2), leased_by=$3, updated_at=now()
		from due
		where j.id = due.id
		returning j.id, j.org_id, j.mailbox_id, j.message_id, j.envelope_from, j.envelope_to, j.recipient_domain,
			j.is_dsn, j.status, j.attempts, j.next_attempt_at, j.lease_until, coalesce(j.leased_by,''),
			coalesce(j.last_code,''), coalesce(j.last_msg,''), j.scheduled_send_at, j.created_at, j.updated_at`,
		limit, int(lease.Seconds()), workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DeliveryJob
	for rows.Next() {
		j, err := scanDeliveryJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func MarkJobDelivered(ctx context.Context, tx pgx.Tx, id uuid.UUID, code, msg string) error {
	_, err := tx.Exec(ctx, `update mail.delivery_job set status='delivered', last_code=$2, last_msg=$3,
		lease_until=null, updated_at=now() where id=$1`, id, code, msg)
	return err
}

func DeferJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, attempts int, nextAt time.Time, code, msg string) error {
	_, err := tx.Exec(ctx, `update mail.delivery_job set status='deferred', attempts=$2, next_attempt_at=$3,
		last_code=$4, last_msg=$5, lease_until=null, updated_at=now() where id=$1`, id, attempts, nextAt, code, msg)
	return err
}

func BounceJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, attempts int, code, msg string) error {
	_, err := tx.Exec(ctx, `update mail.delivery_job set status='bounced', attempts=$2, last_code=$3, last_msg=$4,
		lease_until=null, updated_at=now() where id=$1`, id, attempts, code, msg)
	return err
}

func RecordDSN(ctx context.Context, tx pgx.Tx, d *model.DSN) error {
	_, err := tx.Exec(ctx, `insert into mail.dsn (id, delivery_job_id, kind, is_hard, smtp_code, reason)
		values ($1,$2,$3,$4,$5,$6)`,
		d.ID, d.DeliveryJobID, d.Kind, d.IsHard, optText(d.SMTPCode), optText(d.Reason))
	return err
}

// ListJobsByOrg: admin queue inspection (Section 11.1).
func ListJobsByOrg(ctx context.Context, q Querier, orgID uuid.UUID, limit int) ([]model.DeliveryJob, error) {
	rows, err := q.Query(ctx, `select id, org_id, mailbox_id, message_id, envelope_from, envelope_to, recipient_domain,
		is_dsn, status, attempts, next_attempt_at, lease_until, coalesce(leased_by,''), coalesce(last_code,''),
		coalesce(last_msg,''), scheduled_send_at, created_at, updated_at
		from mail.delivery_job where org_id=$1 order by created_at desc limit $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DeliveryJob
	for rows.Next() {
		j, err := scanDeliveryJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func QueueDepth(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.delivery_job where status in ('queued','deferred','in_flight')`).Scan(&n)
	return n, err
}

// ---- suppression (Section 7.3) ----

func Suppress(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, email, reason string) error {
	_, err := tx.Exec(ctx, `insert into mail.suppression (org_id, email, reason) values ($1,$2,$3)
		on conflict (org_id, email) do nothing`, orgID, email, reason)
	return err
}

func IsSuppressed(ctx context.Context, q Querier, orgID uuid.UUID, email string) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.suppression where org_id=$1 and email=$2`, orgID, email).Scan(&n)
	return n > 0, err
}

func ListSuppression(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.Suppression, error) {
	rows, err := q.Query(ctx, `select org_id, email, reason, created_at from mail.suppression where org_id=$1 order by created_at desc`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Suppression
	for rows.Next() {
		var s model.Suppression
		if err := rows.Scan(&s.OrgID, &s.Email, &s.Reason, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- outbound accounting for limits + circuit breaker (Section 7.3) ----

// CountSentSince counts non-DSN delivery attempts since t.
func CountSentSince(ctx context.Context, q Querier, orgID uuid.UUID, mailboxID *uuid.UUID, since time.Time) (int, error) {
	var n int
	var err error
	if mailboxID != nil {
		err = q.QueryRow(ctx, `select count(*) from mail.delivery_job
			where org_id=$1 and mailbox_id=$2 and is_dsn=false and created_at > $3`,
			orgID, *mailboxID, since).Scan(&n)
	} else {
		err = q.QueryRow(ctx, `select count(*) from mail.delivery_job
			where org_id=$1 and is_dsn=false and created_at > $2`, orgID, since).Scan(&n)
	}
	return n, err
}

// BounceStats: hard-bounce rate over the rolling window (owner decision: hard bounces
// only — 4xx deferrals never count; minimum attempts before the breaker can trip).
func BounceStats(ctx context.Context, q Querier, mailboxID uuid.UUID, window time.Duration, minAttempts int) (attempts, hardBounces int, rate float64, tripped bool, err error) {
	err = q.QueryRow(ctx, `
		select count(*), count(*) filter (where status='bounced' and coalesce(last_msg,'') like '5%')
		from mail.delivery_job
		where mailbox_id=$1 and is_dsn=false and updated_at > now() - make_interval(secs => $2)`,
		mailboxID, int(window.Seconds())).Scan(&attempts, &hardBounces)
	if err != nil {
		return 0, 0, 0, false, err
	}
	if attempts < minAttempts {
		return attempts, hardBounces, 0, false, nil
	}
	rate = float64(hardBounces) / float64(attempts) * 100
	return attempts, hardBounces, rate, rate > 0, nil
}

// BounceRatePct returns the hard-bounce rate for a mailbox over the window,
// keyed on recorded DSNs: attempts are completed attempts only (a queued,
// deferred or in-flight job is not an attempt), and "hard" is the is_hard
// flag the DSN carried — not a guess from the last message text.
func BounceRatePct(ctx context.Context, q Querier, mailboxID uuid.UUID, window time.Duration) (attempts, hard int, pct float64, err error) {
	err = q.QueryRow(ctx, `
		select count(*), count(*) filter (where d.is_hard)
		from mail.dsn d
		join mail.delivery_job j on j.id = d.delivery_job_id
		where j.mailbox_id=$1 and j.is_dsn=false and d.created_at > now() - make_interval(secs => $2)`,
		mailboxID, int(window.Seconds())).Scan(&attempts, &hard)
	if err != nil {
		return 0, 0, 0, err
	}
	if attempts == 0 {
		return 0, 0, 0, nil
	}
	return attempts, hard, float64(hard) / float64(attempts) * 100, nil
}

func RecordRateEvent(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, mailboxID *uuid.UUID, kind, detailJSON string) error {
	_, err := tx.Exec(ctx, `insert into mail.rate_event (id, org_id, mailbox_id, kind, detail) values ($1,$2,$3,$4,$5::jsonb)`,
		uuid.New(), orgID, optUUID(mailboxID), kind, detailJSON)
	return err
}

// ListSuppressionAll lists suppressions across orgs (admin console, register §2.4).
func ListSuppressionAll(ctx context.Context, q Querier, limit int) ([]model.Suppression, error) {
	rows, err := q.Query(ctx, `select org_id, email, reason, created_at from mail.suppression order by created_at desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Suppression
	for rows.Next() {
		var s model.Suppression
		if err := rows.Scan(&s.OrgID, &s.Email, &s.Reason, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
