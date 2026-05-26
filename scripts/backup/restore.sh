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

echo "Mongo sanity: ping"
mongosh "$SCRATCH_MONGO_URI" --quiet --eval 'db.runCommand({ping:1}).ok' \
    | grep -qE '^1$'

echo "restore.sh OK"
