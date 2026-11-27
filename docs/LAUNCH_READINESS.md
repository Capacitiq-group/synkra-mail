# Launch Readiness Assessment — 27 November 2026

Status of the platform against the **27 November 2026 external audit** (the
second audit; the first, 26 November, produced the consolidated build register
and is fully landed as phases 0–7). This document is the pass/fail map: every
audit item, where it was fixed, how it is proven, and what still remains.
Verification for everything below: `go vet ./...` and `go test ./... -count=1`
green on this commit (the E2E suite boots the whole stack — API, MX,
submission, IMAP, worker, tracker — against a disposable Postgres).

---

## 1. Critical — breaks core function or exposes tenants

| # | Finding | Status | Where / proof |
|---|---|---|---|
| 1 | Outbound TLS broken: no STARTTLS command before the handshake | **fixed + tested** | `internal/outbound/worker.go` `connect()` now sends `STARTTLS`, reads the `220`, then handshakes (RFC 3207 4). `TestSTARTTLSCommandSequence` delivers through a **real go-smtp TLS server** (self-signed cert, STARTTLS advertised) — the old code fails that test exactly as the audit described. **Owner task remains:** one manual send against Gmail/Outlook from the VPS once the sandbox restriction (no outbound :25) is gone; the wire sequence is now provably correct. |
| 2 | Bcc / envelope-only recipients bypassed suppression, validation, limits | **fixed + tested** | `Pipeline.QueueSubmission` always merges envelope recipients into admission (validation, suppression, limit reservation); `subSession.Rcpt` refuses invalid syntax at the wire. `TestEnvelopeBccAdmission`, `TestSecondFromRejected`. |
| 3 | Impersonation guardrails cosmetic | **fixed + tested** | Every mail-content read (`/messages`, `/messages/{id}`, `/raw`, `/search`, `/tracking`) is audited under impersonation **fail-closed**; impersonated sessions are refused on `/users/invite`, `/addresses`, `/domains`, `/org/webhooks`, `/org/tracking`, shared-mailbox membership, API keys, app passwords, password change; `requireStaff` rejects non-`web` sessions. **MFA:** TOTP enrol → activate → mandatory code at login (`TestMFALoginFlow`, `TestImpersonationBlocksBackdoors`). |
| 4 | DKIM private keys plaintext | **fixed + tested** | Keys sealed AES-256-GCM (`enc1.` prefix, `SYNKRA_MAIL_KMS_KEY`; prod refuses to boot without it). Rotation is staged → publish → activate-after-DNS-check (`/domains/{id}/rotate-dkim`, `/activate-dkim`). `TestDKIMKeySealedAtRest`. |
| 5 | Unauthenticated memory DoS | **fixed** | 1 MiB default body cap on every API route (`limitBody`), `/send` keeps its message-size cap, tracker endpoint caps at 64 KiB **before** the HMAC check and verifies the signature first. Full `ReadTimeout`/`WriteTimeout`/`IdleTimeout` on the API and tracker servers. Tracker is excluded from the default prod roles — run it as `maild -only tracker` (no DB credentials reachable). Inbound SMTP got a connection cap (`SYNKRA_MAIL_MAX_INBOUND_CONNS`, default 256). |
| 6 | Login throttle abusable | **fixed + tested** | Account lockout counts **that e-mail's failures only**; the IP gate is a separate soft 429, and X-Forwarded-For is trusted only with `SYNKRA_MAIL_TRUSTED_PROXY=true` (the chi `RealIP` middleware is likewise gated). `SYNKRA_MAIL_LOGIN_WINDOW` / `_LOCKOUT` are now wired (they were dead knobs). Indexes `(email, created_at)` and `(ip, created_at)` on `login_attempt`. |
| 7 | Config fails open | **fixed + tested** | `SYNKRA_MAIL_ENV` must be exactly `dev` or `prod` (anything else refuses to start); prod requires a 32-byte KMS key, TLS certs, non-default secrets of ≥32 chars with a minimal entropy check; the outbound transport must be named (`auto|loopback|smtp`) and `loopback` is refused in prod. |

## 2. High — security and data integrity

| # | Finding | Status | Where / proof |
|---|---|---|---|
| 8 | Hand-rolled DMARC | **fixed + tested** | `internal/smtpin/dmarc.go`: exact tag parsing (`sp=reject` no longer misread), organizational-domain fallback for subdomain policies, relaxed alignment over org domains (sibling subdomains align), **any aligned** DKIM signature passes, From parsed by `net/mail`. Unit-tested in `dmarc_test.go`. |
| 9 | DKIM/submission header weaknesses | **fixed + tested** | From/To/Cc/Subject **oversigned** (+ Content-Type now signed): `TestDKIMOversigningBreaksOnInjectedFrom` proves a forged second From breaks the signature. Submissions with duplicate From/Subject/Date are refused. CR/LF injection in `in_reply_to`, attachment `content_type`, filenames and content-ids is rejected in the builder. **Inline attachments** are included (`multipart/related` for HTML+inlines) instead of dropped. |
| 10 | Outbound SSRF | **fixed + tested** | Address literals refused end-to-end (`ValidAddress` + `pickMXHosts`); every MX target is resolved and checked against RFC 1918 / loopback / link-local / CGNAT / benchmark space before dialling (`egressAllowed`). `TestEgressFilterRefusesPrivateSpace`. |
| 11 | Domain handling flaws | **fixed** | Registration is org-admin-only with a per-org cap; RSA key generation moved **outside** the transaction; real FQDN validation (`ValidDomainName`); unverified registrations expire (janitor releases the name — the unique constraint is now partial on live rows); verified domains re-verify yearly (alert → grace → disable). |
| 12 | Role-address squatting | **fixed** | Reserved locals extended to `dmarc`, `dmarc-reports`, `noreply`, `no-reply`, `donotreply`, `mailer-daemon`, `root`, `support`, `help`, `billing`, `notifications`, `administrator` on the platform domain. Cross-tenant local-part collisions no longer exist as a confusion: login e-mails are **globally unique** (schema, item 13). |
| 13 | E-mail not globally unique; no verification | **fixed + tested** | `user_email_global_idx` makes the login identity globally unique (the ambiguous-login resolver stays as a fail-safe); `mail.verification_token` + `/me/verify-email` + `/auth/verify-email` implement the verification flow. Test updated to the new invariant. |
| 14 | Viewer can write | **fixed + tested** | `MailboxAccessWrite` (owner/member) gates flags, move, delete, folder create/delete and thread writes; `MailboxMemberRole` fixed and used. `TestViewerCannotWrite`. |
| 15 | Alias limit bypass | **fixed** | Aliases and forwards share the plan limit; `forward_target` must be a valid address. |
| 16 | Thread note/assign unscoped | **fixed** | Writes need write-level membership of the thread's mailboxes; the assignee must have mailbox access (which pins the org). |
| 17 | Delivery can duplicate | **fixed** | Job completion/deferral/bounce are guarded by `status='in_flight'` — a reclaimed lease converts to `ErrLeaseLost` and the loser's result is discarded, never written. |

## 3. High — delivery and abuse economics

| # | Finding | Status | Where / proof |
|---|---|---|---|
| 18 | IP-reputation failures look like invalid recipients | **fixed + tested** | `classifyBounce` reads enhanced status codes + reply text: `5.1.x` suppresses; `5.7.x` (blocklists, policy) **never suppresses** and never feeds the breaker — it bounces soft, records a `policy_reject` rate event and alerts the owner; `5.2.2` mailbox-full is a soft bounce. Suppression list + delete endpoint + owner-side mailbox resume exist; the breaker pause now sends the owner alert the design promised. `TestClassifyBounce`, `TestSuppressionManagement`. |
| 19 | Head-of-line blocking | **fixed** | Jobs in a batch run concurrently (`SYNKRA_MAIL_WORKER_CONCURRENCY`, default 8) with a per-destination-domain gate (max 2 in flight) — a few tarpits no longer stall all outbound mail. |
| 20 | Other delivery defects | **fixed** | MX hosts tried in preference order with failover; NXDOMAIN fails fast (permanent 550) instead of retrying 72 h; transient DNS stays deferrable; `SYNKRA_MAIL_OUTBOUND_SOURCE_IP` binds the sending socket; the warmup cap is enforced **per IP across all orgs** atomically; org/mailbox caps are single-statement atomic reservations (`mail.send_counter`) — no more read-then-send races. |
| 21 | Missing reputation tooling | **partial** | VERP envelope sender (`bnc+<job>@hostname`) correlates async bounces to the exact job; ARF `feedback-report` replies on a VERP path suppress as `complaint`. List-Unsubscribe + one-click already existed (register §12e). **Still open:** full FBL enrolment per provider, VERP bounce DSN parsing depth (delivery-status body parsing), outbound content scanning. |
| 22 | Wrong DNS instructions | **fixed + tested** | `/domains/{id}/dns` and the §4b template no longer tell customers to publish the report-authorisation record: per RFC 7489 §7.1 the platform publishes `<customer>._report._dmarc.synkra.email` **in the platform zone**; the SPF instruction says *merge the include, never add a second v=spf1*. `TestMailReadyTemplateShape`, `TestDoctorVerdicts`. |

## 4. Medium — scalability and correctness

| # | Finding | Status |
|---|---|---|
| 23 | Quota scan without org index | fixed: `message_org_idx` (migration 008). |
| 24 | Folder listing loads everything | already cursor-paginated in phase 1 (`ListItemsPage`, no `body_text` in the page query); re-verified this audit. |
| 25 | Permanent delete broken | fixed: `ExpungeItemNow` for the API Trash path (the `\Deleted` flag the API never set is no longer required); `purgeMessageRow` removes attachments, jobs, DSNs, tracking and delivery events and **releases every blob ref**. Blob GC was already scheduled (audit's "no caller" was stale). |
| 26 | tsvector >1 MB fails forever | fixed: the generated column now indexes `left(body_text, 400000)` etc. — oversized bodies index a prefix. |
| 27 | Search slow and loose | fixed: LIKE metacharacters escaped; `pg_trgm` GIN index on `subject`. |
| 28 | Claim query can't use its index | fixed: the partial index now covers `in_flight` too. |
| 29 | Other medium issues | fixed: advisory lock around migrations (`pg_advisory_lock`), pool size configurable (`SYNKRA_MAIL_DB_MAX_CONNS`), UIDVALIDITY now time-derived for new folders, submission read/write timeouts, inbound connection cap, helo already flowed from config. **Open:** migrations still read from a directory on disk (embed requires the files inside the package — a mechanical follow-up), multi-recipient inbound partial success (still all-or-nothing per message), S3 fetch inside the store transaction (unchanged). |

## 5. Missing features — honest state

- **Auth/account:** MFA (done), session list + revoke (done), e-mail verification (done). **Still open:** public signup, password reset (token table exists, the mail-out flow does not), user disable/remove surface beyond suspend, MFA recovery codes.
- **Mail handling:** plus-addressing still gets a 550; vacation/Sieve/autoconfig/MTA-STS *hosting* (the record is published, the HTTPS policy file is not) remain open.
- **Protocols/platform:** port 465 (`SubmissionTLSAddr`) still unserved; API TLS still terminates at the proxy; ACME reload still needs a restart. Deliberate: single-VPS Stage-A topology.
- **Operations:** CI, Dockerfile, Makefile, backup tooling and alerting landed in phase 3. Metrics/access-log/request-id-correlation remain open (logs are `log.Printf`; the error envelope already carries a request id).

## 6. Quality notes

- Error codes: `invalid_credentials` misuse for bad IDs replaced with `invalid_request`/`not_found`; duplicate detection still matches error text in two handlers (parameterized SQL everywhere — unchanged, correct).
- `parseIDParam` now genuinely enforces the id prefix (it did since phase 1 — audit was stale).
- No CHECK constraints / row-level security yet: tenancy still depends on handler checks (all verified). This is the biggest *architectural* gap that remains.

## 7. What this means for launch

The platform now survives the audit's three launch-blockers: mail actually
leaves (TLS sequence fixed and proven), tenants cannot poison each other
(suppression/limit/throttle surfaces behave), and a compromised staff password
is no longer a total compromise (MFA + backdoor closures). The remaining work
before the M1 pilot is operational, not architectural: run the tracker and DNS
edge as separate services, one real-network send test from the VPS, the owner
items that were never code (DNS records, IP warm-up, POPIA review), and the
short open list in §5.
