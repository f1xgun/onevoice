# Security & Performance — OneVoice

Defensive baseline for the whole codebase. Anything marked **MUST** is non-negotiable; the rest are strong defaults.

---

## Secret Handling

- **MUST never** log secrets: tokens, passwords, API keys, full JWTs, OAuth refresh tokens, cookies. Enforced by code review; `gosec` catches obvious cases.
- All secrets come from environment variables. `.env.example` documents them; `.env` is `.gitignore`'d.
- OAuth tokens stored in the DB are encrypted with AES-256-GCM (`pkg/crypto`). Encryption keys live in `ENCRYPTION_KEY` env var, not in the DB.
- Key rotation: plan for it — tokens should be re-encryptable with a new key without downtime.

## Authentication

- JWT access tokens (HMAC-SHA256, 15-minute expiry) — see [api-design.md](api-design.md#authentication).
- Refresh tokens stored as SHA-256 hashes in Redis (7-day TTL) + PostgreSQL `refresh_tokens` backup.
- Logout **MUST** invalidate the refresh token (delete from Redis).
- Platform-modifying operations restricted to `owner` / `admin` roles (RBAC in `pkg/domain/roles.go`).

## Input Validation

- Validate every external input with `go-playground/validator/v10` at the handler boundary.
- Convert validation errors to HTTP 400 with field-level `details`.
- Never trust `channel_id`, `business_id`, or other identifiers from the LLM — resolve via the token-lookup fallback pattern (`GetDecryptedToken` in `services/api/internal/service/integration.go`).

## SQL / NoSQL Injection

- **MUST** use parameterized queries. For PostgreSQL: `pgx` + `squirrel` (the builder handles placeholders automatically).
- **MUST never** build SQL with `fmt.Sprintf`. No exceptions.
- MongoDB: pass BSON documents, not string-concat queries.

## XSS / CSRF

- React escapes by default; don't call `dangerouslySetInnerHTML` unless the content is vetted (LLM output is **not** vetted).
- CSRF tokens for state-changing operations that use cookie-based auth (not needed for pure Bearer-token APIs, but if session cookies are ever added, this matters).

## Rate Limiting

Three layers, each with its own purpose:

1. **API middleware** (Redis, `services/api/internal/middleware/ratelimit.go`) — per-IP + per-endpoint, guards against brute-force and noisy clients.
2. **LLM Router** (Redis, `pkg/llm/ratelimit.go`) — per-user, per-tier (requests/min, tokens/min, tokens/month, daily spend cap).
3. **Platform agents** — each respects the upstream platform's limits (VK quotas, Telegram `429 retry_after` with backoff queue).

## Transport

- HTTPS only in production. HTTP → HTTPS redirect at the load balancer / nginx.
- CORS: explicit allowlist of origins (`services/api/internal/middleware/cors.go`); never `*` in production.

## Audit Trail

- Sensitive actions append to `audit_logs` (PostgreSQL, append-only).
- Events to log: login, token refresh, logout, integration connect/disconnect, role change, admin operations.
- Fields: `user_id`, `action`, `resource`, `details` JSONB, `created_at`.
- **MUST** not include raw secrets in `details`.

## Compliance

- Russian 152-ФЗ (personal data law) — user data is stored within scope; no export outside approved regions.
- Structured logs include a correlation ID per request (`X-Request-ID`) so events can be traced without containing sensitive payloads.

## Pre-deploy Checklist

- [ ] Every secret is in env vars (not in code, not in version control).
- [ ] OAuth tokens encrypted with AES-256-GCM.
- [ ] Rate limiting enabled (Redis) on all public endpoints.
- [ ] CORS allowlist configured (not `*`).
- [ ] HTTPS enforced.
- [ ] Input validation on every endpoint.
- [ ] SQL injection audit (`grep 'Sprintf.*SELECT'` returns nothing meaningful).
- [ ] Audit logs firing for sensitive actions.

---

## Performance

### Backend

- Connection pools for every stateful dependency (PostgreSQL, MongoDB, Redis).
- Cache expensive queries in Redis (5–60 min TTL depending on volatility).
- Paginate list endpoints (default 20, max 100) — see [api-design.md](api-design.md#pagination).
- Indexes on every frequently-queried field. Review `migrations/postgres/*.sql` when adding a new query pattern.
- Stream large responses: SSE for chat, chunked transfer for file downloads.

### Frontend

- Next.js `Image` component for all images — automatic optimization.
- Route-level code splitting: `dynamic(() => import(...), { ssr: false })` for heavy client-only components.
- Debounce search inputs (300 ms).
- Virtualize long lists (`react-window` or equivalent).
- Check bundle size with `pnpm build`; investigate any single chunk > 250 KB gzipped.

### Targets

| Metric | Target |
|---|---|
| API latency (p95, excluding external calls) | < 500 ms |
| LLM response (p95) | < 10 s |
| Sync time per platform (target, excl. external moderation) | < 30 s |
| Uptime | > 99.5% |
