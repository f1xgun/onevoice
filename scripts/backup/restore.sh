#!/usr/bin/env bash
# Restore latest restic snapshot to scratch PG + Mongo, assert sanity queries.
# Used by both the operator monthly drill (docs/runbook-restore.md §3) and
# the weekly CI restore drill (.github/workflows/backup-restore-drill.yml).
set -euo pipefail

: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY required}"
: "${RESTIC_PASSWORD:?RESTIC_PASSWORD required}"
: "${SCRATCH_PG_DSN:?postgres://… of a throwaway DB; data WILL be destroyed}"
: "${SCRATCH_MONGO_URI:?mongodb://… of a throwaway DB}"

RESTORE=$(mktemp -d)
trap 'rm -rf "$RESTORE"' EXIT

echo "restic check (repo integrity)"
restic check

echo "restic restore latest → $RESTORE"
restic restore latest --target "$RESTORE"

PG_DUMP=$(find "$RESTORE" -name 'onevoice-pg-*.dump' | head -1)
MONGO_ARCHIVE=$(find "$RESTORE" -name 'onevoice-mongo-*.archive' | head -1)
test -s "$PG_DUMP" || { echo "missing pg dump"; exit 4; }
test -s "$MONGO_ARCHIVE" || { echo "missing mongo archive"; exit 5; }

echo "pg_restore → scratch"
pg_restore --clean --if-exists --no-owner --no-acl \
    --dbname="$SCRATCH_PG_DSN" "$PG_DUMP"

echo "mongorestore → scratch"
mongorestore --uri="$SCRATCH_MONGO_URI" --gzip --archive="$MONGO_ARCHIVE" --drop

echo "PG sanity: SELECT 1"
psql "$SCRATCH_PG_DSN" -tAc 'SELECT 1' | grep -qE '^1$'

echo "Mongo sanity: serverStatus"
# The operator backup image ships the MongoDB Database Tools, including
# mongostat, but not the separate mongosh package. One bounded mongostat row
# proves that the restored server answers an authenticated serverStatus read.
mongostat --uri="$SCRATCH_MONGO_URI" --rowcount=1 --noheaders >/dev/null

echo "restore.sh OK"
