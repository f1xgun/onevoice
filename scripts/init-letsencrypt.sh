#!/usr/bin/env bash
#
# init-letsencrypt.sh — first-time TLS bootstrap for the production overlay.
#
# Solves the chicken-and-egg: nginx is configured to load the cert from
# /etc/letsencrypt/live/${DOMAIN}/fullchain.pem and will refuse to start
# if it's missing — but certbot can't issue the real cert without nginx
# already running to serve the ACME http-01 challenge.
#
# Strategy:
#   1. Read DOMAIN / ACME_EMAIL / CERTBOT_STAGING from .env.
#   2. Drop a self-signed placeholder under live/${DOMAIN}/ so nginx
#      can boot with valid (invalid-trust) certs.
#   3. Start nginx (it serves /.well-known/acme-challenge/ on :80).
#   4. Delete the placeholder, run certbot certonly --webroot.
#   5. Reload nginx so it picks up the real cert.
#
# Idempotent: re-running on a host that already has a valid cert is a
# no-op except for the nginx reload at the end.

set -euo pipefail

cd "$(dirname "$0")/.."

# --- Load env vars ----------------------------------------------------
if [ ! -f .env ]; then
  echo "[init-letsencrypt] .env not found. Copy .env.example to .env and fill it in." >&2
  exit 1
fi
# shellcheck disable=SC1091
set -a; . ./.env; set +a

: "${DOMAIN:?DOMAIN must be set in .env}"
: "${ACME_EMAIL:?ACME_EMAIL must be set in .env}"
STAGING_FLAG=""
if [ "${CERTBOT_STAGING:-0}" = "1" ]; then
  echo "[init-letsencrypt] STAGING mode — test certs (not browser-trusted)"
  STAGING_FLAG="--staging"
fi

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.prod.yml"

# --- 1. Placeholder self-signed cert ---------------------------------
# We can't bind-mount into the named volume cleanly, so the cleanest path
# is `docker compose run --rm --entrypoint sh certbot -c '...'` which
# shares the letsencrypt_data volume.
LIVE_DIR="/etc/letsencrypt/live/${DOMAIN}"
ARCHIVE_DIR="/etc/letsencrypt/archive/${DOMAIN}"

if $COMPOSE run --rm --entrypoint sh certbot -c "test -f ${LIVE_DIR}/fullchain.pem"; then
  echo "[init-letsencrypt] Real cert already present for ${DOMAIN} — skipping issuance."
  $COMPOSE up -d nginx
  $COMPOSE exec nginx nginx -s reload || true
  exit 0
fi

echo "[init-letsencrypt] Generating self-signed placeholder for ${DOMAIN}..."
$COMPOSE run --rm --entrypoint sh certbot -c "\
  mkdir -p ${LIVE_DIR} ${ARCHIVE_DIR} && \
  openssl req -x509 -nodes -newkey rsa:2048 \
    -days 1 \
    -keyout ${LIVE_DIR}/privkey.pem \
    -out    ${LIVE_DIR}/fullchain.pem \
    -subj   '/CN=${DOMAIN}' >/dev/null 2>&1 \
"

# --- 2. Start nginx so the ACME challenge can hit port 80 -------------
echo "[init-letsencrypt] Starting nginx with placeholder certs..."
$COMPOSE up -d nginx

# Give nginx ~5s to bind 80/443.
sleep 5

# --- 3. Delete placeholder and request the real cert -----------------
echo "[init-letsencrypt] Removing placeholder, requesting real cert from Let's Encrypt..."
$COMPOSE run --rm --entrypoint sh certbot -c "rm -rf ${LIVE_DIR} ${ARCHIVE_DIR}"

# Need to recreate the directory hierarchy under live/ before certbot
# writes — certbot expects to be the one creating it.
$COMPOSE run --rm certbot certonly \
  --webroot \
  --webroot-path=/var/www/certbot \
  --email "${ACME_EMAIL}" \
  --agree-tos --no-eff-email \
  --rsa-key-size 4096 \
  $STAGING_FLAG \
  -d "${DOMAIN}"

# --- 4. Reload nginx with real cert ----------------------------------
echo "[init-letsencrypt] Reloading nginx to pick up the new cert..."
$COMPOSE exec nginx nginx -s reload

echo "[init-letsencrypt] Done. Verify with: curl -I https://${DOMAIN}/health/live"
