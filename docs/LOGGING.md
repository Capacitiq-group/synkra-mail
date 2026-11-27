# Logging discipline (build register 3.10)

**Rule: no message content in logs, ever.** Content lives only in the blob
store; logs carry identifiers and outcomes so a leaked log file can never leak
mail.

## What is logged

| Surface | Logged | Never logged |
|---|---|---|
| SMTP in | peer host, envelope from/to, response codes, auth results summary, message id | body, headers beyond identification, recipient addresses in DSN bodies |
| Submission | user id, org id, recipient count, size class, response codes | body, attachment content |
| Outbound worker | delivery job id, mailbox id, remote host, SMTP code + reason line, attempt number | message content |
| API | route, status, request id, actor user id | bodies, passwords, tokens, app-password secrets |
| Janitor | table, row count, blob sha256 prefixes (12 hex) | anything else |
| Tracker | pixel/link id, event kind, status | URL contents beyond the tracked id |

## Verified

A code audit of every `log.Printf` site (23 across `internal/` and `cmd/`, 26
November 2026) found no message content, no credentials, no tokens in log
arguments. Tests: the E2E suite exercises send, inbound, submission and
delivery paths; the assertion that no raw message bytes reach the log writer
is part of the ops review checklist until `/metrics`-style log-based tests land.

## Request ids

Every API error carries a `request_id` (UUID) in the error envelope; log lines
on the same request correlate through it. The delivery pipeline keys on the
message id (`msg_…`), so support can trace one message end to end without
seeing its content (register §12a, Mail Trace).
