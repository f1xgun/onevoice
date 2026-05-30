# Runbook: Backup & Restore Operations

**Audience:** Operator running OneVoice in production.
**Scope:** Daily backup operation, monthly end-to-end restore drill, disaster-recovery procedure when Yandex KMS is unreachable, weekly CI drill secrets.
**Owners:** ops@onevoice.app.
**Source-of-truth:** Phase 23-03 (OPS-01 + OPS-06). Decisions D-03 (cadence), D-04 (drill cadence), D-05 (alerting), D-06 (BusyBox crond), D-07 (KMS), D-08 (bucket).

## §1 First-time setup (operator one-time)

Run on a host with `yc` CLI installed and authenticated to the right Yandex Cloud folder.

1. Create the Object Storage bucket:
   ```bash
   yc storage bucket create --name onevoice-backups --default-storage-class STANDARD
   ```
2. Set the 30-day lifecycle policy on the `daily/` prefix (defense-in-depth — restic forget enforces the same retention):
   ```bash
   yc storage bucket update --name onevoice-backups \
       --lifecycle-rule '{"id":"daily-30d","filter":{"prefix":"daily/"},"status":"Enabled","expiration":{"days":30}}'
   ```
3. Create the KMS symmetric key:
   ```bash
   yc kms symmetric-key create --name onevoice-restic --default-algorithm aes-256
   ```
   Record the key id into `.env.prod` as `RESTIC_PASSWORD_KMS_KEY_ID`.
4. Encrypt the restic password:
   ```bash
   RESTIC_PW=$(openssl rand -base64 32)
   echo -n "$RESTIC_PW" | yc kms symmetric-crypto encrypt \
       --id "$RESTIC_PASSWORD_KMS_KEY_ID" \
       --plaintext-file - --ciphertext-file - \
       | base64 > restic.enc.b64
   # Save restic.enc.b64 contents into .env.prod as RESTIC_PASSWORD_CIPHERTEXT.
   # KEEP $RESTIC_PW in your password manager — disaster recovery if KMS is unreachable (see §5).
   ```

   The plaintext you save here — the raw `openssl rand -base64 32` output —
   is exactly what the container's `entrypoint.sh` recovers from KMS at
   boot (no double-decode). The same value is what you'd export to
   `RESTIC_PASSWORD` in the §5 disaster-recovery procedure. Regression
   guarded by `.github/workflows/backup-restore-drill.yml::entrypoint-roundtrip`
   (Phase 23-07 / WR-04).
5. Create the service account + KMS-decrypt role binding + key:
   ```bash
   yc iam service-account create --name onevoice-backup
   yc resource-manager folder add-access-binding <folder-id> \
       --role kms.keys.encrypterDecrypter \
       --service-account-name onevoice-backup
   yc iam key create --service-account-name onevoice-backup --output sa.json
   # Paste sa.json contents (single-line JSON) into .env.prod as YC_SA_JSON_CREDENTIALS.
   ```
6. Initialize the restic repository ONCE from a one-shot container:
   ```bash
   docker compose -f docker-compose.observability.yml run --rm backup \
       bash -lc '. /etc/profile.d/restic.sh && restic init'
   ```
   (The `. /etc/profile.d/restic.sh` pulls in the KMS-decrypted `RESTIC_PASSWORD`
   that `entrypoint.sh` wrote there before exec'ing the command.)

## §2 Daily operation

BusyBox `crond` (inside the `backup` container in `docker-compose.observability.yml`)
runs `/scripts/backup/backup.sh` at **00:00 UTC = 03:00 MSK** daily (D-03, D-06 amended).
Crontab installed by Dockerfile at `/etc/crontabs/root`:

```
0 0 * * * /scripts/backup/backup.sh >> /var/log/backup.log 2>&1
```

`crond` runs in the foreground (`crond -f -L /dev/stderr -d 8`) so Docker captures
job stderr.

The Grafana dashboard `Backup Health` shows `backup_last_success_timestamp` and
`backup_duration_seconds` — both metrics are produced by `backup.sh` pushing to
Prometheus pushgateway after each successful run. Prometheus scrapes pushgateway
via the `pushgateway` job in `observability/prometheus/prometheus.yml` (without
that scrape config the metric never reaches Prometheus — plan-checker W-5 fix).

Alertmanager rule (D-05): page if `time() - backup_last_success_timestamp > 26h`
(2h slack on a 24h cadence). Email destination: `ops@onevoice.app`.

## §3 Operator monthly drill (D-04)

On the first of every month, perform a full end-to-end restore drill against a
scratch host. CI catches `restic check` failures and decryption breaks weekly;
this drill catches operator-facing gaps (forgotten credentials, runbook drift,
RTO regression).

1. Provision a scratch PG + Mongo (single-host Docker compose works):
   ```bash
   docker run -d --name scratch-pg \
       -e POSTGRES_PASSWORD=drill -e POSTGRES_DB=scratch \
       -p 55432:5432 postgres:16
   docker run -d --name scratch-mongo -p 57017:27017 mongo:7
   ```
2. Run `restore.sh` against them via the backup container (which already has
   restic + pg_restore + mongorestore + KMS decrypt):
   ```bash
   docker compose -f docker-compose.observability.yml run --rm \
       -e SCRATCH_PG_DSN='postgres://postgres:drill@host.docker.internal:55432/scratch?sslmode=disable' \
       -e SCRATCH_MONGO_URI='mongodb://host.docker.internal:57017' \
       backup bash -lc '. /etc/profile.d/restic.sh && /scripts/backup/restore.sh'
   ```
3. Time the run end-to-end (`time` prefix); record outcome in §4 below.
4. Tear down the scratch hosts:
   ```bash
   docker rm -f scratch-pg scratch-mongo
   ```

## §4 Drill log

| Date       | Snapshot ID | RTO (min) | Result | Operator | Notes |
|------------|-------------|-----------|--------|----------|-------|
| YYYY-MM-DD | abc123      |        12 | OK     | ops      |       |

## §5 Disaster recovery: KMS unreachable

If Yandex KMS is down (very rare; would require a Yandex Cloud-wide outage) or
the `onevoice-restic` key was accidentally destroyed:

1. Retrieve the plaintext `RESTIC_PASSWORD` from the ops password manager
   (you saved it in §1 step 4).
2. On a recovery host with restic + postgres-client + mongodb-tools installed,
   export `RESTIC_PASSWORD` and `RESTIC_REPOSITORY` directly in the shell.
3. Skip `entrypoint.sh` entirely — run `restore.sh` from a normal shell:
   ```bash
   export RESTIC_PASSWORD='<from-password-manager>'
   export RESTIC_REPOSITORY='s3:https://storage.yandexcloud.net/onevoice-backups'
   export AWS_ACCESS_KEY_ID='<from-vault>'
   export AWS_SECRET_ACCESS_KEY='<from-vault>'
   export SCRATCH_PG_DSN='postgres://postgres:x@127.0.0.1:5432/scratch?sslmode=disable'
   export SCRATCH_MONGO_URI='mongodb://127.0.0.1:27017'
   bash scripts/backup/restore.sh
   ```
4. Once KMS recovers, re-encrypt the password with a fresh key and replace
   `RESTIC_PASSWORD_CIPHERTEXT` / `RESTIC_PASSWORD_KMS_KEY_ID` in `.env.prod`.

## §6 Weekly CI drill secrets

The workflow `.github/workflows/backup-restore-drill.yml` runs every Sunday at
04:00 UTC against a SEPARATE test bucket (`onevoice-backups-drill`) so a
corrupted production repo can be surfaced without burning a real restore
window. Required GitHub Actions secrets:

| Secret name | Source |
|---|---|
| `RESTIC_REPOSITORY_DRILL` | `s3:https://storage.yandexcloud.net/onevoice-backups-drill` |
| `RESTIC_PASSWORD_DRILL_PLAINTEXT` | `openssl rand -base64 32` — stored in password manager |
| `YC_S3_ACCESS_KEY_DRILL` | Service-account static S3 key for the drill bucket |
| `YC_S3_SECRET_KEY_DRILL` | Pair of the above |

The drill bucket is seeded by an offline weekly sync from the production bucket
(operator runs `restic copy --repo prod --repo2 drill` monthly). This keeps the
test bucket small and avoids exposing production KMS credentials in CI — the
drill uses a plaintext restic password held only as a GitHub Actions secret, on
a bucket that contains a sampled subset of production data. Trade-off accepted
per threat T-23.3-08.

If the workflow fails, page on-call: the production repo may be silently
corrupted, OR the drill bucket has drifted out of sync. Run the operator drill
in §3 against the production bucket to disambiguate.
