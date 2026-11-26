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

### Explicitly still pending

1. **S3 data-residency sign-off** (owner item 2) — cannot be self-approved.
2. `synkra.email` DNS ownership verification ( registrar/DNS control check).
3. Contabo §8.4 checklist: outbound port 25 in writing, self-service rDNS,
   IP reputation pre-check on the additional IP, backup-scope confirmation.
