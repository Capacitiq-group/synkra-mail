# Synkra Email — Owner decisions locked (2026-10-20)

Recorded verbatim-in-intent from the owner's decision table; implemented in
this repository as noted. Bias: security, deliverability, predictable costs,
manageable first release.

| # | Decision | Value | Implemented in |
|---|----------|-------|----------------|
| 1 | Product domain | `synkra.email`, pending DNS ownership/control confirmation; `trk.synkra.email` reserved for tracking | `config.PlatformDomain`, `EnsurePlatformDomain` |
| 2 | S3 region & residency | Keep the §8.4 check result but require documented confirmation + formal §17.1 sign-off **before any customer data flows** | **STILL PENDING — pre-VPS checklist item** |
| 3 | IMAP ports | IMAPS **993 only** at launch; no 143. Submission 587 STARTTLS | `config.IMAPAddr`, prod listener plan in README |
| 4 | App passwords | Max **10 active** per user, individually labelled + revocable, **180-day** expiry | `authn.CreateAppPassword`, `config.AppPassword*` |
| 5 | Trial | **14 days**, outbound sending disabled until payment or explicit admin approval | `ProvisionOrg` (trial subscription), `ResolveSendingState` |
| 6 | Shared files area | Phase 1.5, after core email is stable | not built (correct) |
| 7 | XOAUTH2/OAuth for IMAP | Deferred until after launch | not built (correct) |
| 8 | Dedicated IP | Purchase trigger = Stage B, earlier only on demonstrated reputation problems | config/ops, no code |
| 9 | R19/GB overage | **Manual invoicing** for the initial cohort, per **GB started** per billing month, disclosed before purchase; auditable usage record | `storage_ledger`, `OrgUsageBytes` |
| 10 | Grace buffer + bounce pause | Keep 10% grace buffer and 5% **hard-bounce** pause as initial defaults — documented, monitored, not immutable | `ingest.CheckQuota`, `checkBreakerLocked` |

### Interpretation notes locked with the decisions

- **Storage overage**: included 10 GB; temporary grace to 11 GB; notify at
  80/90/100%; record overage usage; never silently delete. R19 applies to each
  additional GB started per billing month (favouring straightforward invoicing).
- **Bounce pause**: the 5% threshold is **hard bounces only** (5xx on RCPT/DATA),
  over a rolling window with a **minimum message volume** before it can trip;
  pausing alerts the owner and provides a review/recovery path.
- **Tracking**: optional and **off by default** until privacy implications and
  customer controls are fully defined; per-message opt-out; the tracking host
  is separate from the mailbox/login domain.

### Engineering decisions from the 2026-11-25 audit (owner to confirm)

| # | Topic | Decision implemented |
|---|-------|----------------------|
| A | Outbound TLS | Opportunistic but never silently downgraded: a certificate that fails verification gets exactly one unverified-TLS attempt (still encrypted); a host without STARTTLS is logged and used in cleartext; after a failed TLS handshake we never fall back to cleartext. |
| B | Server identity | One knob, `SYNKRA_MAIL_HOSTNAME` (default `mx1.synkra.co.za`): EHLO name, bounce Reporting-MTA, Message-ID domain and the DNS-doctor MX target. (`mx1.synkra.email` did not resolve at the DNS check.) |
| C | Retry schedule | Attempt N gets the Nth delay of the §5.2 table (1m … 48h); previously the first retry fired immediately. |
| D | Interrupted deliveries | Jobs whose 10-minute claim lease expires are reclaimed and retried (a worker crash used to strand them forever). N workers run per config, and shutdown drains the queue for up to 30 s. |
| E | Suppression list | A suppressed recipient is skipped and reported per recipient instead of aborting the whole message; all-suppressed refuses. One malformed address still rejects the whole message. |
| F | Bounce-pause volume | The breaker's denominator is completed attempts recorded as DSNs, with hard bounces keyed on the DSN `is_hard` flag — queued/deferred jobs no longer inflate the denominator. |
| G | Bounce-pause accounting | A hard bounce is recorded and counted under a row lock on the mailbox, so concurrent workers count attempts one after another. Without the lock two workers could each miss the other's uncommitted DSN and both skip the pause. |
| H | Login identity (§1.2) | The schema keeps login e-mail unique per organisation; login resolves duplicates by domain ownership and refuses a truly ambiguous login with a 409 instead of silently picking one. |
| I | Suspended-org access (§1.3) | A suspended org keeps exactly logout and the /me status read; the old \"/admin/\" prefix test could never match paths under /api/v1, so it was dead code. |
| J | Reserved platform addresses (§1.5) | postmaster, abuse, admin, hostmaster, webmaster and security cannot be registered on the platform domain by any organisation (RFC 2142); they stay platform-operated. Custom domains are unaffected. |
| K | Idempotency keys (§1.9) | POST /send, address (alias) creation and domain create/verify accept an Idempotency-Key header; the first response is stored per user + key and replayed; a key reused with a different body is a 422. |
| L | App-password lockout (§1.6) | IMAP and submission logins share the web login's 10-fails/10-minute lockout. Submission keys it by login identity + source IP; IMAP keys it by login identity (go-imap v2 beta.8 exposes no remote address to sessions). |
| M | Message-list pagination (§1.8) | GET /messages pages by uid cursor (limit, default 50 / max 200; before_uid), newest page first, ascending order inside a page — the folder is never loaded whole. |
| N | Org-invite decline (§2.1) | There is no pending user-invite object to decline (invites create users immediately); "decline" maps to the org review application: POST /admin/orgs/{id}/decline plus a review_status filter on the queue. |
| O | Quarantine release (§2.11) | Release removes the quarantine row and audits it; re-injecting the held message lands with the spam verdict engines (hook points only, disabled by design for the pilot). |
| P | Attachment API (§2.10) | The list + download routes serve existing attachment blobs; mimeutil.Att gained JSON tags so the send API reads content_type correctly. |
| Q | Bare-LF wire normalisation (§3.7) | go-smtp passes DATA bytes through unmodified, so bare LF was stored and relayed verbatim; DeliverInbound and submission queueing now normalise to CRLF (RFC 5321), verified by a wire-level test. |
| R | Restore drill (§3.5) | cmd/restorecheck dumps every mail table, rebuilds the schema from migrations in a scratch DB, restores, verifies counts and a write probe; the sandbox's embedded Postgres has no pg_dump, so the drill is dependency-free Go. Production keeps pg_dump + WAL archiving per the runbook. |
| S | IMAP DELETE with children (§5.2) | A parent folder with child folders refuses DELETE with a clear NO instead of leaving a half-deleted hierarchy; CREATE auto-creates intermediate levels; RENAME moves children with the parent; LIST emits \HasChildren/\HasNoChildren. INBOX cannot be renamed or deleted. |
| T | APPEND admission (§5.1) | APPEND now obeys the same admission as the send path: oversize is rejected with TOOBIG (the old reader silently truncated), the org storage quota gates the append, and bare LF is normalised like the wire paths. |
| U | Delivery event log schema (§7.3, decision 17.4) | Locked in migration 005: mail.delivery_event (message_id, org_id, mailbox_id, state, detail, occurred_at) with states queued → delivered | bounced | deferred; "submitted"/"authenticated" fold into "queued" for v1 because admission and queueing share one transaction. Rows are written inside the transactions that move the jobs, so trace and truth cannot diverge. |
| V | Feature endpoints (§12) | Signatures, autoresponder (one reply per sender per mailbox per day, daemon paths skipped), inbound routing rules (first match in position order overrides the folder, system injections skip the engine), Mail Trace and the deliverability dashboard all read the primitives built in phases 1-5. Autoresponder replies go through normal admission, so a suspended org stays quiet. |
| W | One-click unsubscribe (§12e) | Sends flagged newsletter:true carry RFC 2369 List-Unsubscribe/Post headers when there is exactly one envelope recipient (a batch send cannot name a per-recipient token); the isolated tracker verifies the HMAC, forwards to the core, the core suppresses and audits. |
| X | Webhooks + API keys (§13f) | Webhook deliveries fan out inside the same transaction that writes the event; the worker POSTs with X-Synkra-Signature (sha256 HMAC of the body, secret shown once), retries with backoff and drops after 5 attempts. API keys (sk_ tokens) authenticate through requireSession with normal tenancy checks; they cannot change credentials and are revocable instantly. |
| Y | Composite health score (§13a) | 0-100 from the last 30 days: delivery rate (70), bounce hygiene (20), list pressure (10); components published beside the score — never one opaque number. |

### Explicitly still pending

1. **S3 data-residency sign-off** (owner item 2) — cannot be self-approved.
2. `synkra.email` DNS ownership verification ( registrar/DNS control check).
3. Contabo §8.4 checklist: outbound port 25 in writing, self-service rDNS,
   IP reputation pre-check on the additional IP, backup-scope confirmation.
