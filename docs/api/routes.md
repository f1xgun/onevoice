# API routes

`services/api/internal/router/router.go` wires every HTTP route exposed by the API service. There are two muxes:

- `Setup(...)` — the public chi router mounted on the main listener (`PORT`, default `8080`). Carries auth, business CRUD, integrations, chat, conversations, projects, search, reviews, posts, members, roles, invitations, audit log, account deletion, consents, telemetry, OAuth callbacks, the platform registry, Prometheus metrics, and health checks.
- `SetupInternal(...)` — the internal chi router mounted on the mTLS listener (`INTERNAL_PORT`, default `8443`). Carries `/internal/v1/tokens` and the billing endpoints. mTLS is enforced at the listener; routes inside are further gated by `RequireServiceIdentity`.

## Global middleware stack (outer → inner)

Both muxes share the same global wrapper order. Order matters — auth and metrics rely on earlier middleware:

1. `chimiddleware.RequestID` — generates `X-Request-ID` if absent.
2. `middleware.CorrelationID` — propagates `X-Correlation-ID` end-to-end.
3. `chimiddleware.Logger` — request log line.
4. `chimiddleware.Recoverer` — catches panics, returns 500.
5. `cors.Handler` *(public mux only)* — allowed origins driven by `CORS_ALLOWED_ORIGINS`; allows credentials; `MaxAge = 300s`. Allowed methods: `GET, POST, PUT, DELETE, OPTIONS`. Exposes `Link, X-Correlation-ID`.
6. `i18n.LocaleMiddleware` — runs after CORS/correlation but before security headers / metrics / auth so even unauthenticated error responses can be localized off `Accept-Language`.
7. `middleware.SecurityHeaders` *(public mux only)* — emits HSTS/X-Frame-Options/etc.
8. `metrics.HTTPMiddleware` *(public mux only)* — Prometheus per-request labels.

`chi.RealIP` is intentionally **not** mounted. It rewrites `r.RemoteAddr` from `X-Forwarded-For` unconditionally with no trust knob, which lets an attacker spoof the upstream peer IP and bypass the `TRUSTED_PROXY_CIDRS` trust gate in `middleware.ClientIP`. `middleware.ClientIP` is the single source of truth for "did this XFF entry come from a trusted proxy?".

## Per-endpoint per-minute rate limits

Backed by Redis. Defaults surfaced as env vars and passed to `Setup` via a `RateLimits` struct. The window is always `time.Minute`.

| Field | Env var | Why this budget |
|---|---|---|
| `Register` | `RATE_LIMIT_REGISTER` | Tightest — automated signup abuse is the primary cost of getting this wrong. |
| `Login` | `RATE_LIMIT_LOGIN` | Looser than register to absorb retry storms after backend hiccups. Also reused for `/auth/refresh` and the public invitation preview (same threat model). |
| `Chat` | `RATE_LIMIT_CHAT` | Per-user budget. Shared with HITL because the chat-proxy fans out into HITL resolve calls under the same auth. |
| `HITL` | `RATE_LIMIT_HITL` | Per-user budget for `/chat/{id}/resume`. Scope shared with `chat` for the budget bucket. |
| `Consents` | `RATE_LIMIT_CONSENTS` | Per-user budget for `/auth/consents` + `/users/me/consents/pdn/withdraw`. GET stays unthrottled. |

## Public mux: `/api/v1/...`

### Public (no auth)

- `POST /auth/register` — chi rate-limit `Register`.
- `POST /auth/login` — chi rate-limit `Login`. When `lock != nil`, `middleware.LockoutMiddleware` is layered **before** the rate limit so a locked account returns `423` (with `retry_after_seconds`) rather than `429`. When `lock == nil` the legacy rate-limit-only path is used (graceful disable when Redis is unavailable at boot).
- `POST /auth/refresh` — chi rate-limit `Login`.
- `POST /auth/password-reset/request` — no chi rate-limit wrapper. The handler does its own per-email Redis rate-limit inside the service so the timing-parity contract is enforced uniformly. A chi rate-limit here would short-circuit before the service runs and skew the unknown-email branch.
- `POST /auth/password-reset/confirm` — no GET handler. The frontend renders the page off `?token=…` and the user must explicitly POST after clicking the reveal CTA — scanner-protection (Outlook Safe Links / Yandex 360 link prefetch cannot consume the token via GET).
- `POST /auth/verify-email/confirm` — public (no JWT). The verify-email page is reachable when the user is logged out (e.g. clicks the link in another browser). Returns `204` with **no** `Set-Cookie` / session material. No GET handler — same scanner-protection as password-reset.
- `GET /oauth/vk/callback`, `GET /oauth/vk/community-callback`, `GET /oauth/yandex_business/callback`, `GET /oauth/google_business/callback` — OAuth code-flow callbacks. State parameter validates the session.
- `GET /platforms` — public platform registry (single source of truth for the integration list). Returns only non-sensitive metadata (name, description, configured/availability) so the marketing landing page can render without auth.
- `GET /invitations/{token}` — public invitation preview (the token IS the auth). chi rate-limit `Login` budget per IP, same threat model as login (automated abuse with no auth state).

### Authenticated, **never** decorated by `BlockWritesDuringGrace`

These are the user's escape hatches from the soft-deleted state plus idempotent reads. Right-to-erasure / right-to-verify cannot be gated by other middleware.

- `POST /auth/logout`
- `GET /auth/me`
- `POST /auth/verify-email/resend`
- `PATCH /auth/email-before-verify`
- `DELETE /users/me` — idempotent (second call surfaces `423` from the service, not the middleware).
- `POST /users/me/restore` — the explicit restore escape hatch.
- `POST /auth/consents` — `RateLimitByUser(Consents, scope="consents")`. Re-consent path; always reachable per 152-ФЗ Art. 21 (right to withdraw cannot be gated by verification or grace).
- `GET /users/me/consents` — unthrottled. Listing your own consents is non-mutating and userID-scoped.
- `POST /users/me/consents/pdn/withdraw` — `RateLimitByUser(Consents, scope="consents")`. Triggers the 30-day deletion flow so abuse is naturally self-limiting, but the budget guards against accidental client retry loops.

### Authenticated, write-gated by grace

`middleware.Auth(jwtSecret)` + (when `pgPool != nil`) `middleware.BlockWritesDuringGrace(pgPool, deletionGraceDaysForRouter)`. The grace middleware blocks `POST/PUT/PATCH/DELETE` for users inside the 30-day grace window. GETs bypass at the middleware layer (method-check guard).

- `PUT /auth/password`
- `PATCH /auth/locale` — UI language choice (`'ru'|'en'`). PATCH (not PUT) — partial; matches the verb used for other scalar mutations (e.g. `/members/{userId}`).
- `GET /permissions` — RBAC static permission registry. Auth-only, no business scope.
- `GET /businesses` — list user's businesses. Auth-only, NOT business-scoped.
- `POST /businesses` — gated by `RequireVerifiedEmailDay7` (day-7 soft-restrict). Listing remains open (reads are banner-only).
- `POST /reviews/refresh` — manual cross-business review sync. No business scope.
- `POST /telemetry` — frontend telemetry ingest.
- `POST /invitations/{token}/accept` — auth-required, NOT business-scoped (the `{token}` targets a specific business). `RateLimitByUser(Login, scope="invite_accept")` for defense-in-depth.

### Authenticated + business-scoped: `/api/v1/businesses/{id}/...`

Single chokepoint: `authz.RequireBusinessAccess(authzCache, GetUserID)` parses and validates the business UUID, looks up membership (returns `404` on non-member, not `403`), and injects `BusinessContext` with the caller's role + permissions into ctx.

#### Business profile
- `GET /` — get business.
- `PUT /` — update business.
- `PUT /schedule`, `PUT /voice-tone`, `PUT /logo`
- `GET /tool-approvals`, `PUT /tool-approvals`

#### Integrations
- `GET /integrations`, `DELETE /integrations/{integrationId}` — never gated by verification.
- `GET /integrations/vk/auth-url`, `GET /integrations/vk/communities`, `GET /integrations/vk/community-auth-url`, `GET /integrations/yandex_business/auth-url`, `GET /integrations/google_business/auth-url`, `GET /integrations/google_business/locations` — OAuth auth-url endpoints. Need JWT + business context to generate state.
- All `POST /integrations/*` connect / probe / select-location / verify / refresh endpoints are gated by `RequireVerifiedEmailDay0` (day-0 hard-block) when `users != nil`. Each connect endpoint is wrapped individually so the decorator order stays explicit at the route declaration. Attack surface is spam token connection by unverified accounts.

  Endpoints: `vk/connect`, `vk/{id}/refresh-name`, `yandex_business/probe`, `yandex_business/companies`, `yandex_business/connect`, `yandex_business/{id}/refresh-name`, `google_business/select-location`, `telegram/verify`, `telegram/connect`, `telegram/refresh`.

#### Chat + HITL
- `POST /chat/{conversationID}` — `RateLimitByUser(Chat, scope="chat")` + `RequireVerifiedEmailDay7` (day-7 soft-restrict). Rate limit comes **before** verify so a throttled request short-circuits before the DB lookup.
- `POST /chat/{id}/resume` — HITL resume. `RateLimitByUser(HITL, scope="chat")` (shared chat bucket).
- `POST /conversations/{id}/pending-tool-calls/{batch_id}/resolve` — HITL resolve.
- `GET /tools` — orchestrator tools registry passthrough.

#### Conversations
- `GET /conversations`, `POST /conversations`
- `GET /conversations/{id}`, `PUT /conversations/{id}`, `DELETE /conversations/{id}`
- `GET /conversations/{id}/messages`
- `POST /conversations/{id}/move`
- `POST /conversations/{id}/pin`, `POST /conversations/{id}/unpin`
- `POST /conversations/{id}/regenerate-title` — only mounted when the titler service is wired (graceful disable when no LLM provider configured).

#### Projects
- `GET /projects`, `POST /projects`
- `GET /projects/{id}`, `PUT /projects/{id}`, `DELETE /projects/{id}`
- `GET /projects/{id}/conversation-count`

#### Search
- `GET /search` — sidebar search. Mounted only when the search handler is wired.

#### Reviews & posts
- `GET /reviews`, `GET /reviews/{id}`, `PUT /reviews/{id}/reply`
- `GET /posts`, `GET /posts/{id}`

#### Agent tasks
- `GET /tasks`, `GET /tasks/stream` — task list + SSE stream.

#### Members + roles (RBAC)
- `GET /members`, `PATCH /members/{userId}`, `DELETE /members/{userId}`
- `GET /roles`, `POST /roles`, `PATCH /roles/{roleId}`, `DELETE /roles/{roleId}` — role CRUD. Gated by `RequireBusinessAccess` (inherited) + per-route `Can` check inside the handler.
- `GET /me/permissions` — actor's effective permissions in the active business. No additional permission gate beyond `RequireBusinessAccess` (any member can read their own permissions).

#### Invitations
- `POST /invitations` — gated by `RequireVerifiedEmailDay0` when `users != nil`. Unverified users cannot send invites (spam vector).
- `GET /invitations` (list pending), `DELETE /invitations/{inviteId}` — open inside the business-scoped group.

#### Audit logs
- `GET /audit-logs` — `PermAuditRead` is enforced inside the handler (Owner+Admin via seed). `RequireBusinessAccess` handles membership + business-id validation; the handler handles the permission check + cursor/filter validation.

### Outside `/api/v1`

- `GET /metrics` — Prometheus. Mounted on the main listener (`PORT`, default `8080`) but **not reachable from the public internet**: nginx proxies only `/api/v1`, `/media`, `/health`, and `/` — `/metrics` is intentionally excluded (`nginx/nginx.conf.template`). Prometheus scrapes it directly over the compose network (`api:8080/metrics`, `observability/prometheus/prometheus.yml`). It is deliberately **not** moved to the mTLS internal listener (`INTERNAL_PORT`, default `8443`) because Prometheus has no client cert provisioned; relocating it behind mTLS requires that provisioning decision first.
- `GET /health/live`, `GET /health/ready`, `GET /health` (backward-compat alias for live).

## Internal mux: `/internal/v1/...` (mTLS only)

`SetupInternal` carries identity-gated routes. mTLS is enforced at the listener; the `RequireServiceIdentity` middleware adds defense-in-depth by checking the client cert CN against an allowlist.

### Service-identity allowlist

`internalServiceIdentityAllowlist = ["orchestrator", "api"]` — kept narrow on purpose:

- `orchestrator` — the primary caller (LLM router → billingclient).
- `api` — defense-in-depth slot for the future case where the API service dials its own internal listener (e.g. titler self-bills). Today the API wires `Repos.Billing` directly in-process for those callers, so the entry is currently unused but reserved.

Platform agents (`telegram` / `vk` / `yandex_business` / `google_business`) are **intentionally NOT allowlisted** — they do not bill in v1.4.

### Routes

- `GET /internal/v1/tokens` — internal token lookup. mTLS only.
- `POST /internal/v1/billing/usage_logs` — billing write path. mTLS + `RequireServiceIdentity`.
- `GET /internal/v1/billing/daily_spend` — read path for the orchestrator's rate-limiter, called before every chat turn. Same gate as the write path so it cannot be probed off-cluster.
- `GET /health/live`, `GET /health/ready`, `GET /health` — duplicated on the internal mux for the internal load balancer.

## Soft-restrict wrappers

The `UserLookup`, `pgPool`, and `lock` parameters to `Setup` are nil-tolerant for legacy/test compatibility:

- `users == nil` — `RequireVerifiedEmailDay0` and `RequireVerifiedEmailDay7` degrade to `passThroughMiddleware` (no-op).
- `pgPool == nil` — `BlockWritesDuringGrace` is skipped; writes are not gated by the 30-day grace window.
- `lock == nil` — `LockoutMiddleware` is skipped; `/auth/login` falls back to legacy rate-limit-only.

The optional handler fields (`Permissions`, `Members`, `Roles`, `Invitations`, `AuditLog`, `UserDeletion`, `Consents`, `Titler`, `Search`, `HITL`, `Platforms`) are similarly nil-guarded — `nil` simply means the relevant routes aren't mounted.

## Deletion grace constant

`deletionGraceDaysForRouter = 30` mirrors `service.AccountDeletionService.graceDays`. The `BlockWritesDuringGrace` middleware needs the value to compute the `deletionDate` in the `423` body.
