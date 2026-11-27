# Synkra Mail — Consolidated Build Register

<!-- BUILD-STATUS (update this table as each phase lands; keep the register body below verbatim) -->

## Build status — updated as work lands

| Phase | Register sections | Status | Commit | Date |
|---|---|---|---|---|
| 0 | Register adopted into the repository, status tracking added | **done** | this commit | 2026-11-26 |
| 1 | §1 API and authentication hardening (1.1–1.10) | planned | — | — |
| 2 | §2 Missing endpoints (2.1–2.13) | planned | — | — |
| 3 | §3 Operations, packaging and verification (3.1–3.10) | planned | — | — |
| 4 | §5 IMAP completeness (5.1–5.3) | planned | — | — |
| 5 | §7.3 delivery event log schema (decision 17.4) | planned | — | — |

Notes kept with the status:

- Verification for every code phase: `scripts/check.sh` (gofmt, build, vet,
  `go test -count=1 ./...`) must be fully green on the exact commit before it
  is committed; the E2E suite runs against the embedded Postgres.
- Items that are not code — DNS record changes (§4a), IP warm-up, hosting and
  the pre-purchase checklist, POPIA legal review, billing contracts, B-BBEE
  pack — stay with the owner and are tracked unchanged in the register below;
  they are not marked done by this build.

**Purpose.** Every requirement, fix and decision raised across the design conversation and the 26 November 2026 launch-readiness audit, deduplicated and ordered by importance. Priority reflects *what blocks what*, not effort.

**Priority legend**

| Tier | Meaning |
|---|---|
| **P0** | Blocks the internal pilot (M1). Nothing downstream can be validated without it. |
| **P1** | Blocks closed beta (M2), or is an architectural primitive everything else depends on. |
| **P2** | Core product value; blocks public launch (M3) for headline features. |
| **P3** | Differentiators and retention features; post-launch. |
| **P4** | Platform expansion; explicitly deferred. |

**What changed since the audit.** The decision to run our own authoritative DNS removes the Oner record-level limitation entirely and simplifies the registration workflow. It also promotes authoritative DNS from "configuration task" to "tier-0 service with its own SLA." Section 4b reflects this.

---

## Table of contents

1. API and authentication hardening — P0
2. The ten missing endpoints — P0
3. Operations, packaging and verification — P0
4. DNS: immediate fixes (4a) and authoritative DNS service (4b)
5. IMAP completeness — P0/P1
6. Frontend: webmail, admin console, onboarding — P1
7. Core platform primitives — P1
8. Security architecture — P1
9. Privacy and POPIA — P1
10. Domain reselling via Oner — P1
11. Billing, payments and VAT — P1
12. Feature register — P2
13. Differentiators — P3
14. Suite and expansion — P4
15. South African specifics — cross-cutting
16. Milestones and sequencing
17. Open decisions
18. Master priority table
19. Consolidated register appendix

---

## Master priority table

| # | Workstream | Tier | Source |
|---|---|---|---|
| 1 | API & auth hardening (8 items) | P0 | Audit §4.1 |
| 2 | Ten missing endpoints | P0 | Audit §4.2 |
| 3 | Operations: janitor, CI, Docker, backups, alerting | P0 | Audit §4.4 |
| 4 | DNS: immediate record fixes + PTR | P0 | Audit §6 |
| 5 | Authoritative DNS service | P0/P1 | New decision |
| 6 | IMAP completeness | P0/P1 | Audit §4.3 |
| 7 | Frontend: webmail, admin console, onboarding | P1 | Audit §5 |
| 8 | Core platform primitives (8) | P1 | Design conversation |
| 9 | Security architecture | P1 | Design conversation |
| 10 | Privacy & POPIA | P1 | Design conversation |
| 11 | Domain reselling via Oner | P1 | Oner docs |
| 12 | Billing, payments, VAT | P1 | Design conversation |
| 13 | Feature register: aliases, signatures, OOO/routing, unsubscribe/newsletters, deliverability dashboard | P2 | Design conversation |
| 14 | Differentiators: health score, reliability monitor, continuity, migration, archive, API/webhooks | P3 | Design conversation |
| 15 | Suite (calendar/contacts), white-label, native apps, domain registration | P4 | Design conversation |
| 16 | South African specifics | Cross-cutting | Design conversation |

---

# 1. API and authentication hardening — P0

From audit §4.1. All self-contained; all must close before the missing endpoints expose more surface on top of them.

| # | Item | Required behaviour | Consequence if shipped |
|---|---|---|---|
| 1.1 | ID prefix validation | Parse the prefix against the route's expected type; reject `msg_id` where a folder id is required | Cross-type id confusion weakens object-level authorisation |
| 1.2 | Login e-mail uniqueness | Resolve uniqueness **per organisation**; the lookup is currently global | Ambiguous login the moment two orgs share an address |
| 1.3 | Suspended-org middleware path bug | The exception tests for `/admin/` inside `/api/v1` paths that never start with it — fix path logic so the intended admin exception fires | Dead logic; suspended orgs may be over- or under-blocked |
| 1.4 | Thread notes & assignment membership | Check membership of the **mailbox the thread belongs to**, not just the org | Privilege leak across mailboxes within one org |
| 1.5 | Reserved local parts | Reserve `postmaster@`, `abuse@`, `admin@`, `hostmaster@`, `webmaster@`, `security@` on the platform domain | An organisation can register an address RFC 2142 and receivers expect to reach us |
| 1.6 | App-password login throttling | Apply the configured failed-attempt lockout to IMAP/submission app-password auth | Protocol-port brute force currently unmitigated |
| 1.7 | Staff-impersonates-staff | Forbid staff impersonating another staff member (audit log alone is not a control) | Privilege boundary between staff unenforced |
| 1.8 | Message list pagination | Real cursor/offset pagination on the list endpoint; stop loading the whole folder and keeping the last 50 | Large folders degrade the API and every client |

**Two additional hardening items not in the audit** (add now — cheap):

- **1.9 Idempotency keys** on send, alias creation, rule creation, domain mutation. The webmail will double-submit.
- **1.10 Consistent error envelope** — already present; extend it so the DNS doctor and deliverability surfaces return *what is wrong, why, and how to fix it*, not just a code.

---

# 2. The ten missing endpoints — P0

From audit §4.2. Store-layer capabilities with no route. Three are hard prerequisites for the webmail (★).

| # | Capability | Route needed | Unblocks |
|---|---|---|---|
| 2.1 | Decline organisation invite + filter review queue | `POST /org/invites/{id}/decline` | Admin console |
| 2.2 | End impersonation session | `POST /admin/impersonation/{id}/end` (store method `EndImpersonation` exists, unreachable) | Admin console; security control |
| 2.3 | Resume mailbox after bounce-breaker pause | `POST /admin/mailboxes/{id}/resume` | Admin console; breaker operability |
| 2.4 | List suppression list | `GET /admin/suppression` | Admin console |
| 2.5 | Change own password | `POST /me/password` | Webmail settings ★ |
| 2.6 | List users | `GET /org/users` | Admin console |
| 2.7 | Suspend user | `POST /org/users/{id}/suspend` | Admin console |
| 2.8 | Revoke all sessions of a user | `POST /org/users/{id}/revoke-sessions` | Webmail + admin ★ |
| 2.9 | Manage shared-mailbox members | `GET/POST/DELETE /org/shared-mailboxes/{id}/members` | Shared mailbox feature (P2) — model now |
| 2.10 | **Download attachment** (blobs stored and referenced; no route serves them) | `GET /messages/{id}/attachments/{att_id}` | **Webmail cannot be complete without this** ★ |
| 2.11 | List / release quarantine | `GET /org/quarantine`, `POST /org/quarantine/{id}/release` | Spam feature; admin console |

**Also required in this workstream (not in the ten):**

- **2.12** Surface `healthz`/`readyz` to alerting — endpoints exist, nothing watches them.
- **2.13** Delivery-queue inspection per org and stats endpoint — already staff-only; the admin console consumes them. Verify shapes now.

---

# 3. Operations, packaging and verification — P0

From audit §4.4. No pilot on real hardware without these.

| # | Item | Detail |
|---|---|---|
| 3.1 | **Janitor** | Scheduled jobs: expire sessions, prune login attempts and rate events, garbage-collect blobs with no remaining reference. Queries exist; nothing schedules them. Without this, DB and blob store grow monotonically. |
| 3.2 | **CI workflow** | Format, build, vet, test with cache disabled, **in fresh processes** — the E2E suite creates its DB once per process and cannot repeat with `-count=N`, which is how an intermittent bounce-breaker failure stayed hidden. |
| 3.3 | **Dockerfile** | Reproducible image for `cmd/maild` and `cmd/tracker`. |
| 3.4 | **Makefile** | Replace manual workspace-toolchain steps. |
| 3.5 | **Backup runbook + tested restore** | Unacceptable to ship a mail system holding customer data without it. Schedule restores, not just dumps. Include per-mailbox restore — users will delete a folder and expect it back. |
| 3.6 | **Alerting** | Wire `healthz`/`readyz` to on-call. Add queue-depth, delivery-failure-rate, breaker-trip and disk/blob-growth alerts. |
| 3.7 | **Bare-LF normalisation** | Verify on the wire that submission messages with bare line feeds are normalised to CRLF before relay. Currently unverified, not missing. |
| 3.8 | **Fresh-process E2E harness** | `scripts/e2e_repeat.sh` exists; make it a CI requirement. |
| 3.9 | **SBOM + dependency scanning** | From P0, not later. |
| 3.10 | **Logging discipline** | No message content in logs. Structured, keyed by message ID; content lives only in the blob store. |

**Accepted for pilot, required before beta:**

- Spam verdict engines (ClamAV/DNSBL) are hook points only, disabled by design.
- No integration test against a real remote MX.

---

# 4. DNS

## 4a. Immediate fixes — P0 (owner, 1–2 days)

From audit §6, observed 25 November 2026. Re-verify before acting — the observations were not repeated.

| # | Record | Problem | Fix |
|---|---|---|---|
| 4a.1 | `synkra.email` | Does not resolve at all (no MX, A, TXT) | Publish zone |
| 4a.2 | `mx1.synkra.email` | Does not resolve | Publish A record |
| 4a.3 | PTR for `167.86.106.152` | Still `vmi3381672.contaboserver.net` — FCrDNS fails | Set rDNS to `mx1.synkra.co.za` with the provider |
| 4a.4 | `synkra.co.za` MX | Five MX records at **equal preference** | Reduce to one primary + backups with distinct preferences |
| 4a.5 | `synkra.co.za` SPF | Points at an external mail suite | Replace with our own `include:` |
| 4a.6 | `_dmarc.synkra.co.za` | **Two conflicting DMARC records** (`p=none` and `p=reject`) — receivers ignore DMARC entirely | Remove one; single record only |
| 4a.7 | `spf.synkra.email` | Missing, but customer SPF records were told to `include:` it | Publish the SPF include target |

**Records to publish before any external send** (audit Table 4):

| Type | Name | Value | Purpose |
|---|---|---|---|
| A | `mx1.synkra.co.za` | `167.86.106.152` | Sending host (resolves) |
| PTR | `167.86.106.152` | `mx1.synkra.co.za` | FCrDNS match |
| MX | customer domains | `10 mx1.synkra.co.za` | Receiving |
| TXT | customer domains | `v=spf1 include:spf.<platform-domain> ~all` | SPF |
| TXT | `sk1._domainkey` (per domain) | DKIM public key from the domain row | DKIM signing |
| TXT | `_dmarc` | `v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@<platform-domain>` | DMARC + reports |
| TXT | `<platform-domain>._domainkey` | `v=DMARC1` | RFC 7489 report authorisation |
| TXT | `@` | ownership token | Domain verification |

**Pre-purchase checklist still outstanding** (project decision log): outbound port 25 confirmed **in writing** with the provider; self-service rDNS; IP reputation pre-check; **S3 data-residency sign-off** (owner decision, not self-approvable).

**IP warm-up** starts the moment PTR and records are correct. `SYNKRA_MAIL_WARMUP_DAILY_CAP` is already enforced in code; the ramp schedule is an operations task and the elapsed window cannot be compressed by code.

## 4b. Authoritative DNS service — the new tier-0 dependency

Because we run our own authoritative DNS, the Oner record-level limitation disappears: we provision the zone ourselves and hand Oner our nameservers at registration. That is the simplest possible registration-to-mail-ready path. The cost is that DNS becomes a service we must operate to mail-SLA standards.

### Architecture requirements

| # | Requirement | Why |
|---|---|---|
| 4b.1 | **Anycast, or multi-region unicast with health-checked failover** | DNS is the lookup dependency for *every* inbound and outbound mail flow. Single-region is a single point of failure for all customers. |
| 4b.2 | **Minimum two, preferably four, distinct nameserver hostnames on distinct network paths** | Registrars and ZACR validate nameservers at registration; robustness is required. |
| 4b.3 | **Glue records and NS hostnames under our own zone** | Required for in-bailiwick delegation. |
| 4b.4 | **Zone data model + internal zone API** (`createZone`, `putRecord`, `deleteRecord`, `listRecords`, `batchApply`) | This is the API `DnsService` wraps. |
| 4b.5 | **Mail-ready zone templates** | One call writes MX, SPF, DKIM, DMARC, MTA-STS, TLS-RPT and the ownership token. |
| 4b.6 | **DNSSEC signing (KSK/ZSK) with documented rollover**, plus DS publication at the registry | Decide now: on or off. Partial/broken DNSSEC is worse than none. ZACR supports DS submission. |
| 4b.7 | **Zone transfer**: AXFR/IXFR to secondaries, or multi-primary | Needed for redundancy (4b.1) and migrations. |
| 4b.8 | **Response Rate Limiting, response-size caps, DDoS posture** | An authoritative resolver is an amplification target. |
| 4b.9 | **TTL policy** — fixed, documented, low enough (300–600s) that corrections take effect quickly during onboarding | Directly affects how fast a customer becomes mail-ready. |
| 4b.10 | **Zone-change audit log + step-up authentication** | A zone change redirects mail. Treat `putRecord` on MX/NS/TXT like a password change. |
| 4b.11 | **Monitoring**: query latency, SERVFAIL rate, per-zone resolution checks, external propagation probes | We are now the thing that must be up for mail to work. |
| 4b.12 | **Reverse DNS zones** — `in-addr.arpa` delegation from the IP provider, or own PI space | The PTR fix (4a.3) is a one-off; the durable answer is delegated reverse zones. |
| 4b.13 | **Wildcard and catch-all support** | Required for catch-all mailbox routing and customer subdomains. |

### Registration workflow (the zero-cache path)

1. `DnsService.createZone(domain)` → creates the zone and writes the mail-ready record set **before** the domain exists at the registry.
2. `DomainService.register(domain, nameservers=[ns1..ns4])` → pass our own nameservers **in the initial `POST /domains` payload**. This is the entire optimisation: a never-delegated domain has no cached NS records, so once ZACR publishes delegation our records resolve almost immediately.
3. Validation job: query MX/SPF/DKIM/DMARC against expected values; surface pass/fail in the DNS doctor.
4. Handle ZACR pre-validation rejection as a distinct recoverable error with retry — not a generic "registration failed."

**Never** register with defaults then call `update-ns`; that path is subject to registry NS TTLs (1–24h for .co.za) plus resolver caching, i.e. 24–48h where mail bounces unpredictably.

### Edge cases to build for

- **Transfer-in / nameserver-only onboarding:** domain already delegated; propagation window applies. Keep the old mail provider active during the window or inbound mail is lost. Guided migration flow.
- **Customer keeps their own nameservers:** we cannot provision records. Surface a manual DNS wizard with provider-specific instructions (konsoleH for xneelo, ClientZone for Afrihost, plus Domains.co.za and Truehost) and mark the domain **"partially managed"** in admin.
- **Oner nameserver templates** (`POST /nameserver-templates/{id}/apply`, `bulk-apply`): **remediation** path only, for customers stuck on Oner defaults.

### POPIA note

Zone data (customer domain names and record contents) is tenant personal information. It belongs in the processing register and in the cross-border assessment if any DNS node sits outside South Africa.

---

# 5. IMAP completeness — P0/P1

| # | Item | Tier | Detail |
|---|---|---|---|
| 5.1 | **APPEND quota and oversize rejection** | P0 | APPEND neither rejects oversize nor checks quota — a client can bypass the storage policy the send path enforces. |
| 5.2 | **Folder create/delete/rename semantics**, including `\HasChildren` | P1 | Real mail clients manage folders aggressively; incomplete semantics cause client-side corruption. |
| 5.3 | **Per-user connection cap enforcement** | P1 | Config is read but not enforced; this is the lever against runaway clients. |

---

# 6. Frontend — P1 (after hardening; API contract first)

From audit §5.

**Constraint.** Frontend-last is only safe if API-first. Treat the HTTP API as the product surface from day one: a versioned OpenAPI spec written before the handler, and a thin internal test client (CLI or crude HTML) exercising every flow. A backend whose only consumer is its own test suite produces API shapes the webmail cannot use.

## 6.0 Backend prerequisites the UI depends on

| UI capability | Depends on |
|---|---|
| Read message with sanitised HTML | ★ Attachment download endpoint (2.10) |
| Message lists | ★ Real pagination (1.8) |
| App passwords / sessions | ★ Revoke-all-sessions (2.8) |
| Admin: breaker resume, suppression list | 2.3, 2.4 |
| Admin: impersonation entry **and exit** | 2.2 |
| Onboarding: domain + DNS doctor | None — endpoint exists |
| Webmail settings: change password | 2.5 |
| Real-time inbox | SSE or WebSocket decision (§17.10) |

## 6a. Webmail MVP

Login and session handling; folder and message lists with real pagination and unread counts; thread/conversation view; message reading with **sanitised HTML** (security requirement — inbound mail is untrusted); compose with plain and rich text, attachments, and the per-recipient suppressed/blocked feedback the send API already returns; search; flags, move, delete; storage/quota display; app-password management; tracking disclosure per owner policy.

**Additional MVP items from the design conversation:**

- **Responsive, mobile-first** — swipe actions (archive, delete, toggle read).
- **Keyboard shortcuts** — non-negotiable for Gmail/Outlook migrants.
- **Undo send** (configurable delay) and **send later** (scheduled send).
- **Draft autosave and crash recovery.**
- **Smart attachment warning** ("please find attached" with no attachment).
- **Plain-text mode** and **HTML source view** for advanced users.
- **Multiple sender identities** and **multiple signatures**.
- **Remote-image blocking by default**, with a visible notice when something attempted to track.
- **External-link warning** and **external-sender banner.**
- **Advanced search** with operators (sender, recipient, subject, date, attachment, folder).
- **Follow-up reminders** ("remind me if no reply in 3 days") — cheap once the metadata store exists.
- **Snooze** and **pin**.

## 6b. Admin console (staff-facing)

Review queue with approve/suspend/resume; impersonation entry and exit; delivery-queue inspection per org; bounce-breaker resume; suppression list; audit viewer; stats as charts; quarantine list/release. This is the surface that makes the trial/review/breaker gates operable by humans — no pilot scales without it.

**Also required in admin:**

- **Support console with scoped, audited, read-only visibility** — separate from the admin panel; exposes tenant delivery status **without** message-content access. A security boundary; design before a support engineer accidentally reads customer mail.
- **Domain admin role distinct from org admin** — not every mailbox administrator should move nameservers.
- **Shared-mailbox administration** (§12f) — assignment, collision prevention, activity log.
- **DNS doctor with provider-specific copy buttons and live verification state.**
- **Health score and reliability monitor** (§13.1–13.2) rendered here.

## 6c. Onboarding / DNS experience

Add-a-domain flow with ownership token; DNS doctor records with copy buttons and live verification state; honest messaging about sending gates (trial length, review status, warm-up caps, per-tier limits); **domain availability + registration inside the wizard** once Oner integration lands; **ZACR email-verification status tracked explicitly** (failure leads to suspension or deletion).

**Onboarding assistant** (P2): connect domain → verify ownership → configure authentication → first mailbox → add employees → configure signatures → import existing mail → test send/receive → run health check. This is the flow that lets a customer without an IT department succeed self-serve.

## 6d. PWA before native

A well-built PWA with push, offline drafts, biometric unlock and install prompts delivers roughly 70% of native-app value at roughly 15% of the cost. Native Android/iOS is P4, triggered by paying customers asking. Required behaviours when built: push notifications, biometric auth, offline read and draft composition, attachment previews, contact/calendar sync, secure account switching, notification controls, quick replies.

---

# 7. Core platform primitives — P1

Eight primitives nearly every P2/P3 feature consumes. Build them once, deliberately. The failure mode for from-scratch mail platforms is building each feature on its own system and never being able to reconcile them.

| # | Primitive | Powers |
|---|---|---|
| 7.1 | **Address object model** — one generic "address → set of mailboxes + policy" entity | Mailboxes, aliases, groups, distribution lists, catch-all, departments, shared mailboxes, continuity routing, role addresses (`info@`, `sales@`, `accounts@`, `support@`, `careers@`) |
| 7.2 | **Message metadata store** — parsed headers, auth results, classification, `List-*`, `Auto-Submitted`, attachment manifest | Deliverability dashboard, Mail Trace, newsletter detection, unsubscribe, quarantine, search, webhooks, RBL/abuse triage |
| 7.3 | **Delivery event log** — per-message state machine: submitted → authenticated → queued → remote contacted → response → delivered / bounced / deferred | Mail Trace, deliverability dashboard, reliability monitor, health score, reputation protection, webhooks, audit. **Lock this schema first — most consequential schema decision in the product.** |
| 7.4 | **Rules engine** — one evaluator, typed rules (`when/if/then`) | OOO, forwarding, urgent routing, alias rules, newsletter classification, workflow automation, continuity routing. Do **not** build four parallel evaluators. |
| 7.5 | **Policy engine** — per-domain / per-org | Signatures, sending limits, retention, legal hold, admin roles, alias reply-from, trial gates |
| 7.6 | **Directory** — employees, departments, roles, org structure | Signature `{{fields}}`, shared mailbox assignment, calendar, contacts, admin, SSO |
| 7.7 | **Event bus** — every state change emits an event | Webhooks, notifications, audit log, reliability monitor, IMAP `IDLE` wake (already via `LISTEN/NOTIFY` — extend, don't duplicate) |
| 7.8 | **Asset host** — public cached URLs backed by the content-addressed blob store | Signature images, avatars, branding, attachment previews. Never inline base64 images in signatures. |

## API-contract prerequisites before webmail starts

So "backend stable" is testable, not aspirational:

1. Versioned OpenAPI spec covering auth, mailboxes, messages, folders, search, contacts, admin, rules, signatures, aliases, domains.
2. **Real-time delivery semantics chosen**: IMAP `IDLE` for external clients; SSE or WebSocket for webmail. Pick one; it changes the gateway.
3. Search as an API, not SQL `LIKE` — decide Postgres FTS vs. dedicated index before volume forces it.
4. Pagination, filtering and **partial-message fetch** (list without bodies).
5. Idempotency keys.
6. Consistent, human-readable error model.
7. Internal test client used daily by the team.

---

# 8. Security architecture — P1

Cheap now, expensive later. These are decisions, not features.

## Identity and sessions

- Argon2id with tuned parameters (in place).
- Opaque, random session tokens stored hashed and revocable for webmail — **not** JWTs for sessions. JWT only for short-lived service-to-service.
- **Separate auth service** from mail services; mail trusts a signed identity assertion.
- WebAuthn/passkeys for admin accounts from the start; TOTP for all users.
- Recovery flows designed with the same rigour as login: rate limits, delays, notification.

## Key and credential custody

- **DKIM private keys** encrypted at rest under a KMS key, never logged, per-domain, with documented rotation. Most sensitive secret after user passwords.
- TOTP secrets, app passwords, API keys: encrypted at rest, scoped, revocable, last-used tracked.
- Secrets manager from P0. No secrets in env files or code.

## Tenancy and least privilege

- `tenant_id` on every table; every query scoped; Postgres RLS where possible.
- Separate DB roles per service — the SMTP ingest role cannot read billing.
- Service-to-service auth via mTLS or signed tokens; never "internal network is trusted."
- Per-tenant rate limits and reputation isolation even on a shared IP pool.

## Untrusted input

- HTML email is hostile. Sanitise with a strict allowlist **at render time**, not ingest (we may need to re-render later).
- Remote images proxied through the asset host; tracking pixels stripped/blocked by default.
- Attachments parsed in a sandboxed worker (separate process, no network, resource limits) — never in the web tier.
- **Every URL the system fetches on a user's behalf is an SSRF vector**: one-click unsubscribe POSTs, image proxying, link previews, migration sources, DMARC report fetches. Guards: resolve, block private ranges, block redirects into private ranges, timeout, never return the body to the user.
- DNS lookups during inbound mail (SPF/DKIM/DMARC/MTA-STS) need a hardened resolver with its own egress policy.

## Abuse and compromise

- **Detect new auto-forward or filter rules after login** — a top real-world attack. Alert, delay, require re-auth before they take effect.
- Impossible-travel and new-device alerts on by default.
- Outbound throttling per mailbox with automatic circuit breaker (exists) plus a manual admin "restrict sending" action.
- **Internal abuse console** before the first incident: investigate, throttle, suspend, restore.
- **Reputation protection flow**: detect suspicious pattern → determine likely compromise → temporarily restrict → notify admin → remediation steps → restore.

## Operational

- Append-only audit log (consider hash-chaining) of every admin and security-relevant action, including domain mutations.
- No content in logs.
- Encrypted backups with tested restore.
- Dependency scanning and SBOM from P0.
- Third-party penetration test **before paying customers** and again before any enterprise deal.
- Security review of the auth surface plus a delivery-queue load test before M2 (audit §8, step 5).

## Deliverability security

- **ARC or SRS** the moment we forward mail — aliases and forwarding rules break SPF alignment for the recipient. Mandatory, not optional.
- **MTA-STS and TLS-RPT** implementation; DANE noted as future work.
- **FBL registration** with major ISPs, and a pipeline to ingest and act on complaints.
- **DMARC aggregate (RUA) ingestion** — parsing XML reports is a real subsystem; "DMARC monitoring" is impossible without it.
- Subdomain isolation for outbound (e.g. `send.customer.com`) so bulk mail cannot damage the primary domain's reputation.

---

# 9. Privacy and POPIA — P1

POPIA is enforced and its amended regulations updated registration requirements in April 2025. This is an engineering requirement.

## Posture

- **State exactly what we scan and why.** "We process content only for spam, malware and deliverability, never for advertising or profiling" is defensible. "We never read your email" is not, if we run a filter.
- Metadata minimisation with a written retention schedule (e.g. delivery logs 90 days hot / 1 year cold). Decide now.
- **Per-tenant envelope encryption** via KMS — cryptographic isolation and verifiable destruction.
- **Be honest about encryption.** TLS in transit + encryption at rest + DKIM signing ≠ end-to-end encryption. If E2EE (PGP/S/MIME) is ever added, state that it is opt-in and that metadata/subject lines may be uncovered.
- **Remote content blocked by default**, with a visible notice when tracking was attempted.

## POPIA engineering requirements

| # | Requirement | Detail |
|---|---|---|
| 9.1 | **Section 21 operator agreements** | Machine-readable, generated per tenant, **versioned**, stored against the tenant record. Must cover domain registration, not just mail hosting. When sub-processors change we must know which tenants accepted which version. |
| 9.2 | **Section 22 breach scoping** | Within minutes, answer: which tenants, which mailboxes, which messages, what personal information? Requires the metadata store and audit log queryable by tenant and time range. A breach we cannot scope is one we cannot report. |
| 9.3 | **Section 19 security measures** | Demonstrable evidence: encryption at rest with per-tenant keys, 2FA, admin access logs. |
| 9.4 | **Section 72 cross-border transfers** | A documented assessment for **every** offshore component — backups, S3 replicas, search cluster, DNS nodes, third-party APIs, DMARC report processing. |
| 9.5 | **POPIA readiness API** | Endpoints exporting processing register, lawful basis, sub-processor list, retention schedule, breach history. Becomes the Privacy Centre UI later. |
| 9.6 | **DSAR and deletion as first-class operations** | APIs, not tickets. Must cover registrant records. Handle the tension with registry retention: we cannot delete a registrant record ZACR requires for the life of the registration — document that lawful basis explicitly. |
| 9.7 | **Breach notification runbook** | With Regulator and data-subject templates pre-written. |
| 9.8 | **Registrant data** | `.za` requires four contacts (registrant, admin, technical, billing), registrant email, nameservers; data flows to ZACR and appears in WHOIS. Processing register entries needed. **WHOIS privacy**: decide and document. **Retention after deletion or transfer-out**: define it. |
| 9.9 | **Registrant correctness** | The tenant is the registrant by default. Make delegation explicit, logged and revocable — never let a contractor accidentally become the domain owner. |

---

# 10. Domain reselling via Oner — P1

**Confirmed from the docs:** 62 endpoints, **no record-level DNS routes** on any TLD. Delegation only.

| Endpoint | Purpose |
|---|---|
| `POST /domains/update-ns` | Push nameservers to the registry |
| `PUT /domains/{id}/nameservers` | Local ns1–ns4 update (nulls clear ns3/ns4) |
| `GET /nameservers/check-availability` | Availability check |
| `GET/POST/PUT/DELETE /nameserver-templates` | Template CRUD |
| `POST /nameserver-templates/{id}/apply` | Apply to a domain |
| `POST /nameserver-templates/bulk-apply` | Apply in bulk |
| `POST /domains`, `GET /domains/name/{domain}`, `PUT /domains/{domain}/renew`, `DELETE /domains/{domain}`, `POST /domains/transfer-with-updates`, `PUT /domains/update-contacts`, `GET /domains/domain-contact`, `GET /domains/check/{domain}`, `POST /auth/login` | Registration, renewal, transfer, contacts, availability |

## Known API gaps to design around

- **No domain-list endpoint** → maintain our own `domain → oner_domain_id` mapping as source of truth, with a scheduled reconciliation job.
- **No webhooks** → poll for expiry, transfer status and renewal results. Budget a reconciliation job.
- **No DNS records** → irrelevant now, because we run our own DNS.

## Architecture

- Wrap everything behind an internal **`DomainService`** (`check`, `register`, `renew`, `transferIn`, `transferOut`, `updateContacts`, `updateNameservers`, `getAuthCode`, `getStatus`). Oner is the first adapter; `.za` has multiple accredited registrars, so a second adapter must be possible without touching billing or tenant code.
- **Do not build on the WHMCS module.** It would become an unremovable dependency.
- Keep `DomainService` and `DnsService` independent: a change of DNS provider must not touch registration logic, and vice versa. Orchestration calls `DnsService.createZone()` and `createMailRecords()` **before** `DomainService.register()` with the resulting nameservers. Make this ordering a hard rule in code, not a convention.

## Security — domain takeover is now our highest-impact attack

A compromised domain account redirects MX, takes over every mailbox, and receives password resets for every service the customer uses.

- **Step-up authentication** (password + WebAuthn/TOTP) on every domain-mutating action: nameserver changes, registrant/contact changes, auth-code retrieval, transfer-out, deletion.
- **Notify registrant and all org admins immediately on nameserver change**, with a short cooling-off window on domains with active mail. The single control that most directly prevents mail interception.
- **Transfer lock by default**; unlock requires step-up and an audit entry. Transfer-out must be self-service (EPP code without a support ticket) but logged and alerted.
- **Every domain event audited**: who, what, from which IP, with which auth method.
- **`domain_admin` permission distinct from `org_admin`.**

## Reliability — domain expiry is a mail outage, not a billing event

If a domain lapses, MX disappears and **all inbound mail bounces**.

- **Auto-renew on by default** for domains attached to an active mail tenant, with a stored payment method.
- **Escalating reminders** at 60/30/14/7/1 days, to billing contact **and** org admin, via a channel that does not depend on the expiring domain (SMS or alternate email — never only `admin@`).
- **Health-score integration**: expiry within 30 days visibly degrades the score.
- **Grace/redemption rules** surfaced in admin ("recoverable until X").
- **Renewal failure wired into the reliability monitor as an incident.**

## Commercial

- Single invoice, single renewal date, but **separate line items** with the 15% VAT breakdown per line.
- Oner's reseller terms are non-exclusive and allow fee changes with notice → billing must absorb cost changes without breaking subscriptions; do not model margin on price stability.
- Defer domain registration as a revenue line (thin margin, R45–R105/year retail) but keep the onboarding flow **domain-aware**.

---

# 11. Billing, payments and VAT — P1

South African payment rails are fragmented; a single international gateway will lose customers.

| Provider | Capability | Role |
|---|---|---|
| **PayFast** | Subscriptions, refunds, webhooks | Primary subscription rail |
| **Yoco** | One-time, webhooks, no native subscriptions | Card fallback |
| **Ozow** | One-time, webhooks, no native subscriptions | Instant EFT |
| **PayShap** | Instant bank transfer (launched March 2023) | Emerging rail |
| **Peach Payments / Stitch** | Card processing, established | Alternative |

## Requirements

| # | Item |
|---|---|
| 11.1 | **Payment abstraction service** from P0 — subscription lifecycle, dunning, proration, VAT invoicing must not couple to one gateway's SDK |
| 11.2 | **Recurring billing scheduler** — if the gateway lacks native subscriptions, we own retries, grace periods and failed-payment notifications. Idempotent charge attempts. |
| 11.3 | **VAT at 15%**, clearly shown, **per line item**, stored in minor units with explicit breakdown. The Competition Commission has scrutinised VAT-inclusive vs -exclusive pricing. |
| 11.4 | **ZAR pricing**, downloadable invoices, payment history, automated payment reminders |
| 11.5 | **B-BBEE commercial pack** — company registration, tax clearance, affidavit or SANAS-accredited certificate. A procurement prerequisite (10–20 points in tenders), not a marketing asset. Needed before the first enterprise conversation. |

---

# 12. Feature register — P2

The four feature groups, plus deliverability surfaces. These are formal requirements, not options.

## 12a. Deliverability dashboard and Mail Trace

Every domain gets: delivery success rate, bounce rate, complaint rate, sending reputation, authentication status, outbound volume, rejected, deferred, reputation warnings, blacklist monitoring, sending-limit status.

**Message-level report** — "Delivered: recipient server accepted. SPF passed, DKIM passed, DMARC passed, TLS used, 2.4s." Careful wording: acceptance ≠ inbox.

**Mail Trace** — customer enters a recipient or selects a message and sees the journey: submitted → authentication checked → outbound queue → recipient contacted → response → final status. The highest-value support-deflection feature we can build.

**Deliverability protection** (mostly present in the engine; needs surfacing and admin controls): rate limiting, bounce detection, complaint monitoring, suspicious-pattern detection, compromise detection, automatic temporary suspension, IP/domain reputation monitoring, separate transactional vs mailbox sending policies, outbound queue management, retry policies.

**Guided flows** (P2): DMARC rollout wizard (`p=none` → ingest RUA → `p=quarantine` → `p=reject`), BIMI setup (requires DMARC enforcement + VMC; 10–20% open-rate lift), MTA-STS/TLS-RPT configuration.

## 12b. Disposable & masked aliases

Lightweight routing identities, **not** mailboxes; no separate subscription.

- Generate instantly; custom or random; assign label; created date; message count; last used.
- Disable, delete, reactivate; auto-expiry; single-use (deactivates after first successful message).
- Block specific senders per alias; alias-specific rules; alias-specific spam filtering; alias-specific forwarding destination; activity notifications.
- **One-click "Disable Alias"** — incoming mail stops immediately; user sees which alias caused it.
- Bulk creation; categories (Shopping, Newsletters, Suppliers, Personal).
- **Reply-from an alias** — only on domains whose DKIM/SPF we manage; envelope sender and DKIM must align.
- **Atomic delivery counter** for single-use and expiry, incremented in the same transaction that accepts the message, or concurrent deliveries both slip through.
- Custom-domain aliases route without per-alias DNS records.
- **Implementation rule:** alias-specific rules and filtering are *scopes* in the rules engine — not a parallel filtering path.

## 12c. Email Signatures Manager

**Personal editor:** rich text, HTML source, preview, templates, logos/images, social and website links, contact info, multiple signatures, default selection, different for new vs reply, switching while composing, auto-insertion.

**Raw HTML importer:** accept pasted HTML → parse/validate → identify elements → sanitise (strict allowlist) → convert to email-compatible structure → preview → allow editing of the rendered result → save as reusable. The user must never have to understand HTML.

**Compatibility engine — pragmatic version first.** Ship a **linter**, not a simulator:

- Flags: external stylesheets, `<style>` blocks, flexbox/grid, CSS variables, background-image URLs, unhosted images, missing alt text, fixed widths >600px, missing font fallbacks.
- Generation requirements: email-safe HTML, table-based layouts where needed, inline CSS, no JS, no external CSS, font fallbacks, hosted images, clickable `tel:`/`mailto:`/links, responsive where practical, preserved spacing/alignment/image dimensions, image size caps, graceful dark mode.
- Preview in an iframe with email-safe defaults; word the UI **"compatibility warnings," not "pixel-perfect preview."** No system can guarantee identical rendering across every Outlook version — say so.
- Headless-browser screenshot rendering of Gmail/Outlook Desktop/Outlook Web/Apple Mail/mobile is a **later premium feature**, not MVP.

**Company signatures:** templates with `{{employee_name}}`, `{{job_title}}`, `{{department}}`, `{{phone}}`, `{{email}}`, `{{company_name}}`, `{{website}}`, `{{logo}}`, `{{address}}`, `{{linkedin}}` resolved against the directory. Policies: assign to department/employee, require standard signature, allow customisation of approved sections, **lock branding elements** (editor refuses edits inside locked nodes), auto-update on employee detail change, remove on offboarding, per-domain signatures.

**External-client enforcement** (the hard part):

- Inject **only on outbound submission**, never on inbound (rewriting inbound breaks DKIM and is a support nightmare).
- Detect existing signature (heuristic + optional marker) and honour the policy: *append policy signature* / *only if none detected* / *replace client signature*. Default: append-if-absent.
- Per-mailbox policy with a client allow-list; log every injection for audit.
- Make the conflict explicit in the UI — a server-injected signature and an Outlook signature will both appear otherwise.

**Assets:** serve from the asset host with long cache headers; never inline base64.

## 12d. Unsubscribe & Newsletter Manager

- Parse `List-Unsubscribe` and `List-Unsubscribe-Post` **at ingest**, store on the metadata record.
- RFC 8058 one-click: `POST` with body `List-Unsubscribe=One-Click` to the HTTPS URL; `mailto:` fallback. **Server-side, short timeout, never from the user's browser.**
- Distinguish: one-click supported / website / body link / none recognised.
- **Safety hard rule:** auto-POST only to the sender's own endpoint (host matches sender or known ESP), per-sender rate limit, explicit user click, log result.
- Newsletter detection: scored heuristic (`List-*`, `Precedence: bulk`, recurring sender, unsubscribe link present, low personal-recipient signal). Store the score; user overrides become training signal.
- **Newsletters view**: grouped by sender, unread counts, frequency, sort, search, bulk archive, unsubscribe inline, block sender.
- **Subscription dashboard**: newsletter / frequency / status, with unsubscribe, mute, manage.
- **Digest** (P3): ship the *view* first. Re-injected digest email creates a classification loop (the digest must be excluded from newsletter detection) and storage duplication — gate behind per-user opt-in and ship last. Daily / weekly / none / per-newsletter.

## 12e. Out-of-Office & Routing Rules

**OOO:** start/end datetime, timezone, internal-only or external-only, separate internal/external messages, auto activation/deactivation, multiple scheduled periods.

**Reply frequency:** once per sender per absence, once every N days, known contacts only, external only, exclude lists and automated mail, **prevent reply loops**.

**Loop prevention is non-negotiable and must be a single shared guard function** used by OOO and by every auto-reply rule action: check `Auto-Submitted` (RFC 3834), `Precedence: bulk/list/junk`, and null return-path before sending.

**Smart forwarding:** all mail, selected contacts, urgency keywords, specific departments, selected addresses, arbitrary conditions.

**Urgent routing:** keywords, senders, recipients, subject conditions, attachment conditions, destination, optional sender acknowledgement.

**Rules builder:** *when* a mail arrives, *if* conditions match, *then* act — forward, redirect, label, move, mark important, notify colleague, create task, auto-reply, add to shared mailbox.

**Sieve vs. own engine:** Sieve is the right substrate if we expose ManageSieve — `vacation` natively covers once-per-sender-per-N-days and `:addresses` covers internal/external. If not, implement the same semantics in the rules engine with a KV store for `(mailbox, sender) → last_replied_at`. **This choice locks the rule schema — decide before P2.**

**"Create a task" is a dead button until a task system exists** — stub it or cut it.

**Business continuity routing:** admin-configurable temporary routing when an employee is on leave, resigns, is suspended, unavailable, or has their mailbox disabled; e.g. `salesperson@` → sales shared mailbox. Reuses the alias table with admin scope and audit.

## 12f. Shared mailboxes with assignment

The strongest single differentiator: Google Workspace still charges per seat for this.

Multiple users with access; email assignment to employees; internal notes (never exposed on forward/reply); status indicators; shared drafts; reply ownership; **collision prevention** (show that another employee is already responding); email history; team activity logs.

**Model decision required now:** shared mailbox as a mailbox with ACLs, or a distinct address object with assignment metadata? Affects IMAP folder semantics and the webmail UI; painful to change later. Related missing endpoint: 2.9.

---

# 13. Differentiators — P3

| # | Feature | Notes |
|---|---|---|
| 13.1 | **Business Email Health Score** | Live score across domain authentication, deliverability, security, storage, DNS, reputation. Must explain how to improve — not decorative. Degrades on approaching domain expiry. |
| 13.2 | **Email Reliability Monitor** | Alerts when MX stops resolving, DKIM fails, queue grows abnormally, mailbox hits 90% storage, reputation drops, a business-critical message is rejected. Premium tier. |
| 13.3 | **Business Continuity Mode** | Backup MX, independent recovery, emergency archive access, automatic queue recovery, DR status dashboard, documented procedures, continuity notifications. Redundancy designed in, not advertised. |
| 13.4 | **Migration Assistant** | IMAP-based migration of mail, folders, contacts, calendars, attachments, aliases, settings from Gmail/M365/other. Progress dashboard with per-message attention list. Removes the biggest switching barrier. |
| 13.5 | **Archive + legal hold** | Long-term retention, searchable, exportable, retention policies, legal hold, admin-controlled access. A separate compliance offering with legal weight — treat accordingly. |
| 13.6 | **Email Address Reputation Protection** | Detect suspicious sending → determine likely compromise → temporarily restrict → notify admin → remediation steps → restore (§8). |
| 13.7 | **Developer API + webhooks + SMTP relay** | Send, retrieve, search, manage contacts/mailboxes/aliases, delivery reports, metadata, forwarding rules, domains. Events: received, delivered, bounced, mailbox created, domain verified, quarantined, suspended. SMTP credentials for transactional sending (invoices, password resets, order confirmations). |
| 13.8 | **Per-mailbox backup and restore** | Distinct from archive. 30-day user-initiated restore is a support-cost pre-emption. |
| 13.9 | **Privacy Centre UI** | Renders the POPIA readiness API (9.5). |

**Integration note:** "email-to-accounting" is honestly just a routing rule forwarding invoices to a designated address. Ship that and stop. Do not build accounting integrations.

---

# 14. Suite and expansion — P4

| # | Item | Decision required |
|---|---|---|
| 14.1 | **Calendar** | Build on **CalDAV** so Apple Mail, Thunderbird and mobile OS calendars work. If we commit, commit fully; don't half-build. |
| 14.2 | **Contacts** | Build on **CardDAV**. Read-only contact list can ship earlier as a stopgap. |
| 14.3 | **Tasks / notes** | Only after a task system exists. Notes-on-email (private, never forwarded) is cheap and valuable; ship with shared mailboxes. |
| 14.4 | **White-label webmail** (`mail.company.co.za`) | Threads theming through login, webmail, admin, emails, error pages. Phase 4 at the earliest. |
| 14.5 | **SSO** (OIDC/SAML) | Build vs. integrate (Keycloak/WorkOS). Affects directory and auth boundaries. Larger customers only. |
| 14.6 | **Native Android/iOS** | After the PWA proves demand. |
| 14.7 | **Domain registration as a revenue line** | Defer — regulated reseller business, thin margin. |

---

# 15. South African specifics — cross-cutting

## Infrastructure

- **Host in South Africa.** Teraco (Isando), xneelo (Centurion/Cape Town) and peers. ~70% of Africa's data centres are here. Local hosting is both POPIA-relevant and a latency win for webmail/IMAP (200ms per search is noticeable).
- **Choose a facility with redundant power and carrier-neutral connectivity** — not one relying on a local substation. Load shedding has eased (12+ months without it, first time since 2018) but is not solved, and grid pressure grows with data-centre demand. **Multi-AZ within South Africa before cross-border DR.** Advertise it explicitly — every SA business buyer asks.
- **Section 72 assessment for anything offshore**, including DNS nodes.

## Market positioning

- Budget locals: Truehost Workplace from R8/mo (annual, 10GB), Afrihost ~R59, Domains.co.za ~R69, xneelo ~R99. Global suites: Google Workspace ~R130+, M365 ~R105+, often USD via resellers.
- **Do not compete on cheapest.** Position mid-market (roughly R60–R150/mailbox by storage and features), ZAR, VAT shown. Sell to businesses that have outgrown R8 email but don't want M365 complexity.
- **Domain + mail bundling is the onboarding wedge**, not the revenue line: whoever controls DNS controls mail authentication.

## Support

- Local competitors advertise 24/7 chat and WhatsApp; SA business-hours support is an expectation. "Email is down" is an emergency at any hour.
- Build the **support console** (§6b) so support can see delivery status without message content.

## Onboarding

- **DNS doctor with provider-specific instructions** for konsoleH (xneelo), ClientZone (Afrihost), Domains.co.za and Truehost. "Log into konsoleH, click DNS, paste this" beats "add a TXT record."
- **SA business email templates** (customisable, not rigid): quotations, purchase orders, invoices, statements, supplier correspondence, customer enquiries, employment applications, proposals.
- **Onboarding assistant** (§6c).

## Commercial

- ZAR, 15% VAT per line item, invoice downloads, payment history, automated reminders.
- **B-BBEE pack** ready before enterprise conversations.

---

# 16. Milestones and sequencing

From audit §7 and Table 5, extended with the DNS decision and feature register.

| Milestone | Target | Scope | Entry criteria |
|---|---|---|---|
| **M0 — DNS fix** | Days 1–2 | §4a records, PTR, duplicate DMARC removal, `spf.` include published | — |
| **M1 — Internal pilot** | ~4–6 weeks | Real VPS, own domains, hand-approved orgs, existing mail clients via IMAP/SMTP | §1 hardening closed; §2 ten endpoints closed; §3 janitor + CI + backups + alerting live; §4a DNS/PTR/DKIM correct; warm-up started; §5 IMAP APPEND fixed |
| **M2 — Closed beta** | ~8–12 weeks | Invited customers, custom domains, interim webmail or early custom client | Admin console usable end-to-end; §5 IMAP gaps closed; §8 security review findings fixed; load test passed; §9.1 and 9.2 in place; Oner integration live or manual DNS wizard |
| **M3 — Public launch** | ~4–5 months | Self-serve signup, full custom webmail + onboarding | Custom frontend complete including sanitised HTML reading; §7 primitives stable; privacy text finalised; warm-up complete; breaker and limits observed stable; §9.5 POPIA readiness API live |

## Estimated effort

Planning estimates from the audit — single experienced Go developer, not measurements:

| Workstream | Person-weeks |
|---|---|
| Backend hardening (API/auth) | 2–3 |
| Missing API endpoints | 2–3 |
| IMAP completeness | 1–2 |
| Operations: janitor, CI, packaging | 2–3 |
| Security review + load test | 1–2 |
| DNS/PTR/deliverability prep | 1–2 (elapsed) |
| Frontend MVP | 6–12 (small team) |
| **Authoritative DNS build** | **additional — scope separately with its own owner** |

## Elapsed-time dependencies outside engineering

- **DNS propagation and provider turnaround.**
- **IP warm-up** — weeks, not compressible by code.

Start both at M1.

## Readiness percentages (audit §7, heuristics)

Delivery engine ~85%, API ~70%, IMAP ~80%, ops ~30%, frontend 0%, DNS/deliverability ~40%. Overall ~55–60% to public launch, ~80% to internal pilot. These shift upward once §1–§4a land.

## Process rule

Build **vertical slices** per phase (ingress → storage → API → minimal UI → dogfood), not "all backend, then all frontend." Mail platforms are integration problems; deliverability, IMAP semantics and session handling cannot be validated without using them. A crude internal client per phase is cheaper than a big-bang integration later.

---

# 17. Open decisions

| # | Decision | Blocks | Notes |
|---|---|---|---|
| 17.1 | **Frontend path**: open-source interim client (Roundcube/SnappyMail, 1–2 weeks) for the pilot while the custom build starts | Every later milestone date | Recommended: yes, interim client; admin console first in the custom build. The audit calls this the single most useful immediate decision. |
| 17.2 | **Sieve vs. own rules engine** | §12e rule schema | Locks the schema permanently |
| 17.3 | **Shared mailbox model**: mailbox with ACLs vs. distinct address object | §12f, IMAP semantics, webmail UI | Painful to change later |
| 17.4 | **Delivery event log schema** | §7.3 and every dashboard | Most consequential schema in the product |
| 17.5 | **IP strategy**: shared pool, dedicated per org, or hybrid | Reputation isolation promises | Affects sales claims |
| 17.6 | **DNSSEC on or off** for customer zones | §4b.6 | Broken DNSSEC is worse than none |
| 17.7 | **Signature injection default**: opt-in per domain or on by default | Every external-client customer | §12c |
| 17.8 | **Alias reply-from**: restrict to managed-DNS domains, or support external with verification | §12b | Deliverability safety |
| 17.9 | **Search backend**: Postgres FTS vs. dedicated engine | §7 API contract | Decide before volume forces it |
| 17.10 | **Webmail real-time**: SSE or WebSocket | §7, gateway design | Pick one |
| 17.11 | **S3 data-residency sign-off** | Go-live on production storage | Owner decision, not self-approvable |
| 17.12 | **Suite or email-first**: calendar/contacts in or out, and CalDAV/CardDAV or proprietary | §14, Phase 3–4 scope | Make it explicit, don't drift |
| 17.13 | **WHOIS privacy** offering | §9.8 | Decide and document |
| 17.14 | **POPIA legal review** | Public launch | Flagged by the audit as a legal task, not assessed |

---

# 18. The three things to do this week

1. **Fix DNS** (§4a) — PTR, duplicate DMARC, publish platform records.
2. **Close hardening before endpoints** (§1 → §2) — in that order, because the endpoints expose more surface on top of it.
3. **Decide the frontend path** (17.1) — every later milestone date depends on it.

Everything else sequences behind those.

---

# 19. Consolidated register appendix

| ID | Item | Tier | Source |
|---|---|---|---|
| 1.1–1.8 | API/auth hardening (8 items) | P0 | Audit §4.1 |
| 1.9 | Idempotency keys | P1 | Conversation |
| 1.10 | Human-readable error model extension | P1 | Conversation |
| 2.1–2.11 | Ten missing endpoints (11 routes) | P0 | Audit §4.2 |
| 2.12–2.13 | Health alerting; queue/stats verification | P0 | Audit §4.4 |
| 3.1–3.10 | Ops, packaging, verification | P0 | Audit §4.4 |
| 4a.1–4a.7 | Immediate DNS record fixes | P0 | Audit §6 |
| 4b.1–4b.13 | Authoritative DNS service | P0/P1 | New decision |
| 5.1–5.3 | IMAP completeness | P0/P1 | Audit §4.3 |
| 6.0 | Frontend backend prerequisites | P0 | Audit §5 |
| 6a–6d | Webmail, admin console, onboarding, PWA | P1 | Audit §5 + conversation |
| 7.1–7.8 | Eight platform primitives | P1 | Conversation |
| 7.API | API-contract prerequisites | P1 | Conversation |
| 8.x | Security architecture | P1 | Conversation |
| 9.1–9.9 | POPIA and privacy | P1 | Conversation |
| 10.x | Domain reselling (Oner + own DNS) | P1 | Oner docs |
| 11.1–11.5 | Billing, payments, VAT, B-BBEE | P1 | Conversation |
| 12a | Deliverability dashboard + Mail Trace + guided DMARC/BIMI/MTA-STS | P2 | Conversation |
| 12b | Disposable & masked aliases | P2 | Conversation |
| 12c | Signatures: personal, HTML importer, linter, company admin, injection | P2 | Conversation |
| 12d | Unsubscribe + newsletter manager | P2/P3 | Conversation |
| 12e | OOO + smart/urgent forwarding + rules builder + continuity | P2 | Conversation |
| 12f | Shared mailboxes with assignment | P2 | Conversation |
| 13.1–13.9 | Health score, reliability monitor, continuity, migration, archive, reputation protection, API/webhooks, per-mailbox backup, privacy centre | P3 | Conversation |
| 14.1–14.7 | Calendar, contacts, tasks, white-label, SSO, native apps, domain registration | P4 | Conversation |
| 15 | South African specifics | Cross-cutting | Conversation |
| 17.1–17.14 | Open decisions | — | Both |

---

*End of document.*