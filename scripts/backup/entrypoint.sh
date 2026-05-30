#!/usr/bin/env bash
# Boot path: configure yc CLI from service-account JSON, decrypt RESTIC_PASSWORD
# via Yandex KMS, persist into /etc/profile.d/restic.sh so the crond-spawned
# shell inherits it (crond itself does NOT propagate parent env to job shells),
# then exec the CMD (crond -f).
# 152-ФЗ Art. 19 §2(2) KMS-managed key.
set -euo pipefail

: "${YC_SA_JSON_CREDENTIALS:?Service-account JSON required (mount as env or file)}"
: "${RESTIC_PASSWORD_KMS_KEY_ID:?KMS key id required}"
: "${RESTIC_PASSWORD_CIPHERTEXT:?KMS-encrypted restic password (base64) required}"
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY required}"
: "${PG_DSN:?PG_DSN required}"
: "${MONGO_URI:?MONGO_URI required}"
: "${PUSHGATEWAY_URL:?PUSHGATEWAY_URL required}"

mkdir -p /root/.config/yandex-cloud
printf '%s' "$YC_SA_JSON_CREDENTIALS" > /root/.config/yandex-cloud/credentials.json
chmod 600 /root/.config/yandex-cloud/credentials.json
yc config set service-account-key /root/.config/yandex-cloud/credentials.json

# `yc kms symmetric-crypto decrypt --plaintext-file /dev/stdout` writes the
# raw plaintext bytes to stdout. Capture them directly — the value written
# here MUST equal byte-for-byte the plaintext the operator passed to
# `yc kms symmetric-crypto encrypt` during the backup runbook. An earlier
# implementation piped this output through a base64 decoder, which decoded
# raw bytes as if they were base64-encoded — producing a value that passed
# the `test -n` check but did NOT match the operator-saved plaintext. That
# was a silent backup-correctness defect that only surfaced at
# disaster-recovery time. Regression-guarded by
# .github/workflows/backup-restore-drill.yml. DO NOT add `| base64 ...`
# to this pipeline.
DECRYPTED=$(yc kms symmetric-crypto decrypt \
    --id "$RESTIC_PASSWORD_KMS_KEY_ID" \
    --ciphertext-base64 "$RESTIC_PASSWORD_CIPHERTEXT" \
    --plaintext-file /dev/stdout 2>/dev/null)
test -n "$DECRYPTED" || { echo "KMS decrypt produced empty plaintext"; exit 10; }

# BusyBox crond runs jobs in a clean shell; export-style env passing via
# /etc/profile.d/ is the idiomatic Alpine pattern. The job script sources
# this file at the top of backup.sh / restore.sh.
cat > /etc/profile.d/restic.sh <<EOF
export RESTIC_PASSWORD='${DECRYPTED//\'/\'\\\'\'}'
export RESTIC_REPOSITORY='${RESTIC_REPOSITORY//\'/\'\\\'\'}'
export PG_DSN='${PG_DSN//\'/\'\\\'\'}'
export MONGO_URI='${MONGO_URI//\'/\'\\\'\'}'
export PUSHGATEWAY_URL='${PUSHGATEWAY_URL//\'/\'\\\'\'}'
export AWS_ACCESS_KEY_ID='${AWS_ACCESS_KEY_ID:-}'
export AWS_SECRET_ACCESS_KEY='${AWS_SECRET_ACCESS_KEY:-}'
export AWS_DEFAULT_REGION='${AWS_DEFAULT_REGION:-ru-central1}'
EOF
chmod 600 /etc/profile.d/restic.sh

# Drop secrets from the environment before exec'ing crond. The decrypted
# password lives only in /etc/profile.d/restic.sh from here on.
unset RESTIC_PASSWORD_CIPHERTEXT YC_SA_JSON_CREDENTIALS

exec "$@"
