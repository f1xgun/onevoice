# Research Summary

**Date:** 2026-03-15
**Feeds into:** Requirements and roadmap creation for OneVoice hardening milestone.

---

## Key Findings

The system is functionally operational end-to-end (Telegram path proven), but has four major gaps before it can be called production-ready:

1. **Security:** Refresh token in localStorage is the single highest-risk issue. httpOnly cookie migration must happen before any real users onboard.
2. **Yandex RPA stubs:** All four Yandex.Business tools return errors or empty results. The scaffolding exists but no real DOM automation is implemented.
3. **VK agent is incomplete:** Missing photo post, scheduled post, comment reply, wall reading. Text-only post is insufficient for digital presence management.
4. **Observability is absent:** No health checks, no Prometheus metrics, no correlation IDs across services. Incidents are invisible until users complain.

---

## Stack Recommendations

1. **VK SDK:** Keep `github.com/SevereCloud/vksdk/v3` (current, correct). Do not use raw HTTP or v2. Add error code classification (`Error 5` = permanent, `Error 9` = rate-limited, `Error 15` = permanent) before any retry logic is useful.

2. **Playwright:** Keep `github.com/playwright-community/playwright-go` at pinned version. Do not use `chromedp`. Switch from per-call `playwright.Run()` (current) to a shared `BrowserPool` struct initialized at startup — eliminates 2–4s Chromium overhead per tool call.

3. **JWT:** Keep `golang-jwt/jwt/v5 v5.3.1` (correct). Replace `jwt.MapClaims` with typed struct + `jwt.ParseWithClaims()`. Add `jwt.WithExpirationRequired()`, `jwt.WithValidMethods([]string{"HS256"})`, and `iss`/`aud` validation. Do not switch to RS256.

4. **Metrics:** Use `github.com/prometheus/client_golang v1.22.x`. Skip OpenTelemetry at this stage — Prometheus + Grafana is sufficient. Only use low-cardinality labels: `service`, `platform`, `tool_name`, `status`. Never use `business_id` or `user_id` as labels (cardinality explosion).

5. **Logging:** Keep `log/slog` (stdlib). Switch `pkg/logger/New()` to `slog.NewJSONHandler` for log aggregation compatibility. Do not migrate to `zap` or `zerolog`.

---

## Table Stakes Features

### VK Agent (bring to parity with Telegram)
- `vk__publish_photo_post` — two-step upload: `PhotosGetWallUploadServer` → upload binary → `PhotosSaveWallPhoto` → `WallPost` with attachment string
- `vk__schedule_post` — `publish_date` parameter on `wall.post`; required for realistic social media workflow
- `vk__reply_comment` — `wall.createComment` with `owner_id` + `comment_id` + `message`
- `vk__delete_comment` — comment spam/moderation
- `vk__get_community_info` — LLM needs to read current community state without hallucinating
- `vk__get_wall_posts` — prevent duplicate content, report on recent activity

### Yandex.Business Agent (implement all stubs)
- `yandex_business__get_reviews` — navigate `/reviews`, extract rating/author/text/date/ID
- `yandex_business__reply_review` — navigate to review, click reply, fill, submit, confirm
- `yandex_business__update_info` — profile edit form: phone, website, description
- `yandex_business__update_hours` — per-day hour fields from structured JSON input
- Session canary check before every action — return `ErrSessionExpired` immediately, do not retry
- Cookie expiry alert at startup — log warning if any cookie expires within 7 days

### Security
- Refresh token → httpOnly + Secure + SameSite=Strict cookie (remove from localStorage)
- Typed JWT claims + `jwt.WithExpirationRequired()` + `jwt.WithValidMethods()`
- Rate limiting on `/auth/login` (10/min/IP), `/auth/register` (5/min/IP), `/chat/{id}` (10/min/user) using existing Redis rate limiter
- CSP + security headers middleware on API service
- Atomic refresh token rotation: `DELETE ... RETURNING` (eliminates replay race)
- `iss` + `aud` JWT claims on issue and validation

### Observability
- `/health/live` and `/health/ready` on all six services; readiness checks PostgreSQL + MongoDB + Redis + NATS as applicable
- Prometheus metrics on `/metrics`: HTTP request duration histogram, request counter, LLM call duration, tool call counter, NATS timeout counter
- `pkg/logger` JSON output with `service` field
- Correlation ID middleware (`X-Correlation-ID`) in API and orchestrator; propagate as `request_id` in A2A `ToolRequest`

---

## Architecture Patterns

### Token Lifecycle (centralized in API service)
Agents never hold long-lived tokens. They call `GET /internal/tokens/{businessID}?platform=vk` on every tool invocation. The API service handles refresh proactively inside `GetDecryptedToken`. Concurrent refresh races are prevented with a PostgreSQL advisory lock (`pg_try_advisory_xact_lock`) — no Redis required.

### RPA Session Management (shared BrowserPool)
```
Startup:  playwright.Run() → pw  →  pw.Chromium.Launch() → browser  →  browser.NewContext(cookies) → bCtx
Per call: bCtx.NewPage() → canaryCheck() → execute action → page.Close()
```
Never call `playwright.Run()` per tool invocation. One shared browser context per agent lifecycle. Canary check on every call detects session expiry before attempting writes.

### Error Taxonomy (apply consistently across all agents)
- **Transient** (network error, HTTP 5xx, VK Error 9): retry with exponential backoff
- **Permanent** (VK Error 5/15, `ErrSessionExpired`, selector not found): wrap in `NonRetryableError`; `withRetry` skips all retry attempts
- **Rate-limited** (VK Error 9, HTTP 429): backoff 1s minimum, surface to user rather than silent fail

### Health Check Pattern (pkg/health)
Reusable `Checker` with `CheckFn` map. Liveness: always 200. Readiness: dependency pings with 5s context timeout. Return structured JSON `{"status":"ok|degraded","checks":{...}}`. Cache results 5s to avoid hammering dependencies on every Kubernetes probe.

### Graceful Shutdown Order
1. Stop accepting new HTTP requests (`srv.Shutdown`)
2. Drain NATS subscriptions (`sub.Drain()` + WaitGroup for in-flight handlers)
3. Flush pending writes (MongoDB saves, billing logs)
4. Close DB connections (`pgPool.Close()`, `mongoClient.Disconnect()`)
5. Drain NATS connection (`nc.Drain()`, never `nc.Close()`)

Use `activeStreams sync.WaitGroup` in `ChatProxyHandler` to track SSE streams during shutdown.

### Correlation ID Propagation
Generated at API boundary → `X-Correlation-ID` HTTP header → `ToolRequest.request_id` NATS field → logged in every agent handler. Enables single-ID log grep across all services.

---

## Critical Pitfalls

### 1. VK Rate Limit Cascade (Error 9 flood)
**Risk:** LLM retrying a rate-limited VK call doubles the flood. VK does not automatically throttle.
**Prevention:** Classify Error 9 as a rate-limited (retryable with 1s+ backoff) error. Add per-businessID sliding window rate limiter in the VK agent (reuse existing Redis pattern). Surface "VK rate-limiting; wait 30s" to user instead of silent failure.

### 2. Yandex Browser Memory Leak
**Risk:** Hung pages accumulate in the shared browser process; agent pod OOM-kills silently. Each page ~50–150MB.
**Prevention:** `page.SetDefaultTimeout(30000)` at page creation. `defer page.Close()` guaranteed. Memory watchdog: if Chromium RSS > threshold, restart before next call. Consider per-call browser context isolation if memory growth is observed.

### 3. Refresh Token Replay Race
**Risk:** Concurrent refresh requests both read the old token before either deletes it. Attacker with stolen localStorage token can race alongside legitimate refresh.
**Prevention:** Atomic `DELETE ... RETURNING` for refresh token consumption. Refresh token family revocation: if a revoked family member is re-presented, revoke all tokens for that user.

### 4. Yandex Selector Drift (Silent Success)
**Risk:** DOM class rename makes selector match wrong element. Action "succeeds" (no error) but wrong data submitted or nothing changes.
**Prevention:** After every write action, assert a success confirmation element is visible (success toast, updated value). Treat absence of confirmation as failure. Capture screenshot at end of every call. Use `getByRole`/`getByText`/`getByLabel` over CSS selectors.

### 5. Prometheus Cardinality Explosion
**Risk:** Adding `business_id` or `user_id` as metric labels creates thousands of time series per metric; Prometheus OOM-kills.
**Prevention:** Hard rule: only `service`, `platform`, `tool_name`, `status`, `http_method`, `endpoint` (route pattern) as labels. Never UUIDs, emails, or free-form strings. Enforce via PR review. Monitor `prometheus_tsdb_head_series` monthly.

---

## Phase Implications

### What Must Come First (hard dependencies)

1. **Security hardening before any real users.** Refresh token → httpOnly cookie is a blocker. All subsequent auth improvements (typed claims, iss/aud, atomic rotation) build on this.
2. **VK error taxonomy before VK retry logic.** Retrying without error classification makes rate limit floods worse. Classify first, then add `withRetry` to VK client.
3. **Yandex BrowserPool before stub implementation.** Per-call `playwright.Run()` is too slow to validate RPA selectors iteratively. Refactor first, then implement tools.
4. **Health checks before adding Prometheus.** Dependency connectivity must be validated before metric alerting is meaningful.
5. **Canary check before any Yandex write tool.** Without it, expired cookie failures are indistinguishable from DOM failures.

### What Can Be Parallelized

- VK agent completion and Yandex agent BrowserPool refactor can proceed concurrently (different services, no shared code).
- Security hardening (auth middleware) and observability (health checks, Prometheus) can proceed concurrently after their respective prerequisites are met.
- JSON logging (`pkg/logger`) can be done as a standalone change in any phase.
- Correlation ID middleware can be added independently of Prometheus metrics.

### What Needs Special Attention

- **Yandex DOM selectors:** Write one tool at a time, validate against real Yandex.Business account, capture screenshots. Do not bulk-implement all four at once — selector failures are silent.
- **VK community permissions:** Validate admin role at OAuth time. Debugging a permissions error at first post is expensive (requires user re-auth).
- **Graceful shutdown:** Changes to `pkg/a2a/agent.go` affect all agents. Test shutdown sequence explicitly — NATS drain ordering matters.
- **SSE tool call persistence:** Move per-event MongoDB save out of the `done` handler to prevent data loss on client disconnect. This is a correctness issue, not just reliability.

---

## Open Questions

1. **Yandex.Business test account:** Is there a real Yandex.Business account available for RPA selector development and weekly smoke tests? Selectors cannot be written without a live account.

2. **VK test community:** Is there a VK community with admin token (offline scope) for integration tests? CI integration tests require a stable test community.

3. **Cookie refresh workflow for Yandex:** When Yandex session cookies expire (30–90 days), what is the operational procedure to refresh them? Does the team have a browser extension or manual flow documented?

4. **Deployment target:** Docker Compose (single-node) or Kubernetes? This determines whether health check probe configuration and the Redis-based distributed lock for conversation serialization are needed.

5. **JWT secret rotation schedule:** Has `JWT_SECRET` ever been rotated? Is there a runbook? The dual-key rotation pattern needs to be established before any real user data is stored.

6. **Prometheus/Grafana infrastructure:** Is there an existing Prometheus instance to scrape OneVoice metrics, or does a monitoring stack also need to be set up as part of this milestone?
