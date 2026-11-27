#!/bin/bash
# backup_verify.sh — build register 3.5: run the restore drill.
# Dumps every mail table from the source database, rebuilds the schema from the
# migrations in a scratch database, restores row by row, verifies counts and a
# write probe. See docs/BACKUP_RUNBOOK.md.
#
# Usage: bash scripts/backup_verify.sh            (from the repository root)
#        SYNKRA_MAIL_RESTORECHECK_URL=... bash scripts/backup_verify.sh
set -euo pipefail

# Default: the E2E database (it has real fixtures). Point the env var at any
# instance whose restorability you want proven.
export SYNKRA_MAIL_RESTORECHECK_URL="${SYNKRA_MAIL_RESTORECHECK_URL:-postgres://synkra@127.0.0.1:5433/synkra_mail_test?sslmode=disable}"

cd "$(dirname "$0")/.."
exec go run ./cmd/restorecheck
