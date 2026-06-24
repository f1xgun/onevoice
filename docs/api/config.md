# API configuration

`services/api/internal/config/config.go` loads the API service config from environment variables at startup. `config.Load()` returns `*Config` or an error; required-but-missing or malformed inputs fail loud — the process exits with a structured error before the HTTP listeners start.

## Single source of truth

Cryptographic key lengths are sourced from their owning packages:

- JWT secret minimum length: `services/api/internal/auth.JWTSecretMinLen`
- Encryption key length: `pkg/crypto.AES256KeyLen`

This keeps API config validation aligned with the packages that actually consume the keys.

## Fail-loud parsing policy

Most numeric/duration env vars use one of two helpers:

- `getEnvInt` / `getEnvDuration` — best-effort. Empty or malformed input falls back to the supplied default so an operator typo can't crash startup of an otherwise-healthy service. Used for tunable knobs where a wrong value is at most a tuning regression.
- `parseIntEnv` / `parseDurationEnv` — fail loud. Empty returns the default; non-empty MUST parse or `Load()` aborts boot with a forensic error. Used for knobs where silent default coercion has bitten production before (cost-guards, Postgres pool sizing, SSE cap).

`parseFloat` for `LLM_FREE_TIER_DAILY_SPEND_USD` is best-effort (silent fallback on parse error) — historical convention preserved.

## Required fields

`Load()` returns an error if any of these are missing or invalid:

- `JWT_SECRET` — required, must be ≥ `auth.JWTSecretMinLen` characters.
- `ENCRYPTION_KEY` — required, must be exactly `crypto.AES256KeyLen` bytes.

## Field reference

### Server + lifecycle

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `Port` | `PORT` | `8080` | Public listener port. |
| `InternalPort` | `INTERNAL_PORT` | `8443` | mTLS internal listener port. |
| `SecureCookies` | `SECURE_COOKIES` | `true` | Sets `Secure` flag on auth cookies. |
| `ShutdownTimeout` | `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown budget. |
| `HTTPReadTimeout` | `HTTP_READ_TIMEOUT` | `15s` | `http.Server.ReadTimeout` for the public API. |
| `HTTPReadHeaderTimeout` | `HTTP_READ_HEADER_TIMEOUT` | `10s` | `http.Server.ReadHeaderTimeout` for the internal mTLS server. |
| `HTTPIdleTimeout` | `HTTP_IDLE_TIMEOUT` | `60s` | Keep-alive socket idle timeout. |

Defaults preserve the values that were hardcoded in `services/api/cmd/main.go` before extraction.

### Postgres

| Field | Env var | Default | Range | Semantic |
|---|---|---|---|---|
| `PostgresHost` | `POSTGRES_HOST` | `localhost` | — | DB host. |
| `PostgresPort` | `POSTGRES_PORT` | `5432` | — | DB port. |
| `PostgresUser` | `POSTGRES_USER` | `postgres` | — | DB user. |
| `PostgresPass` | `POSTGRES_PASSWORD` | (empty) | — | DB password. |
| `PostgresDB` | `POSTGRES_DB` | `onevoice` | — | DB name. |
| `PGMaxConns` | `PG_MAX_CONNS` | `25` | `0 < n ≤ MaxInt32` | pgxpool max connections. Upper bound matches `pgxpool.Config.MaxConns` (int32) so `wire/databases.go` int→int32 conversions are gosec G115 safe. |
| `PGMinConns` | `PG_MIN_CONNS` | `2` | `0 ≤ n ≤ PGMaxConns` | pgxpool min idle connections. |
| `PGMaxConnLifetime` | `PG_MAX_CONN_LIFETIME` | `30m` | Go duration | Hard lifetime cap. |
| `PGMaxConnIdleTime` | `PG_MAX_CONN_IDLE_TIME` | `15m` | Go duration | Idle reaper. |
| `PGHealthCheckPeriod` | `PG_HEALTH_CHECK_PERIOD` | `1m` | Go duration | Background health check cadence. |
| `PGMaxConnLifetimeJitter` | `PG_MAX_CONN_LIFETIME_JITTER` | `3m` | Go duration | Jitter on lifetime cap so connections don't expire in lockstep. |

Defaults sized for free-beta single-pod / ~10–20 concurrent chats; operators raise via `PG_*` env at scale. Parsing uses `parseIntEnv` / `parseDurationEnv` — fail loud on typos because silent default coercion would silently hide pool starvation in production.

### Mongo + Redis

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `MongoURI` | `MONGO_URI` | `mongodb://localhost:27017` | Conversations / messages. |
| `MongoDB` | `MONGO_DB` | `onevoice` | DB name. |
| `RedisHost` | `REDIS_HOST` | `localhost` | Sessions, rate limit, lockout. |
| `RedisPort` | `REDIS_PORT` | `6379` | Redis port. |

### Auth + crypto

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `JWTSecret` | `JWT_SECRET` | (required) | Token signing key. Must be ≥ `auth.JWTSecretMinLen` chars. |
| `EncryptionKey` | `ENCRYPTION_KEY` | (required) | At-rest encryption for OAuth tokens. Must be exactly `crypto.AES256KeyLen` bytes. |

### OAuth credentials

| Field | Env var | Default | Notes |
|---|---|---|---|
| `VKClientID` | `VK_CLIENT_ID` | (empty) | VK ID app for user auth. |
| `VKClientSecret` | `VK_CLIENT_SECRET` | (empty) | |
| `VKRedirectURI` | `VK_REDIRECT_URI` | `http://localhost/api/v1/oauth/vk/callback` | Dev fallback; production must set explicitly. |
| `VKServiceKey` | `VK_SERVICE_KEY` | (empty) | Service access token from the VK Mini-App that backs `wall.getComments` / `groups.getById`. Intentionally separate from `VKClientID`. |
| `YandexClientID` | `YANDEX_CLIENT_ID` | (empty) | |
| `YandexClientSecret` | `YANDEX_CLIENT_SECRET` | (empty) | |
| `YandexRedirectURI` | `YANDEX_REDIRECT_URI` | `http://localhost/api/v1/oauth/yandex_business/callback` | |
| `TelegramBotToken` | `TELEGRAM_BOT_TOKEN` | (empty) | Bot token for paste-flow connect. |
| `GoogleClientID` | `GOOGLE_CLIENT_ID` | (empty) | |
| `GoogleClientSecret` | `GOOGLE_CLIENT_SECRET` | (empty) | |
| `GoogleRedirectURI` | `GOOGLE_REDIRECT_URI` | `http://localhost/api/v1/oauth/google_business/callback` | |

Production deployments must set the `*_REDIRECT_URI` env vars; the localhost defaults are dev-only.

### Orchestrator + NATS

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `OrchestratorURL` | `ORCHESTRATOR_URL` | `http://localhost:8090` | Base URL for chat / resume / `/internal/tools`. |
| `OrchestratorFetchTimeout` | `ORCHESTRATOR_FETCH_TIMEOUT` | `10s` | Per-request budget on `/internal/tools/*` and token-refresh fan-out. |
| `NATSUrl` | `NATS_URL` | (empty) | Optional. Empty disables review sync + agent-task NATS dispatch. |

### Review sync + AI draft

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `ReviewSyncInterval` | `REVIEW_SYNC_INTERVAL_MINUTES` | `30` | Background ticker period; `0` disables. |
| `ReviewDraftEnabled` | `REVIEW_DRAFT_ENABLED` | `false` | LLM-backed draft pass after every successful sync. Disabled by default to avoid silent LLM spend on upgrade. |
| `ReviewDraftMaxExamples` | `REVIEW_DRAFT_MAX_EXAMPLES` | `5` | Caps the few-shot context window. |
| `ReviewDraftBatchLimit` | `REVIEW_DRAFT_BATCH_LIMIT` | `10` | Caps drafts per sync pass. |

### Object storage (MinIO / S3)

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `S3Endpoint` | `S3_ENDPOINT` | `minio:9000` | |
| `S3AccessKey` | `S3_ACCESS_KEY` | `minioadmin` | |
| `S3SecretKey` | `S3_SECRET_KEY` | `minioadmin` | |
| `S3Bucket` | `S3_BUCKET` | `onevoice` | |
| `S3UseSSL` | `S3_USE_SSL` | `false` | |
| `S3PublicURLPrefix` | `S3_PUBLIC_URL_PREFIX` | `/media` | Prefix used in client-facing URLs. |

### Public URL + CORS

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `PublicURL` | `PUBLIC_URL` | `http://localhost:8080` | Used in email link templates (verify, password reset). |
| `CORSAllowedOrigins` | `CORS_ALLOWED_ORIGINS` | `["http://localhost:3000"]` | Comma-separated. In production this MUST be set to the public frontend origin (e.g. `https://app.example.com`); a missing env var leaves the API reachable only from localhost. Empty entries (e.g. `"a,,b"`) are dropped. |

### Per-endpoint per-minute rate limits

Operators tune via `RATE_LIMIT_*` env vars when the service sees abnormal traffic shape (e.g., a customer integration polling `/chat`).

| Field | Env var | Default | Notes |
|---|---|---|---|
| `RateLimitRegister` | `RATE_LIMIT_REGISTER` | `5` | Tightest — register is the highest-abuse target. |
| `RateLimitLogin` | `RATE_LIMIT_LOGIN` | `10` | Also used for `/auth/refresh` and invitation preview/accept. |
| `RateLimitChat` | `RATE_LIMIT_CHAT` | `10` | Per-user budget. Shared with HITL. |
| `RateLimitHITL` | `RATE_LIMIT_HITL` | `10` | |
| `RateLimitConsents` | `RATE_LIMIT_CONSENTS` | `10` | Per-user budget for `/auth/consents` + `/users/me/consents/pdn/withdraw`. 10/min/user is generous (genuine retry budget, blocks UPSERT thrash). |

### Email infrastructure

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `UnisenderAPIKey` | `UNISENDER_API_KEY` | (empty) | Empty = `NoopSender` (dev/local); set = `UnisenderSender`. Operators: see `docs/runbook-email-dns.md` for the DKIM/SPF/DMARC pre-req. |
| `UnisenderFromEmail` | `UNISENDER_FROM_EMAIL` | `noreply@onevoice.app` | |
| `UnisenderFromName` | `UNISENDER_FROM_NAME` | `OneVoice` | |
| `OutboxPollInterval` | `OUTBOX_POLL_INTERVAL` | `5s` | |
| `OutboxMaxAttempts` | `OUTBOX_MAX_ATTEMPTS` | `5` | |

### Legal entity (152-ФЗ Art. 14 data controller)

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `LegalEntityName` | `LEGAL_ENTITY_NAME` | `[Юридическое лицо — будет обновлено]` | Placeholder default; pre-launch checklist verifies a real value. |
| `LegalINN` | `LEGAL_INN` | (empty) | |
| `LegalAddress` | `LEGAL_ADDRESS` | (empty) | |
| `LegalEmailPDN` | `LEGAL_EMAIL_PDN` | `—` | Placeholder default. |

When any of these is a placeholder, `/legal/*` renders fallback copy and the footer emits `console.warn` (must not crash). The frontend reads mirrored `NEXT_PUBLIC_LEGAL_*` vars so the data-controller block renders SSR-safe.

### Health checks

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `HealthCheckTimeout` | `HEALTH_CHECK_TIMEOUT` | `2s` | Caps any single dep ping inside `/health/ready`. Checks run concurrently (`sync.WaitGroup` in `pkg/health.ReadyHandler`), so total wall-clock budget = `HealthCheckTimeout` (max, not Σ deps × timeout). 2s preserves k8s `readinessProbe` (5s default) headroom. Defensive clamp on `≤ 0` re-asserts the 2s default. |

### Lockout + SmartCaptcha + trusted proxies

Defaults match `pkg/lockout.Default*` constants so they stay in sync. The clamp on `≤ 0` defends against operator typos (negative threshold etc.).

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `LockoutFailThresholdCaptcha` | `LOCKOUT_FAIL_THRESHOLD_CAPTCHA` | `4` | Counter at which `TierCaptcha` kicks in. |
| `LockoutFailThresholdLock` | `LOCKOUT_FAIL_THRESHOLD_LOCK` | `10` | Counter at which `TierLocked` kicks in. |
| `LockoutDuration` | `LOCKOUT_DURATION` | `15m` | Redis TTL and lock window. Also the `retry_after_seconds` value on `423` responses. |
| `SmartCaptchaSiteKey` | `SMARTCAPTCHA_SITE_KEY` | (empty) | Public key for the JS widget; exposed to the frontend via `NEXT_PUBLIC_SMARTCAPTCHA_SITE_KEY`. |
| `SmartCaptchaSecretKey` | `SMARTCAPTCHA_SECRET_KEY` | (empty) | Server-side validation secret. Empty = `Noop` verifier (captcha disabled). |
| `TrustedProxyCIDRs` | `TRUSTED_PROXY_CIDRS` | (empty) | Comma-separated CIDR list controlling which `X-Forwarded-For` sources are trusted. Empty falls back to Yandex Cloud LB defaults. |
| `SmartCaptchaFailOpen` | `SMARTCAPTCHA_FAIL_OPEN` | `true` | On `ErrCaptchaTransient` (Yandex unreachable): `true` → log+proceed (safer default for legitimate users during Yandex outages); `false` → reject as `403`. |

### LLM (auto-titler + cost guards)

Auto-titler env loading mirrors `services/orchestrator/internal/config/config.go` but does **NOT** fail-fast on missing `LLMModel` — graceful disable mandated so the API service boots in dev environments with no LLM env configured at all.

| Field | Env var | Default | Semantic |
|---|---|---|---|
| `LLMModel` | `LLM_MODEL` | (empty) | Default model id. Optional on API (required on orchestrator). |
| `LLMTier` | `LLM_TIER` | `free` | Used by SSE cap and rate-limiter to scope budgets. |
| `TitlerModel` | `TITLER_MODEL` | falls back to `LLMModel` | When both unset, titler disabled (graceful no-op). |
| `OpenRouterAPIKey` | `OPENROUTER_API_KEY` | (empty) | At least one provider key must be set when `TitlerModel != ""` — otherwise the titler is left disabled. The API service does NOT fail-fast on missing keys (different from orchestrator, which requires `LLM_MODEL`). |
| `OpenAIAPIKey` | `OPENAI_API_KEY` | (empty) | |
| `AnthropicAPIKey` | `ANTHROPIC_API_KEY` | (empty) | |
| `SelfHostedEndpoints` | `SELF_HOSTED_N_URL` / `_MODEL` / `_API_KEY` | (empty) | `parseIndexedEndpoints` scans `N = 0, 1, 2, …` stopping when `SELF_HOSTED_N_URL` is missing. Entries without `MODEL` are skipped. Lifted verbatim from `services/orchestrator/internal/config/config.go` so byte-identical semantics apply on the API side. |
| `FreeTierDailySpendUSD` | `LLM_FREE_TIER_DAILY_SPEND_USD` | `0` (silent fallback) | Daily spend cap for the free tier. Best-effort parse — invalid input leaves the field at zero. |
| `RedisDownPolicy` | `LLM_RATELIMIT_ON_REDIS_DOWN` | `block` | Must be `"block"` or `"local_fallback"` — any other value fails Load. |
| `LocalFallbackRequestsPerHour` | `LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR` | `2000` | Fail loud on non-integer input. Must be `> 0` when `RedisDownPolicy=local_fallback`. |
| `SSEMaxPerUser` | `SSE_MAX_PER_USER` | `3` | Per-user SSE concurrency cap (`0` disables). Fail loud on non-integer or negative input — silent default coercion has bitten cost-guard wiring before. The Redis-down decision is governed by the same `RedisDownPolicy` + `LocalFallbackRequestsPerHour` pair as the LLM rate limiter so one operator knob spans both gates. |
| `MessageHistoryLimit` | `MESSAGE_HISTORY_LIMIT` | `100` | Number of prior messages loaded into the LLM context per chat turn (and read by the auto-titler). Fail loud on non-integer input; must be `> 0` and `<= 500` (the upper bound caps prompt size / per-turn cost). Threaded from config into the chat-turn lifecycle and the titler handler so both stay consistent. |

`LLMTier` defaults to `"free"` when unset.

`SelfHostedEndpoint` carries:

- `URL` — endpoint URL.
- `Model` — model id (required; entries without `MODEL` are skipped silently to support a "configured but not yet enabled" deployment phase).
- `APIKey` — optional.

## Default endpoint URL constants

Internal `const` block. Production deployments MUST set the corresponding env vars; these are dev-mode fallbacks only:

- `defaultVKRedirectURI = "http://localhost/api/v1/oauth/vk/callback"`
- `defaultYandexRedirectURI = "http://localhost/api/v1/oauth/yandex_business/callback"`
- `defaultGoogleRedirectURI = "http://localhost/api/v1/oauth/google_business/callback"`
- `defaultOrchestratorURL = "http://localhost:8090"`
- `defaultPublicURL = "http://localhost:8080"`
- `defaultCORSDevOrigin = "http://localhost:3000"`

## Helper functions

| Function | Behavior |
|---|---|
| `getEnv` | String env with default. |
| `getEnvInt` | Int env with default. Silent fallback on parse error. |
| `getEnvDuration` | Go-duration env with default. Silent fallback on parse error. |
| `getEnvSlice` | Comma-separated env → trimmed `[]string`. Empty entries dropped; fully-blank value returns default. |
| `parseIntEnv` | Fail-loud int env. Empty → default; non-empty must parse or `Load` aborts. |
| `parseDurationEnv` | Fail-loud Go-duration env. Empty → default; non-empty must parse or `Load` aborts. |
| `parseIndexedEndpoints` | Scans `SELF_HOSTED_N_*` env vars (N = 0, 1, 2, …); stops on missing `URL`; skips entries with no `MODEL`. |
