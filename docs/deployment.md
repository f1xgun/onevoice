# Deployment Guide — OneVoice on a single VM

End-to-end playbook for getting OneVoice running on a fresh Linux VM (Yandex Cloud-friendly).

> **Scope:** single-VM Docker Compose deploy. K8s / multi-node is out of scope.
>
> **Audience:** anyone with SSH + sudo on the target VM. No prior project knowledge assumed.

---

## Choose a deployment mode

| Mode | When to pick it | HTTPS? | OAuth (VK/Yandex)? | Custom domain? |
|---|---|---|---|---|
| **A — HTTP on bare IP** | Quick demo, only Telegram bot needed | No | No (providers reject IP-based redirect URIs) | No |
| **B — HTTPS via `<ip>.nip.io`** | Demo with full functionality, no domain to buy | Yes (real Let's Encrypt cert) | Yes | No |
| **C — HTTPS with your own domain** | Production / staging | Yes | Yes | Yes |

You can start in mode A and move to B/C later — only the `.env` and one bootstrap command change.

---

## 0. Prerequisites

| Requirement | Notes |
|---|---|
| Linux VM | Ubuntu 22.04 LTS / Debian 12. **4 vCPU / 8 GB RAM minimum** — Yandex.Business agent runs Playwright + Chromium; on 2 vCPU / 4 GB it OOM-kills. |
| Public IPv4 | Static for modes B / C (so the IP and DNS don't desync). Ephemeral is OK for mode A. |
| DNS | Mode A: none. Mode B: none (`nip.io` resolves automatically). Mode C: one A record pointing your domain at the VM. |
| Inbound firewall | `22` (SSH), `80`, `443`. All others closed. |
| Docker | Engine ≥ 24, Compose plugin v2 (`docker compose`, not the old hyphenated form). |
| Git | For cloning the repo. |

### 0a. Yandex Cloud specifics

| What | Where |
|---|---|
| **Static IP** | Console → Compute → VM → Network interfaces → Public IP → reserve. Without this, stopping the VM gives you a new IP after start. |
| **Security group** (this is the main thing) | VPC → Security groups → attach to the VM's network interface. The VM-internal `ufw` runs **after** the SG filters traffic — opening ports in `ufw` alone is not enough. |
| Security group ingress rules | TCP `22` from `<your IP>/32`, TCP `80` from `0.0.0.0/0`, TCP `443` from `0.0.0.0/0`. ICMP from `0.0.0.0/0` is optional (lets the VM be pinged). |
| Image | Ubuntu 22.04 LTS (`ubuntu-2204-lts`). |
| SSH user | Whatever username you put in the cloud-init SSH key block — usually `ubuntu` or `yc-user`. There is no `root` login by default. |
| Boot disk | Network SSD, 30 GB (Docker images + 2 DBs eat ~10 GB). |

### Provision the VM

```bash
# Docker
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER && newgrp docker

# UFW — defense in depth on top of the cloud SG.
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable

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

## 1. Internal mTLS certificates (all modes)

The four agent services authenticate to the API's internal port (`:8443`) with client certificates signed by a project-local CA. These never touch the internet — they secure container-to-container traffic only.

```bash
make certs
ls certs/                 # ca.crt + server.{crt,key} + {telegram,vk,yandex-business}.{crt,key}
```

One-shot. Re-run only on CA rotation. Files are gitignored.

---

## 2. Configure secrets — `.env`

```bash
cp .env.example .env
```

Open `.env` and fill in **every blank required field**. The file is the single source of truth — every section is annotated inline.

### Required values you MUST generate

```bash
openssl rand -base64 48     # JWT_SECRET (≥ 32 chars)
openssl rand -hex 16        # ENCRYPTION_KEY (exactly 32 bytes — 16 hex bytes = 32 ASCII chars)
openssl rand -base64 24     # MINIO_ROOT_USER
openssl rand -base64 24     # MINIO_ROOT_PASSWORD
openssl rand -base64 24     # POSTGRES_PASSWORD (optional but recommended)
```

### Required values from external services

| Variable | Where to get it |
|---|---|
| `OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` | At least one. From the provider's console. |
| `TELEGRAM_BOT_TOKEN` | [@BotFather](https://t.me/BotFather) → `/newbot`. |
| `VK_CLIENT_ID`, `VK_CLIENT_SECRET`, `VK_SERVICE_KEY` | [vk.com/dev → My apps](https://vk.com/dev). Modes B / C only. |
| `YANDEX_CLIENT_ID`, `YANDEX_CLIENT_SECRET` | [oauth.yandex.ru](https://oauth.yandex.ru/). Modes B / C only. |
| `GOOGLE_*` | Only if you intend to enable the unverified Google Business agent. |

### Mode-specific values

#### Mode A — HTTP on bare IP

Substitute `203.0.113.10` with the VM's public IP.

```env
PUBLIC_URL=http://203.0.113.10
CORS_ALLOWED_ORIGINS=http://203.0.113.10
SECURE_COOKIES=false        # CRITICAL: without this, browsers won't store the session cookie over HTTP → login fails silently

# leave the TLS knobs empty — the prod overlay is not used
DOMAIN=
ACME_EMAIL=
```

Skip the VK/Yandex/Google OAuth fields entirely — those providers reject IP-based redirect URIs.

#### Mode B — HTTPS via `<ip>.nip.io`

`nip.io` is a free wildcard DNS service: `203-0-113-10.nip.io` resolves to `203.0.113.10` automatically, no registration. Let's Encrypt issues real certs for these hostnames, and OAuth providers accept them.

Substitute the IP fragment:

```env
DOMAIN=203-0-113-10.nip.io
ACME_EMAIL=you@example.com
CERTBOT_STAGING=1                                  # FIRST RUN: 1, then flip to 0
PUBLIC_URL=https://203-0-113-10.nip.io
CORS_ALLOWED_ORIGINS=https://203-0-113-10.nip.io
VK_REDIRECT_URI=https://203-0-113-10.nip.io/api/v1/oauth/vk/callback
YANDEX_REDIRECT_URI=https://203-0-113-10.nip.io/api/v1/oauth/yandex_business/callback
SECURE_COOKIES=true                                # default; HTTPS supports secure cookies
```

#### Mode C — HTTPS with your own domain

```env
DOMAIN=app.example.com
ACME_EMAIL=ops@example.com
CERTBOT_STAGING=1                                  # FIRST RUN: 1, then flip to 0
PUBLIC_URL=https://app.example.com
CORS_ALLOWED_ORIGINS=https://app.example.com
VK_REDIRECT_URI=https://app.example.com/api/v1/oauth/vk/callback
YANDEX_REDIRECT_URI=https://app.example.com/api/v1/oauth/yandex_business/callback
SECURE_COOKIES=true
```

> **Modes B and C:** update the redirect URIs **inside the VK and Yandex provider dashboards** to match the `.env` values. The OAuth callback returns 400 otherwise.

---

## 3. Start the stack

### Mode A — HTTP on bare IP

```bash
docker compose up -d
docker compose ps
docker compose logs -f api orchestrator
```

Open `http://<VM IP>` — you should see the frontend. Done.

### Modes B / C — HTTPS via Let's Encrypt

The prod overlay (`docker-compose.prod.yml`) adds nginx on `:443`, the `certbot` service, healthchecks, and parks the unverified Google Business agent in a `google` profile.

#### First-time TLS bootstrap

```bash
# Sanity-check DNS first
dig +short ${DOMAIN}               # Mode B: should return the VM IP via nip.io. Mode C: your A record's value.

# Bootstrap. Idempotent. Reads DOMAIN / ACME_EMAIL / CERTBOT_STAGING from .env.
./scripts/init-letsencrypt.sh
```

What it does:

1. Drops a self-signed placeholder cert so nginx can boot.
2. `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d nginx`.
3. Deletes the placeholder, runs `certbot certonly --webroot` for a real cert.
4. Reloads nginx so it picks up the new cert without dropping connections.

#### Verify the staging cert worked

```bash
curl -vI https://${DOMAIN}/health/live 2>&1 | head -30
```

You should see `Server: nginx` and a Let's Encrypt **STAGING** issuer. The browser will warn — that's expected on staging.

#### Promote to a real cert

```bash
# Edit .env → CERTBOT_STAGING=0
docker compose -f docker-compose.yml -f docker-compose.prod.yml run --rm \
  --entrypoint sh certbot -c \
  "rm -rf /etc/letsencrypt/live/${DOMAIN} /etc/letsencrypt/archive/${DOMAIN} /etc/letsencrypt/renewal/${DOMAIN}.conf"

./scripts/init-letsencrypt.sh    # re-runs and now gets a real cert
```

#### Bring everything up

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
docker compose -f docker-compose.yml -f docker-compose.prod.yml ps
docker compose -f docker-compose.yml -f docker-compose.prod.yml logs -f api orchestrator
```

Boot order: `postgres / mongodb / redis / nats / minio` → `migrate` (one-shot) → `minio-init` (one-shot) → `api / orchestrator / agent-*` → `frontend` → `nginx + certbot`.

---

## 4. Smoke tests

For modes B / C substitute `https://${DOMAIN}`; for mode A use `http://<IP>`.

```bash
URL=https://${DOMAIN}    # or URL=http://<VM IP> for mode A

curl -fsS  $URL/health/live              # API liveness — 200
curl -fsS  $URL/api/v1/health            # API versioned health — 200
curl -fsSI $URL/ | head -5               # Frontend — 200
curl -fsSI $URL/media/no-such-file.png   # Object storage rewrite — 404 (NOT 501)
```

A 502 from the first three means an upstream container isn't healthy — `docker compose logs <service>`.

---

## 5. Operational tasks

> Replace the compose command prefix with the right one for your mode:
> - **Mode A:** `docker compose`
> - **Modes B / C:** `docker compose -f docker-compose.yml -f docker-compose.prod.yml`

### Tail logs
```bash
<compose> logs -f --tail=200 api
```

### Restart one service
```bash
<compose> restart api
```

### Apply a code update
```bash
git pull --ff-only
<compose> build --pull
<compose> up -d
# Migrations apply automatically via the `migrate` one-shot service.
```

### Database backups
PostgreSQL:
```bash
docker exec onevoice-postgres pg_dump -U postgres onevoice | gzip > backup-$(date +%F).sql.gz
```
MongoDB:
```bash
docker exec onevoice-mongodb mongodump --archive --gzip --db=onevoice > mongo-$(date +%F).archive.gz
```
Schedule both via cron on the host.

### TLS renewals (modes B / C)
The `certbot` container loops `certbot renew` every 12 h and `nginx` reloads every 6 h, so renewals roll out automatically. Verify:

```bash
<compose> run --rm --entrypoint sh certbot -c "certbot certificates"
```

### Enable the Google Business agent (unverified)
Off by default. To start it:
```bash
<compose> --profile google up -d
```

---

## 6. Rollback

```bash
git log --oneline -10
git checkout <previous-green-sha>
<compose> build --pull
<compose> up -d
```

Migrations are **forward-only** by convention. If a release introduced a destructive schema change, restore from the dump taken before the upgrade. There is no automatic down-migration path.

---

## 7. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `api` container loops with `JWT_SECRET is required` | `.env` missing or compose run from a directory without it | Run from `/opt/onevoice` (the repo root). |
| `api` exits with `ENCRYPTION_KEY must be exactly 32 bytes` | Generated with `rand -base64 32` (44 chars) or `rand -hex 32` (64 chars) | Use `openssl rand -hex 16` exactly. |
| Login redirects back to login (mode A) | `SECURE_COOKIES` left at default `true` → browser refuses to store the cookie over HTTP | Set `SECURE_COOKIES=false`. |
| `nginx` exits with `cannot load certificate ... no such file` | Cert volume empty | Re-run `./scripts/init-letsencrypt.sh`. |
| Certbot returns `DNS problem: NXDOMAIN` | DNS not propagated, or in mode B the IP fragment is misspelled (dashes, not dots) | Mode B uses dashes: `203-0-113-10.nip.io`, not `203.0.113.10.nip.io`. |
| Certbot returns `too many certificates already issued` | Hit Let's Encrypt's 5/week prod limit | Set `CERTBOT_STAGING=1` and iterate; flip to 0 only when you're confident. |
| OAuth callback returns 400 `invalid redirect_uri` | Provider dashboard has the localhost dev URI | Update the redirect URI in the VK / Yandex dashboard to match `.env`. |
| OAuth callback rejected even though URIs match (mode A) | VK / Yandex don't accept bare-IP redirect URIs | Move to mode B (free nip.io HTTPS) or mode C. |
| Chat returns 503 from orchestrator | `LLM_MODEL` set but no provider key | Check at least one of `OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` is populated. |
| Image upload fails with `connect: connection refused` | MinIO never reached healthy | `docker logs onevoice-minio`; common cause is leftover lock files in the named volume — `docker volume rm onevoice_minio_data` and recreate. |
| Yandex.Business agent times out on every action | Yandex blocked the Playwright fingerprint | Check `/tmp/rpa-screenshots/` on the host (mounted volume). |
| External access works locally on the VM but not over the internet (YC) | Security group not attached to the VM's NIC, or its ingress rules don't include 80/443 | YC web console → VM → Network interfaces → Security groups. |

---

## 8. Pre-deploy checklist

- [ ] DNS resolves (mode A: VM IP reachable; mode B: `dig +short <ip>.nip.io` returns the IP; mode C: A record propagated).
- [ ] YC security group attached and lets 22 / 80 / 443 in.
- [ ] `.env` filled in, no blank required fields; **no dev secrets reused**.
- [ ] `JWT_SECRET` ≥ 32 chars, `ENCRYPTION_KEY` exactly 32 bytes.
- [ ] `CORS_ALLOWED_ORIGINS` matches the public origin (not `localhost`, not `*`).
- [ ] Mode A: `SECURE_COOKIES=false`. Modes B / C: leave at default `true`.
- [ ] OAuth redirect URIs (modes B / C) match values in the VK / Yandex dashboards.
- [ ] `make certs` run on the VM.
- [ ] Modes B / C: Let's Encrypt staging cert obtained, then promoted.
- [ ] `docker compose ... ps` shows all services `running` / `healthy`.
- [ ] Health endpoint returns 200.
- [ ] DB backup cron job installed on the host.
- [ ] `REVIEW_DRAFT_ENABLED=false` unless LLM budget is intentional.
