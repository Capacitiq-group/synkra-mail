package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- plans ----

const seedPlanID = "01900000-0000-7000-8000-000000000001" // business, seeded by migration

func GetPlanBySlug(ctx context.Context, q Querier, slug string) (*model.Plan, error) {
	row := q.QueryRow(ctx, `select id, slug, name, coalesce(price_cents,0), currency, "interval",
                mailbox_limit, alias_limit, shared_mailbox_limit, storage_total_mb, coalesce(storage_overage_cents_per_gb,0)
                from mail.plan where slug=$1`, slug)
	var p model.Plan
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.PriceCents, &p.Currency, &p.Interval,
		&p.MailboxLimit, &p.AliasLimit, &p.SharedMailboxLimit, &p.StorageTotalMB, &p.StorageOverageCentsPerGB)
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

func GetPlanByID(ctx context.Context, q Querier, id uuid.UUID) (*model.Plan, error) {
	row := q.QueryRow(ctx, `select id, slug, name, coalesce(price_cents,0), currency, "interval",
                mailbox_limit, alias_limit, shared_mailbox_limit, storage_total_mb, coalesce(storage_overage_cents_per_gb,0)
                from mail.plan where id=$1`, id)
	var p model.Plan
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.PriceCents, &p.Currency, &p.Interval,
		&p.MailboxLimit, &p.AliasLimit, &p.SharedMailboxLimit, &p.StorageTotalMB, &p.StorageOverageCentsPerGB)
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// ---- orgs ----

func CreateOrg(ctx context.Context, tx pgx.Tx, o *model.Org, trialDays int) error {
	// org + trial subscription in one transaction (Section 4.1 provisioning rule 1).
	_, err := tx.Exec(ctx, `insert into mail.org (id, name, plan_id, status, tier, sending_enabled, review_status)
                values ($1,$2,$3,'active','new',false,'pending')`,
		o.ID, o.Name, o.PlanID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into mail.subscription (id, org_id, plan_id, status, trial_ends_at)
                values ($1,$2,$3,'trial', now() + make_interval(days => $4))`,
		uuid.New(), o.ID, o.PlanID, trialDays)
	return err
}

func GetOrg(ctx context.Context, q Querier, id uuid.UUID) (*model.Org, error) {
	row := q.QueryRow(ctx, `select id, name, plan_id, status, tier, sending_enabled,
                coalesce(sending_blocked_reason,''), review_status, tracking_enabled, storage_overage_gb, synkra_org_id, created_at, updated_at
                from mail.org where id=$1`, id)
	var o model.Org
	err := row.Scan(&o.ID, &o.Name, &o.PlanID, &o.Status, &o.Tier, &o.SendingEnabled,
		&o.SendingBlockedReason, &o.ReviewStatus, &o.TrackingEnabled, &o.StorageOverageGB, &o.SynkraOrgID, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &o, nil
}

func SetOrgSending(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, enabled bool, reason string) error {
	_, err := tx.Exec(ctx, `update mail.org set sending_enabled=$2, sending_blocked_reason=$3, updated_at=now() where id=$1`,
		orgID, enabled, optText(reason))
	return err
}

func SetOrgTier(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, tier string) error {
	_, err := tx.Exec(ctx, `update mail.org set tier=$2, updated_at=now() where id=$1`, orgID, tier)
	return err
}

func SetOrgReviewStatus(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, status string) error {
	_, err := tx.Exec(ctx, `update mail.org set review_status=$2, updated_at=now() where id=$1`, orgID, status)
	return err
}

func SetOrgStatus(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, status string) error {
	_, err := tx.Exec(ctx, `update mail.org set status=$2, updated_at=now() where id=$1`, orgID, status)
	return err
}

func ListOrgs(ctx context.Context, q Querier, limit int) ([]model.Org, error) {
	rows, err := q.Query(ctx, `select id, name, plan_id, status, tier, sending_enabled,
                coalesce(sending_blocked_reason,''), review_status, tracking_enabled, storage_overage_gb, synkra_org_id, created_at, updated_at
                from mail.org order by created_at desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Org
	for rows.Next() {
		var o model.Org
		if err := rows.Scan(&o.ID, &o.Name, &o.PlanID, &o.Status, &o.Tier, &o.SendingEnabled,
			&o.SendingBlockedReason, &o.ReviewStatus, &o.TrackingEnabled, &o.StorageOverageGB, &o.SynkraOrgID, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- subscription ----

func GetActiveSubscription(ctx context.Context, q Querier, orgID uuid.UUID) (*model.Subscription, error) {
	row := q.QueryRow(ctx, `select id, org_id, plan_id, status, trial_ends_at, coalesce(paystack_ref,''),
                current_period_end, created_at
                from mail.subscription where org_id=$1 order by created_at desc limit 1`, orgID)
	var s model.Subscription
	err := row.Scan(&s.ID, &s.OrgID, &s.PlanID, &s.Status, &s.TrialEndsAt, &s.PaystackRef, &s.CurrentPeriodEnd, &s.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &s, nil
}

func SetSubscriptionStatus(ctx context.Context, tx pgx.Tx, subID uuid.UUID, status string, periodEnd *time.Time) error {
	_, err := tx.Exec(ctx, `update mail.subscription set status=$2, current_period_end=coalesce($3, current_period_end), updated_at=now() where id=$1`,
		subID, status, periodEnd)
	return err
}

// OrgSendingState summarizes whether outbound is permitted and which tier applies.
type OrgSendingState struct {
	Enabled   bool
	Tier      string // "new" | "verified"
	SubStatus string
	Reason    string
}

// ResolveSendingState: owner rule — payment before any sending, or explicit admin approval.
func ResolveSendingState(ctx context.Context, q Querier, org *model.Org) (*OrgSendingState, error) {
	sub, err := GetActiveSubscription(ctx, q, org.ID)
	if err != nil {
		return nil, err
	}
	st := &OrgSendingState{Tier: org.Tier, SubStatus: sub.Status}
	switch {
	case org.Status == "suspended":
		st.Reason = "org_suspended"
	case !org.SendingEnabled:
		if sub.Status == "active" {
			// Paid but flag not flipped: admin approval pending (review queue).
			st.Reason = "pending_review"
		} else {
			st.Reason = "payment_required"
		}
	case org.ReviewStatus == "declined":
		st.Reason = "review_declined"
	default:
		st.Enabled = true
	}
	return st, nil
}

// ListOrgsByReviewStatus filters the review queue (register §2.1).
func ListOrgsByReviewStatus(ctx context.Context, q Querier, reviewStatus string, limit int) ([]model.Org, error) {
	rows, err := q.Query(ctx, `select id, name, plan_id, status, tier, sending_enabled,
                coalesce(sending_blocked_reason,''), review_status, tracking_enabled, storage_overage_gb, synkra_org_id, created_at, updated_at
                from mail.org where review_status=$1 order by created_at desc limit $2`, reviewStatus, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Org
	for rows.Next() {
		var o model.Org
		if err := rows.Scan(&o.ID, &o.Name, &o.PlanID, &o.Status, &o.Tier, &o.SendingEnabled,
			&o.SendingBlockedReason, &o.ReviewStatus, &o.TrackingEnabled, &o.StorageOverageGB, &o.SynkraOrgID, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetOrgOwnerAddress resolves the org owner's primary mailbox address — the
// destination for breaker/policy owner alerts (Section 7.3, audit item 18).
func GetOrgOwnerAddress(ctx context.Context, q Querier, orgID uuid.UUID) (string, error) {
	var addr string
	err := q.QueryRow(ctx, `
		select a.local::text || '@' || d.name::text
		from mail.user u
		join mail.mailbox mb on mb.owner_user_id = u.id and mb.kind='user'
		join mail.address a on a.mailbox_id = mb.id and a.is_primary
		join mail.domain d on d.id = a.domain_id
		where u.org_id=$1 and u.status='active'
		order by case u.role when 'owner' then 0 when 'admin' then 1 else 2 end, u.created_at
		limit 1`, orgID).Scan(&addr)
	if err != nil {
		return "", notFound(err)
	}
	return addr, nil
}
