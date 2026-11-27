package store

// Delivery analytics (register §12f deliverability dashboard, §13a health
// score, §13b reliability monitor). All queries are org-scoped and windowed;
// none touch message content.

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DeliverabilityStats: the org's sending funnel over a window.
type DeliverabilityStats struct {
	Sent         int64          `json:"sent"`         // delivery jobs created
	Delivered    int64          `json:"delivered"`    // jobs that completed delivered
	Bounced      int64          `json:"bounced"`      // hard bounces (DSN kind=bounce, is_hard)
	SoftExpired  int64          `json:"soft_expired"` // retries exhausted
	Deferred     int64          `json:"deferred"`     // currently queued/deferred/in flight
	Suppressed   int64          `json:"suppressed"`   // suppression rows
	DeliveryRate float64        `json:"delivery_rate"`
	ByDomain     []DomainFunnel `json:"by_domain"`
	WindowDays   int            `json:"window_days"`
}

type DomainFunnel struct {
	Domain    string `json:"domain"`
	Sent      int64  `json:"sent"`
	Delivered int64  `json:"delivered"`
	Bounced   int64  `json:"bounced"`
}

func GetDeliverabilityStats(ctx context.Context, q Querier, orgID uuid.UUID, days int) (*DeliverabilityStats, error) {
	s := &DeliverabilityStats{WindowDays: days}
	err := q.QueryRow(ctx, `select
			count(*),
			count(*) filter (where status='delivered'),
			count(*) filter (where status in ('queued','deferred','in_flight')),
			count(*) filter (where status='bounced')
		from mail.delivery_job where org_id=$1 and created_at > now() - make_interval(days => $2)`,
		orgID, days).Scan(&s.Sent, &s.Delivered, &s.Deferred, &s.Bounced)
	if err != nil {
		return nil, err
	}
	err = q.QueryRow(ctx, `select count(*) from mail.dsn d
		join mail.delivery_job j on j.id = d.delivery_job_id
		where j.org_id=$1 and d.is_hard and d.created_at > now() - make_interval(days => $2)`,
		orgID, days).Scan(&s.Bounced)
	if err != nil {
		return nil, err
	}
	err = q.QueryRow(ctx, `select count(*) from mail.dsn d
		join mail.delivery_job j on j.id = d.delivery_job_id
		where j.org_id=$1 and d.kind='bounce' and not d.is_hard and d.created_at > now() - make_interval(days => $2)`,
		orgID, days).Scan(&s.SoftExpired)
	if err != nil {
		return nil, err
	}
	err = q.QueryRow(ctx, `select count(*) from mail.suppression where org_id=$1`, orgID).Scan(&s.Suppressed)
	if err != nil {
		return nil, err
	}
	if s.Sent > 0 {
		s.DeliveryRate = float64(s.Delivered) / float64(s.Sent)
	}
	rows, err := q.Query(ctx, `select
			split_part(j.envelope_to, '@', 2) as dom,
			count(*),
			count(*) filter (where j.status='delivered'),
			count(*) filter (where j.status='bounced')
		from mail.delivery_job j
		where j.org_id=$1 and j.created_at > now() - make_interval(days => $2)
		group by dom order by count(*) desc limit 20`, orgID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f DomainFunnel
		if err := rows.Scan(&f.Domain, &f.Sent, &f.Delivered, &f.Bounced); err != nil {
			return nil, err
		}
		s.ByDomain = append(s.ByDomain, f)
	}
	return s, rows.Err()
}

// OrgHealth: one composite 0-100 score (register §13a): delivery rate and
// hard-bounce rate over 30 days, plus the suppression-list pressure.
type OrgHealth struct {
	Score          int     `json:"score"`
	DeliveryRate   float64 `json:"delivery_rate"`
	HardBounceRate float64 `json:"hard_bounce_rate"`
	Sent           int64   `json:"sent"`
	Suppressed     int64   `json:"suppressed"`
	WindowDays     int     `json:"window_days"`
}

func GetOrgHealth(ctx context.Context, q Querier, orgID uuid.UUID, days int) (*OrgHealth, error) {
	stats, err := GetDeliverabilityStats(ctx, q, orgID, days)
	if err != nil {
		return nil, err
	}
	h := &OrgHealth{
		DeliveryRate: stats.DeliveryRate,
		Sent:         stats.Sent,
		Suppressed:   stats.Suppressed,
		WindowDays:   days,
	}
	if stats.Sent > 0 {
		h.HardBounceRate = float64(stats.Bounced) / float64(stats.Sent)
	}
	// Composite: delivery rate is worth 70 points, bounce hygiene 20, list
	// hygiene 10 (register §13a: one honest number, the components beside it).
	score := 0.0
	if stats.Sent > 0 {
		score += 70 * stats.DeliveryRate
	} else {
		score += 70 // no sending history: neutral, not punitive
	}
	score += 20 * (1 - h.HardBounceRate)
	if h.Suppressed == 0 {
		score += 10
	} else if pressure := float64(h.Suppressed) / float64(stats.Sent+1); pressure < 0.05 {
		score += 10 * (1 - pressure/0.05)
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	h.Score = int(score)
	return h, nil
}

// ReliabilityDay: one day of the org's delivery outcomes (register §13b).
type ReliabilityDay struct {
	Day       string `json:"day"`
	Queued    int64  `json:"queued"`
	Delivered int64  `json:"delivered"`
	Bounced   int64  `json:"bounced"`
	Deferred  int64  `json:"deferred"`
}

func GetReliability(ctx context.Context, q Querier, orgID uuid.UUID, days int) ([]ReliabilityDay, error) {
	rows, err := q.Query(ctx, `select to_char(d.day, 'YYYY-MM-DD'),
			coalesce(count(*),0),
			coalesce(count(*) filter (where e.state='delivered'),0),
			coalesce(count(*) filter (where e.state='bounced'),0),
			coalesce(count(*) filter (where e.state='deferred'),0)
		from generate_series((now() - make_interval(days => $2 - 1))::date, now()::date, '1 day') d(day)
		left join mail.delivery_event e on e.org_id=$1 and e.occurred_at >= d.day and e.occurred_at < d.day + interval '1 day'
		group by d.day order by d.day`, orgID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReliabilityDay
	for rows.Next() {
		var r ReliabilityDay
		if err := rows.Scan(&r.Day, &r.Queued, &r.Delivered, &r.Bounced, &r.Deferred); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var _ = time.Now // keep the time import honest for the struct types above
