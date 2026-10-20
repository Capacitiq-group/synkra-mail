# Synkra Email — Backend

The self-operated mail platform from `Synkra-Email-Design.md` (v1.2): our SMTP,
our IMAP, our storage, our queues, our delivery. This repository is the
**backend only** (webmail UI comes later by owner instruction).

**Language:** Go (design doc §3.2 primary recommendation) · **Database:** Postgres
(`mail` schema) · **Blobs:** content-addressed, FS adapter (dev) / S3 adapter
(any S3-compatible endpoint — Contabo Z1 in production) · **Protocols:** go-smtp,
go-imap v2, go-msgauth (DKIM), blitiri SPF, enmime (MIME).

## Quick start (dev)

```bash
# 1. Postgres (userland binaries, no root needed)
PGBIN=/home/z/my-project/tooling/pg/node_modules/@embedded-postgres/linux-x64/native/bin
export LD_LIBRARY_PATH=/home/z/my-project/tooling/pg/node_modules/@embedded-postgres/linux-x64/native/lib
$PGBIN/pg_ctl -D /home/z/my-project/devdata/pgdata -l /tmp/pg.log \
  -o "-p 5433 -c listen_addresses=127.0.0.1" start

# 2. Build + seed (platform domain + optional admin from env)
source env.sh
export SYNKRA_ADMIN_EMAIL=admin@synkra.co.za SYNKRA_ADMIN_PASSWORD=change-me
go run ./cmd/maild -seed

# 3. Run everything (API, MX, submission, IMAP, worker, tracker)
go run ./cmd/maild
```

Listeners (dev defaults, production ports in parentheses):

| Service    | Dev            | Prod        |
|------------|----------------|-------------|
| HTTP API   | 127.0.0.1:8080 | 443/80      |
| Inbound MX | 127.0.0.1:2025 | 25          |
| Submission | 127.0.0.1:2587 | 465 (TLS) / 587 (STARTTLS) |
| IMAP       | 127.0.0.1:1143 | 993 only (owner decision: no 143) |
| Tracker    | 127.0.0.1:8081 | trk.synkra.email (isolated) |

## Tests

```bash
source env.sh
go test ./... -count=1          # unit + full-stack E2E (boots every daemon in-process)
```

The E2E suite (`internal/e2e`) covers: send→deliver loop, inbound SMTP routing
(550/554/552), submission with app passwords, a full IMAP session
(LIST/SELECT/FETCH/STORE/SEARCH/APPEND/COPY/MOVE/EXPUNGE), IDLE wake-on-delivery,
the storage grace buffer (552), new-org sending limits, the 5% hard-bounce
circuit breaker + suppression, impersonation guardrails + audit, the tracking
flow through the isolated tracker (3-label classifier), tracking-off-by-default,
and trial-blocks-sending.

Health probes: `GET /healthz` (process liveness) and `GET /readyz`
(database ping + end-to-end blob-store probe) on the API listener.

## Layout

```
cmd/maild        all-in-one server (Stage-A single-VPS topology, §14.1) + -seed
cmd/tracker      isolated tracking service binary (no DB credentials, §9.1)
internal/api     webmail/admin HTTP API v1 (chi) + internal tracker-ingest endpoint
internal/authn   mail-owned login, sessions, app passwords, impersonation
internal/blob    content-addressed blob interface
internal/config  env-driven configuration
internal/db      pgx pool + ordered SQL migrations
internal/events  Postgres LISTEN/NOTIFY hub (IDLE without polling)
internal/fsblob  filesystem blob store (dev + scratch)
internal/imapd   IMAP server (go-imap v2 backend; shared per-folder trackers)
internal/ingest  inbound pipeline: routing, quota/grace, verdicts, storage
internal/krypto  argon2id, tokens, HMAC, secret sealing
internal/mimeutil MIME parse (enmime) + outbound message building
internal/model   domain types
internal/outbound admission (limits/breaker/suppression), DKIM, queue worker, DSN
internal/s3blob   S3-compatible blob store (dependency-free SigV4; Contabo/AWS/MinIO)
internal/smtpin  inbound MX + submission (go-smtp backends; policy is ours)
internal/store   Postgres data access (tenancy-scoped queries)
migrations/      001 schema, 002 tracking toggle, 003 send-as backfill
```

## Operating law encoded here (owner policy)

- **Storage**: warnings at 80/90/100%, 10% grace buffer, `552` beyond the
  buffer, **never auto-delete**, sending blocked only after the buffer (§4.4).
- **Outbound tiers** (§7.3): new org 100/day per org · 20/hour per mailbox ·
  50 recipients/message; verified+paid 500/day per mailbox · 2,000/day per org.
- **Bounce circuit breaker**: ≥5% hard-bounce rate over a rolling window with a
  minimum volume ⇒ mailbox auto-paused (`paused_bounces`), owner alerted;
  deferrals never count as bounces.
- **Signup gates**: no sending without payment or admin approval (review
  queue); domain verification before custom-domain sending; 14-day trial.
- **Auth**: app passwords only on IMAP/submission (main password refused),
  max 10 active per user, 180-day expiry, argon2id hashes, login lockout.
- **Tracking**: org toggle **off by default**, per-message opt-out, HMAC-signed
  pixel/click URLs, isolated tracker service, Opened / Likely opened /
  Possibly automated labels (§9).
- **Admin**: staff-only API surface, 60-minute impersonation sessions, no
  credential changes while impersonating, everything audited (§11).

## Production wiring (Stage A — when the VPS is ordered)

1. Run the §8.4 pre-purchase checklist (port 25 in writing, rDNS, IP
   reputation, Z1 S3 region + residency sign-off).
2. Configure TLS (ACME certs) via `SYNKRA_MAIL_TLS_CERT`/`_KEY`; listeners then
   map to 25/465/587/993. `SYNKRA_MAIL_ENV=prod` forbids insecure auth and
   default secrets.
3. Point `SYNKRA_MAIL_BLOB_MODE=s3` and set `SYNKRA_MAIL_S3_ENDPOINT`,
   `_REGION`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY` (path-style on by
   default; works with Contabo Object Storage, MinIO, AWS). The endpoint/
   bucket are deployment config — set them only after the residency sign-off.
4. Publish DNS: MX, SPF include, DKIM `sk1`, DMARC, MTA-STS, TLS-RPT, PTR.
5. Start IP warmup (§8.3) with `SYNKRA_MAIL_WARMUP_DAILY_CAP` set.

## Deliberate v1 scope notes

- IMAP presents the user's own mailbox (folders as IMAP mailboxes); shared
  mailboxes are readable/writable via the API and IMAP-fanout comes later.
- ClamAV/DNSBL hook points exist in the verdict struct but the engines are
  disabled by default until the Stage-A memory budget is proven (§14.2).
- ARC sealing, DMARC report parsing, Sieve rules, JMAP: post-v1 (design doc).
- Billing is manual (overage recorded in `storage_ledger`); Paystack reserved.
