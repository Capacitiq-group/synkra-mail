# Authoritative DNS build + full audit sweep — 27 November 2026 (second pass)

Two things happened in this pass: the **authoritative DNS server itself** was
built (phase 8's serving edge — the zone API existed, the edge did not), and
the whole repository was re-audited **from scratch** for vulnerabilities,
quality problems, bugs and missing endpoints. Every finding below is either
fixed in this commit or listed under "open" with a reason. Verification:
`go build ./...`, `go vet ./...`, `gofmt` clean, `go test ./... -count=1` green.

---

## 1. The authoritative DNS edge (`internal/dnssvc/serve.go`, `cmd/dnsd`)

A separate process, as the register demands ("tier-0 service with its own
SLA"). Serves the zones managed through `DnsService` over **UDP and TCP**:

- **Authoritative-only**: recursion never offered (`RA` bit clear), foreign
  zones get `REFUSED` (an authoritative server does not guess), non-query
  opcodes get `NOTIMP`.
- **SOA/NS synthesis** per zone from config (`SYNKRA_MAIL_DNS_NS_HOSTS`),
  serial from the store (bumped on every zone-API mutation), negative answers
  carry the SOA for caching.
- **Full record set**: A, AAAA, TXT (quoting/escaping handled), MX, CNAME,
  SRV, CAA, NS — plus **DNS wildcards** with closest-encloser semantics
  (§4b.13: catch-all routing, customer subdomains).
- **Response Rate Limiting** (§4b.8): per-source-IP token bucket on UDP
  (default burst 100, refill 25/s); floods are dropped silently, counted in
  the server stats.
- **AXFR to secondaries** (§4b.7): TCP-only, **denied by default**,
  `SYNKRA_MAIL_DNS_SECONDARIES` CIDR allowlist; the transfer streams
  SOA…records…SOA. DNSSEC stays **off** (§4b.6: partial DNSSEC is worse than
  none — the rollover runbook does not exist yet).
- **Zone freshness**: the provider re-reads `mail.dns_zone`/`mail.dns_record`
  on a ticker (default 15 s) — customer DNS changes go live in seconds.
- **Health endpoint** for the process supervisor (§4b.11), query/RRL/AXFR
  counters in the server struct for the metrics wiring.

Tests: `internal/dnssvc/serve_test.go` (real UDP/TCP round trips: apex A, MX,
DKIM TXT with special characters, SOA identity, wildcard synthesis, NXDOMAIN
vs NODATA, foreign-zone REFUSED, recursion bit, AXFR denied → ACL-allowed,
RRL flood drop) and `internal/e2e/dnssvc_e2e_test.go` (a zone written through
the PGDnsService API is served by the edge within the refresh cadence).

## 2. Findings — fixed in this commit

| # | Severity | Finding | Fix |
|---|---|---|---|
| S1 | **High** | Blob storage accepted arbitrary keys into filesystem/object paths (`blob.PathFor` did no validation; `Open/Stat/Delete` are public interface methods). No caller passes user input today — every key is a DB-stored computed sha — but the interface is one refactor away from a path traversal. | `PathFor` now validates **64 lowercase hex** and returns "" otherwise; fsblob and s3blob treat "" as `ErrNotFound` before any path/URL is built. |
| S2 | **High** | The API registered 7 routes **twice** — once unguarded, once behind the impersonation guard. Correctness depended on chi's duplicate-resolution order, which is not a contract. | Each guarded route (webhook create, tracking toggle, addresses, domains create/delete, API keys, app passwords, password change, shared-mailbox membership) is now registered **exactly once**, behind `requireNotImpersonating`. |
| S3 | **High** | **DELETE /domains/{id} did not exist** — a customer could register domains but never decommission one. | New endpoint behind the backdoor guard: org-ownership check, platform-domain protection, refuses while addresses still route through it (`domain_in_use` + fix hint), audited. |
| S4 | **Medium** | `PGDnsService.PutRecord/DeleteRecord/BatchApply` had **no tenancy guard** — correct today only because nothing exposes them yet. | Added `PutRecordForOrg` / `DeleteRecordForOrg` / `BatchApplyForOrg` with `ErrZoneNotOwned`; the API wiring must use these (documented in the package). |
| S5 | **Low** | Attachment `Content-Disposition` stripped quotes from stored filenames but not CR/LF. | Both stripped (a crafted stored filename can no longer touch the header layer). |
| S6 | **Low** | The tracker swallowed unsubscribe POST failures silently — the user believes they unsubscribed while the suppression was never written. | Request/build/transport/status failures are logged with the status code. |
| S7 | **Quality** | `ReserveSend` named a parameter `cap`, shadowing the builtin. | Renamed to `limit`. |
| S8 | **Quality** | The DNS zone provider could not scan NULL rows (left-join misses) or `bigint` serials — caught by its own e2e test before shipping. | Nullable scans + serial clamp; e2e proves the round trip. |

## 3. Checked and clean (fresh eyes, this pass)

- **trackingd**: the unsubscribe page is static HTML (no user data interpolated → no XSS); open-redirect guard on clicks intact; HMAC verified before any state change.
- **Idempotency**: keys are scoped per user AND bound to the request hash (`errKeyReuse` otherwise) — no cross-tenant replay.
- **Events hub**: reconnect loop with backoff present; NOTIFY-only (no credentials in the payload).
- **Attachments**: mailbox-access check + attachment-belongs-to-message binding both enforced on every download.
- **SQL**: every statement in the sweep is parameterized; no string-built queries found.
- **Config**: fail-closed rules from the morning audit still hold (strict env, secret strength, prod TLS/KMS requirements).

## 4. Open — tracked, with reasons

**Product endpoints (before public beta):** public signup; password-reset
mail-out (token table exists, the mail-out flow does not); user *removal*
(beyond suspend) with data-erasure path for POPIA; MFA recovery codes; an
org-owner-visible audit trail (currently staff-only); plus-addressing
(`user+tag@` still gets 550).

**Platform:** port 465 (implicit-TLS submission listener — `SubmissionTLSAddr`
still unserved); ACME certificate reload without restart; metrics + access
logs + request-id log correlation; multi-recipient inbound partial success;
S3 upload still inside the folder-lock transaction; migrations still read
from a directory on disk (embed needs the files under the package).

**DNS edge (operator decisions):** DNSSEC off by design until a rollover
runbook exists; AXFR is ACL-only (no TSIG yet — acceptable inside a private
secondary network); AXFR streams one message (zones larger than ~64 KB need
message chunking — not a pilot-scale risk); NOTIFY to secondaries is not sent
(secondaries re-transfer on their SOA timers); anycast/health-checked
multi-region routing is infrastructure, not code.

## 5. Register state

Phase 8 is now **served + managed**: the DnsService zone API (last commit)
plus the authoritative edge (this commit). Remaining §4b items are operator
infrastructure (anycast, secondaries, DNSSEC runbook), documented in §4 above.
