# Alerting (build register 3.6 + 2.12)

`/healthz` (liveness) and `/readyz` (readiness: DB + blob store reachable) are
served by the API role. Nothing watches them by itself — wiring them to on-call
is an operations task with the rules below. `deploy/alerts.yml` ships the
Prometheus rules; wire it into the scrape config when monitoring lands.

## Probes

- **uptime**: `GET /healthz` every 15 s from outside the host; page on 2
  consecutive failures (liveness).
- **readiness**: `GET /readyz` every 30 s; page on 5 minutes of failure
  (dependencies down — DB, blob store).

## Service alerts (page = wake a human; ticket = next business day)

| Alert | Threshold | Level | Why |
|---|---|---|---|
| Queue depth | > 500 for 10 min | page | outbound not draining: remote failures or worker down |
| Delivery failure rate | > 5% of attempts over 30 min | page | provider or network trouble; feeds the breaker |
| Breaker trip | any mailbox `paused_bounces` event | ticket | a customer is degrading the shared reputation |
| Disk growth (fsblob) | > 80% of volume for 30 min | page | blob GC (janitor) is not keeping up |
| Blob GC lag | purgeable blobs older than grace > 1000 | ticket | janitor stalled |
| Backup age | last successful dump > 26 h | page | runbook §backup |
| Restore drill failure | `restorecheck` exit ≠ 0 | page | a backup that cannot be restored is dead |
| Cert expiry | submission/IMAP TLS cert < 14 days | ticket | clients refuse STARTTLS |
| Log volume | sudden 10× rise | ticket | an error loop (check before cost does) |

## Metrics to expose (next increment)

`/metrics` (Prometheus format) on the API role: queue depth by status,
delivery attempts by outcome, breaker state per mailbox, blob GC counts, janitor
last-run timestamp, IMAP/submission connection counts (per-user cap already in
config, register §5.3).
