#!/usr/bin/env bash
# OneVoice nightly backup: pg_dump + mongodump → restic → Yandex Object Storage.
# Run via BusyBox crond inside the dedicated alpine backup container.
# OPS-01 / OPS-06 (Phase 23-03).
set -euo pipefail

# crond invokes scripts in a minimal env — pick up RESTIC_PASSWORD and the
# rest of the per-run config that entrypoint.sh dropped into /etc/profile.d/.
if [ -f /etc/profile.d/restic.sh ]; then
    # shellcheck disable=SC1091
    . /etc/profile.d/restic.sh
fi

: "${PG_DSN:?PG_DSN required}"
: "${MONGO_URI:?MONGO_URI required}"
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY required}"
: "${RESTIC_PASSWORD:?RESTIC_PASSWORD required (decrypted by entrypoint.sh)}"
: "${PUSHGATEWAY_URL:?PUSHGATEWAY_URL required}"

TS=$(date -u +%Y%m%dT%H%M%SZ)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

START=$(date +%s)

on_failure() {
    local code=$?
    echo "FAIL: backup.sh exited $code" >&2
    curl --fail --silent --show-error -X POST \
        --data "backup_last_failure_timestamp $(date +%s)" \
        "${PUSHGATEWAY_URL}/metrics/job/onevoice_backup/instance/$(hostname)" \
        || true
    exit "$code"
}
trap on_failure ERR

echo "[$(date -Iseconds)] pg_dump starting"
pg_dump --format=custom --no-owner --no-acl \
    --dbname="$PG_DSN" \
    --file="$WORK/onevoice-pg-$TS.dump"
test -s "$WORK/onevoice-pg-$TS.dump" || { echo "pg_dump produced empty file"; exit 2; }

echo "[$(date -Iseconds)] mongodump starting"
mongodump --uri="$MONGO_URI" \
    --archive="$WORK/onevoice-mongo-$TS.archive" --gzip
test -s "$WORK/onevoice-mongo-$TS.archive" || { echo "mongodump produced empty file"; exit 3; }

echo "[$(date -Iseconds)] restic backup → $RESTIC_REPOSITORY"
restic backup --tag daily --tag "host=$(hostname)" --host onevoice-backup "$WORK"

echo "[$(date -Iseconds)] restic forget retention"
restic forget --tag daily \
    --keep-daily 7 --keep-weekly 4 --keep-monthly 6 \
    --prune

DURATION=$(( $(date +%s) - START ))

{
    echo "backup_last_success_timestamp $(date +%s)"
    echo "backup_duration_seconds $DURATION"
} | curl --fail --silent --show-error -X POST \
    --data-binary @- \
    "${PUSHGATEWAY_URL}/metrics/job/onevoice_backup/instance/$(hostname)"

echo "[$(date -Iseconds)] backup.sh OK (${DURATION}s)"
