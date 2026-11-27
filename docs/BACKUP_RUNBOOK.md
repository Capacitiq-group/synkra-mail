# Backup and restore runbook (build register 3.5)

> A backup you have not restored is a hope, not a backup. Every procedure here
> is exercised by `make backup-verify` / `go run ./cmd/restorecheck` against a
> scratch database; run the drill after every schema migration and before every
> release, and run a full restore rehearsal on the production instance monthly.

## What holds customer data

| Store | Content | Backup |
|---|---|---|
| Postgres (`mail` schema) | orgs, users, mailboxes, addresses, message metadata, delivery history, audit log | nightly logical dump + WAL archiving |
| Blob store (`fsblob` dir or S3) | raw RFC 822 messages, attachments, DKIM private keys are *not* here (DB) | S3 versioning + cross-region replication, or rsync to the second AZ for the dir mode |
| DKIM keys | `mail.domain.dkim_private_pem` (encrypted at rest in prod) | in the DB dump; the KMS key must be backed up with its policy |

## Nightly dump (Postgres)

```bash
pg_dump --format=custom --file=mail-$(date +%F).dump "$DATABASE_URL"
# keep 30 daily + 12 monthly; encrypt at rest; copy to the second AZ
```

Blob store (S3 mode): enable versioning and a lifecycle rule; replication to
the second region is a §9.4 cross-border decision — see the POPIA register
before enabling it outside South Africa.

## Restore (full)

1. Provision an empty Postgres; apply migrations (`maild` runs them at boot, or
   `go run ./cmd/restorecheck`'s first step does).
2. `pg_restore --no-owner --exit-on-error -d "$URL" mail-YYYY-MM-DD.dump`
3. Point the blob store at the restored snapshot (same object keys: hex sha256).
4. Start `maild` with the maintenance flag mindset: verify `/readyz`, then
   check `mail.org`, `mail.mailbox`, `mail.message` counts against the drill
   output (`make backup-verify` prints them for the last dump).
5. Announce; keep the broken instance frozen for forensics.

## Restore drill (what "tested" means)

```bash
make backup-verify   # or: go run ./cmd/restorecheck
```

The drill dumps every `mail.*` table (COPY TO), rebuilds the schema from the
migrations in a scratch database, restores row by row (COPY FROM), compares
row counts, and writes one probe row on the restored schema. Exit status 0 is
the only acceptable result. CI runs the drill against the service database.

## Per-mailbox restore (users delete a folder and expect it back)

1. Identify the mailbox id (`mail.mailbox.name`).
2. Restore into the scratch database (as above, from the newest dump taken
   before the deletion).
3. In scratch: copy the user's rows — `mail.message` rows reachable from the
   mailbox's `mail.mailbox_item` (and their `mail.blob` rows), folders with
   `mailbox_item`, `mail.attachment` rows.
4. Re-insert into production with the blob files present (content-addressed:
   the file name is the sha256, so a missing blob is visible immediately —
   `mail.blob` refcounts reference it).
5. Verify from the user's account: folder counts, one message body, one
   attachment download.

Time budget: 30 minutes for a single mailbox; a full-instance restore is the
disaster-recovery path (§13.3), target RTO 4 hours, RPO 24 hours (nightly
dump) or better with WAL archiving.

## Alerting on the backup itself

- Backup job failure → on-call (the job must be monitored, not cron-silent).
- `restorecheck` failure → on-call (a backup that fails its drill is dead).
- Dump age > 26 h → on-call.
