# Deployment Guide — OneVoice on a single VM

End-to-end playbook for getting OneVoice running on a fresh Linux VM behind a public domain with HTTPS.

> **Scope:** single-VM Docker Compose deploy. K8s / multi-node is out of scope (the archived TR mentions it, but no manifests exist yet).
>
> **Audience:** anyone with SSH + sudo on the target VM. No prior project knowledge assumed.

---

## 0. Prerequisites

| Requirement | Notes |
|---|---|
| Linux VM | Ubuntu 22.04 LTS or Debian 12 tested. 4 vCPU / 8 GB RAM minimum (Yandex.Business agent runs Playwright + Chromium). |
| Public IPv4 | The domain's A record must already point at it before §3. |
| DNS | One A record: `app.example.com → <VM IP>`. Wildcard not required. |
| Ports open in firewall | `22` (SSH), `80` (ACME + redirect), `443` (HTTPS). All others closed. |
| Docker | Engine ≥ 24, Compose plugin v2 (`docker compose`, not the old `docker-compose`). |
| Git | For cloning the repo. |

### Provision the VM

```bash
# Docker
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER && newgrp docker

# Firewall (UFW; if you use cloud security groups, set those equivalently)
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable

# Verify
docker compose version    # should print v2.x
```

### Clone the repo

```bash
sudo mkdir -p /opt && sudo chown $USER /opt
cd /opt && git clone https://github.com/f1xgun/onevoice.git
cd onevoice
git checkout main          # never deploy a feature branch
```

---

## 1. Configure secrets — `.env`

```bash
cp .env.example .env
```

Open `.env` and fill in **every variable that is blank**. The file is the single source of truth — every section is annotated inline.

### Required values you MUST generate

```bash
# JWT signing secret — 32+ chars
openssl rand -base64 48
# → paste into JWT_SECRET=

# AES-256 encryption key — EXACTLY 32 bytes
openssl rand -hex 16
# → paste into ENCRYPTION_KEY=  (32 hex chars = 32 ASCII bytes)

# MinIO root credentials
openssl rand -base64 24    # MINIO_ROOT_USER
openssl rand -base64 24    # MINIO_ROOT_PASSWORD

# PostgreSQL password (optional but recommended)
openssl rand -base64 24    # POSTGRES_PASSWORD
```

### Required values from external services

| Variable | Where to get it |
|---|---|
| `OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` | At least one. Get from the provider's console. |
| `TELEGRAM_BOT_TOKEN` | [@BotFather](https://t.me/BotFather) → `/newbot`. |
| `VK_CLIENT_ID`, `VK_CLIENT_SECRET`, `VK_SERVICE_KEY` | [vk.com/dev → My apps](https://vk.com/dev). Create a "VK ID" app and a "Standalone" Mini-App. |
| `YANDEX_CLIENT_ID`, `YANDEX_CLIENT_SECRET` | [oauth.yandex.ru](https://oauth.yandex.ru/) → register new application. |
| `GOOGLE_*` | Only if you intend to run the (unverified) Google Business agent. Otherwise leave blank. |

### Domain & TLS

```env
DOMAIN=app.example.com
ACME_EMAIL=ops@example.com
CERTBOT_STAGING=1                # FIRST RUN: keep 1 to avoid LE rate limits
CORS_ALLOWED_ORIGINS=https://app.example.com
PUBLIC_URL=https://app.example.com
VK_REDIRECT_URI=https://app.example.com/api/v1/oauth/vk/callback
YANDEX_REDIRECT_URI=https://app.example.com/api/v1/oauth/yandex_business/callback
```

> **Important:** update the redirect URIs **inside the VK and Yandex provider dashboards** to match these values. The OAuth callback will fail silently otherwise.

---

## 2. Internal mTLS certificates (agents ↔ API)

The four agent services authenticate to the API's internal port (`:8443`) with client certificates signed by a project-local CA. These are **not** the same as the public HTTPS certs — they never touch the internet.

```bash
make certs
ls certs/                 # ca.crt + server.{crt,key} + {telegram,vk,yandex-business}.{crt,key}
```

This is a one-shot. Re-run only if you rotate the CA. Files are gitignored.

---

## 3. Public HTTPS — first-time TLS bootstrap

```bash
# Sanity-check DNS first — must return the VM's public IP
dig +short ${DOMAIN:-app.example.com}

# Bootstrap. Idempotent. Reads DOMAIN / ACME_EMAIL / CERTBOT_STAGING from .env.
./scripts/init-letsencrypt.sh
```

What the script does (in case you need to debug it):

1. Generates a self-signed placeholder cert so nginx can boot.
2. `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d nginx`.
3. Deletes the placeholder, runs `certbot certonly --webroot` for a real cert.
4. Reloads nginx so it picks up the real cert without dropping connections.

### Verify the staging cert worked

```bash
curl -vI https://app.example.com/health/live 2>&1 | head -30
```

You should see `Server: nginx` and a Let's Encrypt **STAGING** issuer. The browser will warn — that's expected on staging.

### Promote to a real cert

```bash
# Edit .env → CERTBOT_STAGING=0
docker compose -f docker-compose.yml -f docker-compose.prod.yml run --rm \
  --entrypoint sh certbot -c \
  "rm -rf /etc/letsencrypt/live/${DOMAIN} /etc/letsencrypt/archive/${DOMAIN} /etc/letsencrypt/renewal/${DOMAIN}.conf"

./scripts/init-letsencrypt.sh    # re-runs and now gets a real cert
```

---

## 4. Start the stack

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

Watch the boot order:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml ps
docker compose -f docker-compose.yml -f docker-compose.prod.yml logs -f api orchestrator
```

Expected order: `postgres / mongodb / redis / nats / minio` → `migrate` (one-shot, exits 0) → `minio-init` (one-shot, exits 0) → `api / orchestrator / agent-*` → `frontend` → `nginx + certbot`.

### Smoke tests

```bash
# Public health endpoints
curl -fsS https://${DOMAIN}/health/live           # API
curl -fsS https://${DOMAIN}/api/v1/health         # API (versioned)

# Frontend loads
curl -fsSI https://${DOMAIN}/ | head -5

# Object storage rewrite works (returns 404 for a missing key, NOT 501)
curl -fsSI https://${DOMAIN}/media/does-not-exist.png; echo "exit=$?"
```

All four should return 200 (or 404 on the last one). A 502 means the upstream container isn't healthy — `docker compose logs <service>`.

---

## 5. Operational tasks

### Tail logs
```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml logs -f --tail=200 api
```

### Restart one service
```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml restart api
```

### Apply a code update
```bash
git pull --ff-only
docker compose -f docker-compose.yml -f docker-compose.prod.yml build --pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
# Migrations apply automatically via the `migrate` one-shot service.
```

### Database backups
PostgreSQL volume is `postgres_data`. Snapshot it from the host:

```bash
docker exec onevoice-postgres pg_dump -U postgres onevoice | gzip > backup-$(date +%F).sql.gz
```

MongoDB:

```bash
docker exec onevoice-mongodb mongodump --archive --gzip --db=onevoice > mongo-$(date +%F).archive.gz
```

Schedule these via cron on the host.

### TLS renewals
The `certbot` container runs `certbot renew` every 12h and the `nginx` container reloads every 6h, so renewals roll out automatically. Verify a cert is current with:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml run --rm \
  --entrypoint sh certbot -c "certbot certificates"
```

### Enable the Google Business agent (currently unverified)
It's in the `google` profile, off by default:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml --profile google up -d
```

---

## 6. Rollback

```bash
# Find the previous green commit
git log --oneline -10

# Roll code + rebuild
git checkout <sha>
docker compose -f docker-compose.yml -f docker-compose.prod.yml build --pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

Migrations are **forward-only** by convention. If a release introduced a destructive schema change, restore from the SQL/Mongo dump taken before the upgrade. There is no automatic down-migration path.

---

## 7. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `api` container restarts in a loop with `JWT_SECRET is required` | `.env` missing or compose run from a directory without it | Re-export `.env` path or run from `/opt/onevoice`. |
| `api` exits with `ENCRYPTION_KEY must be exactly 32 bytes` | Generated with `rand -base64 32` (44 chars) or `rand -hex 32` (64 chars) | Use `openssl rand -hex 16` exactly. |
| `nginx` exits with `cannot load certificate ... no such file` | Cert volume empty | Re-run `./scripts/init-letsencrypt.sh`. |
| Certbot returns `DNS problem: NXDOMAIN looking up A for ...` | DNS not propagated yet | `dig +short $DOMAIN`; wait or fix the A record. |
| Certbot says `too many certificates already issued` | Hit Let's Encrypt's 5/week prod limit | Set `CERTBOT_STAGING=1` and iterate until success, then flip to 0. |
| OAuth callback returns 400 `invalid redirect_uri` | Provider dashboard has the localhost dev URI | Update the redirect URI in the VK / Yandex dashboard to match `.env`. |
| Chat returns 503 from orchestrator | `LLM_MODEL` set but no provider key | Check at least one of `OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` is populated. |
| Image upload fails with `connect: connection refused` | MinIO never reached healthy | `docker logs onevoice-minio`; common cause is leftover lock files in the named volume. |
| Yandex.Business agent timeouts every action | Yandex blocked the Playwright fingerprint | Check `/tmp/rpa-screenshots/` on the host (mounted volume). |

---

## 8. Pre-deploy checklist (copy this into the PR description)

- [ ] DNS A record `app.example.com → <VM IP>` propagated (`dig +short`).
- [ ] `.env` filled in, no blank required fields; **no dev secrets reused**.
- [ ] `JWT_SECRET` ≥ 32 chars, `ENCRYPTION_KEY` exactly 32 bytes.
- [ ] `CORS_ALLOWED_ORIGINS` set to the public origin (not `localhost`, not `*`).
- [ ] OAuth redirect URIs match values in VK / Yandex dashboards.
- [ ] `make certs` run on the VM.
- [ ] Let's Encrypt staging cert obtained successfully, then promoted to real cert.
- [ ] `docker compose ... ps` shows all services `running` / `healthy`.
- [ ] `https://${DOMAIN}/health/live` returns 200.
- [ ] DB backup cron job installed on the host.
- [ ] `REVIEW_DRAFT_ENABLED=false` unless LLM budget is intentional.
