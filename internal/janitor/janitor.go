// Package janitor: the scheduled cleanup jobs (build register §3.1). Every
// table it touches grows monotonically without it: expired sessions, login
// attempts, rate events, idempotency keys, and zero-ref blobs past their
// 7-day purge grace (the queries existed; nothing scheduled them).
package janitor

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/store"
)

type Config struct {
	Interval                  time.Duration // loop cadence (default 1h)
	LoginAttemptRetentionDays int           // default 30
	RateEventRetentionDays    int           // default 90
	IdempotencyRetentionDays  int           // default 7
	BlobBatch                 int           // blobs deleted per tick (default 500)
}

type Janitor struct {
	Store *store.Store
	Blobs blob.Store
	Cfg   Config
}

// Run ticks until the context is cancelled.
func (j *Janitor) Run(ctx context.Context) {
	interval := j.Cfg.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	// One sweep at startup, then on the cadence.
	j.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs one full sweep; exported for the E2E harness.
func (j *Janitor) Tick(ctx context.Context) {
	j.expireSessions(ctx)
	j.prune("login_attempt", j.Cfg.LoginAttemptRetentionDays, ctx)
	j.prune("rate_event", j.Cfg.RateEventRetentionDays, ctx)
	j.prune("idempotency", j.Cfg.IdempotencyRetentionDays, ctx)
	j.pruneSendCounters(ctx)
	j.domainLifecycle(ctx)
	j.gcBlobs(ctx)
}

// pruneSendCounters drops the atomic limit counters once their window is past
// (they are day/hour keyed, so the table only ever holds a bounded recent set).
func (j *Janitor) pruneSendCounters(ctx context.Context) {
	n, err := store.PruneSendCounters(ctx, j.Store.Pool, time.Now().Add(-48*time.Hour))
	if err != nil {
		log.Printf("[janitor] prune send counters: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[janitor] pruned %d send counter row(s)", n)
	}
}

// domainLifecycle: unverified registrations expire and release the name, and
// verified domains come up for ownership re-verification (audit item 11 — a
// lapsed domain stayed with its old owner forever).
func (j *Janitor) domainLifecycle(ctx context.Context) {
	released, err := store.ReleaseExpiredDomains(ctx, j.Store.Pool, time.Now())
	if err != nil {
		log.Printf("[janitor] release expired domains: %v", err)
	} else if len(released) > 0 {
		log.Printf("[janitor] released %d expired unverified domain(s): %v", len(released), released)
	}
	flagged, err := store.FlagDomainsForReverify(ctx, j.Store.Pool, time.Now())
	if err != nil {
		log.Printf("[janitor] flag reverify domains: %v", err)
	} else if len(flagged) > 0 {
		log.Printf("[janitor] flagged %d domain(s) for ownership re-verification: %v", len(flagged), flagged)
		for _, name := range flagged {
			j.rateEventForDomain(ctx, name, "domain_reverify_due",
				"ownership re-verification due; mail keeps flowing during the grace window")
		}
	}
	// A 'reverify' domain that stayed unconfirmed for 60 more days is disabled:
	// the DNS token must actually be kept published, not done once.
	ct, err := j.Store.Pool.Exec(ctx, `update mail.domain set status='disabled', updated_at=now()
		where status='reverify' and verified_at is not null and verified_at < now() - interval '60 days'`)
	if err != nil {
		log.Printf("[janitor] disable stale reverify domains: %v", err)
	} else if ct.RowsAffected() > 0 {
		log.Printf("[janitor] disabled %d domain(s) that failed re-verification", ct.RowsAffected())
	}
}

// rateEventForDomain records an owner-visible alert row for a domain event.
func (j *Janitor) rateEventForDomain(ctx context.Context, domainName, kind, note string) {
	ct, err := j.Store.Pool.Exec(ctx, `insert into mail.rate_event (id, org_id, kind, detail)
		select $1, org_id, $2, $3::jsonb from mail.domain where name=$1 and org_id is not null
		on conflict do nothing`, uuid.New(), kind, `{"note":"`+note+`","domain":"`+domainName+`"}`)
	_ = ct
	if err != nil {
		log.Printf("[janitor] rate event for domain %s: %v", domainName, err)
	}
}

// expireSessions marks sessions past their expiry as revoked so verification
// stops touching them and the index stays meaningful.
func (j *Janitor) expireSessions(ctx context.Context) {
	ct, err := j.Store.Pool.Exec(ctx,
		`update mail.session set revoked_at=now() where expires_at < now() and revoked_at is null`)
	if err != nil {
		log.Printf("[janitor] expire sessions: %v", err)
		return
	}
	if ct.RowsAffected() > 0 {
		log.Printf("[janitor] expired %d session(s)", ct.RowsAffected())
	}
}

func (j *Janitor) prune(table string, days int, ctx context.Context) {
	if days <= 0 {
		return
	}
	q := `delete from mail.` + table + ` where created_at < now() - make_interval(days => $1)`
	ct, err := j.Store.Pool.Exec(ctx, q, days)
	if err != nil {
		log.Printf("[janitor] prune %s: %v", table, err)
		return
	}
	if ct.RowsAffected() > 0 {
		log.Printf("[janitor] pruned %d %s row(s) older than %dd", ct.RowsAffected(), table, days)
	}
}

// gcBlobs deletes zero-ref blobs whose 7-day purge grace has elapsed: the file
// first, then the row (a missing file releases the row too — content is
// content-addressed, so a dangling row without a file is only a counter).
func (j *Janitor) gcBlobs(ctx context.Context) {
	batch := j.Cfg.BlobBatch
	if batch <= 0 {
		batch = 500
	}
	shas, err := store.ListPurgeableBlobs(ctx, j.Store.Pool, batch)
	if err != nil {
		log.Printf("[janitor] list purgeable blobs: %v", err)
		return
	}
	for _, sha := range shas {
		if err := j.Blobs.Delete(ctx, sha); err != nil {
			log.Printf("[janitor] blob delete %s…: %v", sha[:12], err)
			continue
		}
		if _, err := j.Store.Pool.Exec(ctx, `delete from mail.blob where sha256=$1`, sha); err != nil {
			log.Printf("[janitor] blob row delete %s…: %v", sha[:12], err)
		}
	}
	if len(shas) > 0 {
		log.Printf("[janitor] garbage-collected %d blob(s)", len(shas))
	}
}
