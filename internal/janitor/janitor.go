// Package janitor: the scheduled cleanup jobs (build register §3.1). Every
// table it touches grows monotonically without it: expired sessions, login
// attempts, rate events, idempotency keys, and zero-ref blobs past their
// 7-day purge grace (the queries existed; nothing scheduled them).
package janitor

import (
	"context"
	"log"
	"time"

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
	j.gcBlobs(ctx)
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
