# Architecture Research

Research scope: hardening and integration patterns for OneVoice — a Go 1.24 microservice system
with NATS message bus, Playwright RPA, and multi-provider LLM routing.

---

## OAuth Token Lifecycle Management

### Problem Statement

VK uses OAuth 2.0 with access tokens that may expire (24h short-lived or "offline" long-lived).
Yandex.Business access tokens expire after ~1 year and require refresh. The current codebase stores
encrypted tokens in PostgreSQL and decrypts them at agent request time via
`GET /internal/tokens/{businessID}`. There is no proactive refresh path — tokens are only refreshed
if `token_expires_at` is within 1 hour at request time (per `services/api/internal/service/oauth.go`).

### VK Token Specifics

VK grants tokens in two modes:
- `offline` scope: non-expiring token (no refresh needed, but can be revoked)
- Without `offline`: short-lived, typically 24h, no refresh token provided

The current VK OAuth flow requests `offline` scope, so VK tokens should not require
refresh under normal operation. However, VK can revoke tokens (user deauthorizes the app,
password change, suspicious activity). This means the agent must detect 403/invalid_token
errors and propagate them as a permanent failure rather than retrying.

### Yandex.Business Token Specifics

Yandex issues access + refresh token pairs. Access tokens expire in 1 year. The refresh token
is valid indefinitely but is single-use — each refresh rotates both tokens.

### Standard Pattern for Microservice Token Lifecycle

**Component boundary:** Token lifecycle management lives entirely in the API service.
Agents never hold tokens beyond a single request — they fetch fresh tokens via the internal
token endpoint on every tool invocation. This means all refresh logic is centralized.

**Data flow for token refresh:**

```
Agent → GET /internal/tokens/{businessID}?platform=vk
                     ↓
         IntegrationService.GetDecryptedToken()
                     ↓
         Check token_expires_at
         If within refresh window (< 1h remaining):
                     ↓
         OAuthService.RefreshToken(platform, refresh_token)
                     ↓
         POST {provider_token_endpoint}
                     ↓
         Update integrations row (new tokens, new expiry)
                     ↓
         Return decrypted access token to agent
```

**Concurrency hazard:** Multiple agents hitting the token endpoint simultaneously can
trigger parallel refresh requests for the same integration. This is a double-spend
problem — a single-use Yandex refresh token consumed twice will make one request fail.

**Solution — advisory lock pattern in PostgreSQL:**

```sql
-- In the token refresh transaction:
SELECT pg_try_advisory_xact_lock(hashtext(business_id || platform))
```

If the lock is acquired, perform refresh and commit. If not acquired (another goroutine is
refreshing), wait briefly and re-read the row — the other goroutine will have written fresh
tokens. This pattern requires no Redis and avoids distributed lock complexity.

Implementation shape:

```go
func (s *OAuthService) RefreshIfNeeded(ctx context.Context, integration *domain.Integration) error {
    if time.Until(integration.TokenExpiresAt) > refreshThreshold {
        return nil // still valid
    }
    // Advisory lock scoped to this integration
    locked, err := s.db.TryAdvisoryLock(ctx, integration.ID)
    if err != nil {
        return err
    }
    if !locked {
        // Another goroutine is refreshing — re-read after brief wait
        time.Sleep(200 * time.Millisecond)
        return s.reloadToken(ctx, integration) // reads updated row
    }
    defer s.db.ReleaseAdvisoryLock(ctx, integration.ID)
    return s.doRefresh(ctx, integration)
}
```

**Error taxonomy for VK/Yandex token errors:**

| Error | Type | Agent action |
|-------|------|-------------|
| 401 / invalid_token | Permanent | Return error to orchestrator, do not retry |
| 429 / rate_limited | Transient | Retry with exponential backoff |
| 500 / provider_error | Transient | Retry once, then fail |
| Token expired (detected in API) | Handled by API | Transparent to agent after refresh |
| Refresh token invalid | Permanent | Return error; user must re-authorize |

**Build order for OneVoice:**

1. Add `token_expires_at` and `refresh_token` columns if not present (migration exists per INTEGRATIONS.md)
2. Implement `OAuthService.RefreshIfNeeded` with advisory lock
3. Call it inside `GetDecryptedToken` before returning to agent
4. Add `ErrTokenExpired` domain error; propagate as 401 from token endpoint
5. In agent handlers: detect permanent errors, return `ToolResponse{Success: false}` without retrying
6. Add integration test: simulate expired token, verify refresh happens exactly once

---

## RPA Session Management

### Problem Statement

The Yandex.Business agent uses Playwright with session cookies loaded from `YANDEX_COOKIES_JSON`
at runtime. Each tool call creates a new Playwright instance, launches Chromium, injects cookies,
and navigates to the target page. This is correct for isolation but introduces:
- High per-request latency (~2-4s Chromium startup)
- No session validation before action execution
- Retry logic that retries all errors including permanent ones (expired session, missing selector)

The `withPage` function in `browser.go` spawns a new `playwright.Run()` on every call, which
initializes the entire Playwright node process. This is extremely expensive.

### Recommended Session Pool Architecture

**Single Playwright instance, persistent browser context per agent lifecycle:**

```
Agent startup:
  playwright.Run() → pw instance (shared)
  pw.Chromium.Launch() → browser (shared)
  browser.NewContext(cookies) → bCtx (shared, reused across requests)

Per tool call:
  bCtx.NewPage() → page (created per call, closed after)
  canary check (navigate to /profile, assert not login page)
  execute action
  page.Close()
```

**Why context-level sharing (not page-level):** Browser contexts isolate cookies and local
storage. A context created once with cookies remains valid until Yandex invalidates the session.
Creating new contexts per request forces re-injection of cookies every time.

**Canary check before action:** Before each tool operation, verify the session is valid:

```go
func (b *BrowserPool) canaryCheck(page playwright.Page) error {
    if _, err := page.Goto("https://business.yandex.ru/", playwright.PageGotoOptions{
        WaitUntil: playwright.WaitUntilStateNetworkidle,
        Timeout:   playwright.Float(10000),
    }); err != nil {
        return err
    }
    // If redirected to login, session is invalid
    if strings.Contains(page.URL(), "passport.yandex.ru") {
        return ErrSessionExpired
    }
    return nil
}
```

**Non-retryable error classification:**

```go
type RPAErrorKind int

const (
    RPAErrorTransient RPAErrorKind = iota // network, timeout — retry
    RPAErrorSessionExpired                // cookie invalidated — do not retry
    RPAErrorSelectorNotFound              // DOM changed — do not retry, alert
    RPAErrorPermission                    // business access denied — do not retry
)

type RPAError struct {
    Kind    RPAErrorKind
    Message string
    Cause   error
}
```

`withRetry` must check `RPAError.Kind` before sleeping:

```go
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error {
    for i := range maxAttempts {
        err := fn()
        if err == nil {
            return nil
        }
        var rpaErr *RPAError
        if errors.As(err, &rpaErr) && rpaErr.Kind != RPAErrorTransient {
            return err // permanent — do not retry
        }
        if i < maxAttempts-1 {
            time.Sleep(time.Duration(1<<uint(i)) * time.Second)
        }
    }
    return fmt.Errorf("all %d attempts failed", maxAttempts)
}
```

**Cookie rotation strategy:**

Yandex session cookies expire when the user's Yandex session expires (logout, password change,
30-day idle). There is no programmatic refresh path — fresh cookies must be obtained by
re-authenticating via a browser and exporting cookies (e.g., via a browser extension).

Operational approach:
- Store cookies in API's `integrations` table (encrypted) rather than env var
- Add `YANDEX_COOKIE_EXPIRES_AT` metadata alongside cookies
- Set alert (log.Warn) when `time.Until(expiresAt) < 7 days`
- The "token endpoint" already handles Yandex credentials — cookies are just the `access_token`
  value stored as JSON string

**Headless vs headed:** Use headless for production. Add `PLAYWRIGHT_HEADLESS=false` env override
for local debugging. The `--disable-blink-features=AutomationControlled` flag already present in
the codebase is the correct anti-detection approach.

**Build order for OneVoice:**

1. Refactor `browser.go`: extract `BrowserPool` struct that holds `pw`, `browser`, `bCtx`
2. Initialize pool in `cmd/main.go` at startup, pass to `NewBrowser()`
3. Add `canaryCheck()` method, call before each tool action
4. Add `RPAError` types; update `withRetry` to distinguish transient vs permanent
5. Move cookie storage from env var to API integrations table; update token endpoint
6. Add cookie expiry warning: log at startup if `expiresAt < 7 days`
7. Implement stub tools: `GetReviews`, `ReplyReview`, `UpdateHours`, `UpdateInfo` using
   `page.Locator()` selectors — these are the HIGH severity TODOs in CONCERNS.md

---

## Health Check Patterns

### Problem Statement

No service in OneVoice exposes health endpoints. Kubernetes, Docker Swarm, and load balancers
need liveness and readiness probes. Without them, requests route to dead or degraded services.

### Liveness vs Readiness

**Liveness probe** (`/health/live`): Is the process alive and not deadlocked?
- Should always return 200 unless the process is stuck
- Check: goroutine count reasonable, no internal deadlock indicators
- For OneVoice: always return 200 (process-level; OS kills stuck processes)

**Readiness probe** (`/health/ready`): Can the service handle traffic?
- Check all critical dependencies
- Return 503 if any dependency is unhealthy
- This is the important one for OneVoice

### Dependency Check Matrix

| Service | Dependencies to check |
|---------|----------------------|
| API (8080) | PostgreSQL ping, MongoDB ping, Redis ping |
| Orchestrator (8090) | NATS connection, Redis (rate limiter), LLM provider reachable |
| Telegram Agent | NATS subscription active |
| VK Agent | NATS subscription active |
| Yandex Agent | NATS subscription active, Playwright process running |

### Implementation Pattern

```go
// pkg/health/checker.go
type CheckFn func(ctx context.Context) error

type Checker struct {
    checks map[string]CheckFn
}

func (c *Checker) Handler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()

        results := make(map[string]string)
        healthy := true
        for name, check := range c.checks {
            if err := check(ctx); err != nil {
                results[name] = err.Error()
                healthy = false
            } else {
                results[name] = "ok"
            }
        }

        status := http.StatusOK
        if !healthy {
            status = http.StatusServiceUnavailable
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(results)
    }
}
```

**PostgreSQL check:**
```go
func PostgresCheck(pool *pgxpool.Pool) CheckFn {
    return func(ctx context.Context) error {
        return pool.Ping(ctx)
    }
}
```

**MongoDB check:**
```go
func MongoCheck(client *mongo.Client) CheckFn {
    return func(ctx context.Context) error {
        return client.Ping(ctx, nil)
    }
}
```

**NATS check:**
```go
func NATSCheck(nc *nats.Conn) CheckFn {
    return func(ctx context.Context) error {
        if !nc.IsConnected() {
            return fmt.Errorf("NATS disconnected")
        }
        return nil
    }
}
```

### Cascade Health Check

The orchestrator depends on NATS (agent connectivity). If NATS is down, the orchestrator
should still be reachable for SSE but will fail on tool dispatch. Readiness check for
orchestrator should check NATS but not fail hard — instead return a degraded status:

```json
{
  "nats": "ok",
  "redis": "ok",
  "llm_provider": "degraded: openrouter timeout, fallback to openai"
}
```

### Router Integration (chi)

```go
// In router/setup.go, add health routes outside auth middleware:
r.Get("/health/live", health.LiveHandler())
r.Get("/health/ready", healthChecker.Handler())
```

**Build order for OneVoice:**

1. Create `pkg/health/checker.go` with `Checker`, `CheckFn`, and standard check factories
2. Add `/health/live` and `/health/ready` to API router (no auth required)
3. Add `/health/ready` to orchestrator (check NATS + Redis)
4. Add `/health/ready` to each agent (check NATS connection status)
5. Wire checks in each `cmd/main.go` after dependency initialization
6. Update docker-compose.yml healthcheck directives to use HTTP endpoints

---

## Structured Logging & Tracing

### Problem Statement

All services use `slog` via `pkg/logger/logger.go`. Logs go to stdout as text. There are no
correlation IDs flowing across service boundaries. When a chat request fails after traversing
API → orchestrator → NATS → agent, there is no way to correlate log lines across services.

### Correlation ID Strategy

Each inbound HTTP request gets a unique correlation ID. It propagates:
- As `X-Correlation-ID` HTTP header (API → orchestrator)
- As `request_id` field in A2A `ToolRequest` (already defined in the protocol: `request_id` field)
- As a structured log field at every log site

**Middleware (already has foundation in chi):**

```go
// middleware/correlation.go
func CorrelationID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := r.Header.Get("X-Correlation-ID")
        if id == "" {
            id = uuid.New().String()
        }
        ctx := context.WithValue(r.Context(), ctxKeyCorrelationID, id)
        w.Header().Set("X-Correlation-ID", id)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func CorrelationIDFromCtx(ctx context.Context) string {
    if id, ok := ctx.Value(ctxKeyCorrelationID).(string); ok {
        return id
    }
    return ""
}
```

**Logger with correlation ID:**

```go
// pkg/logger/logger.go enhancement
func WithCorrelationID(ctx context.Context, base *slog.Logger) *slog.Logger {
    if id := CorrelationIDFromCtx(ctx); id != "" {
        return base.With("correlation_id", id)
    }
    return base
}
```

**JSON format for production:**

```go
func New(service string) *slog.Logger {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    })
    return slog.New(handler).With("service", service)
}
```

This enables log aggregation (ELK, Datadog, Loki) by parsing JSON lines.

### A2A Request Tracing

The `ToolRequest` protocol already has a `request_id` field. Wire it:

1. Orchestrator: set `request_id = correlationID` when creating `ToolRequest`
2. Agent: log `request_id` on every tool execution
3. Agent: include `request_id` in `ToolResponse` for round-trip correlation

This makes it possible to grep across service logs by a single ID:
```
grep '"request_id":"abc-123"' /var/log/onevoice/*.log
```

### Log Levels by Use Case

| Event | Level | Fields |
|-------|-------|--------|
| Request start/end | INFO | method, path, status, duration_ms, correlation_id |
| Tool dispatched | INFO | tool_name, agent_id, request_id, business_id |
| Tool result | INFO | tool_name, success, duration_ms, request_id |
| Token refresh | INFO | platform, business_id, expires_at |
| Session expired (RPA) | WARN | platform, business_id |
| NATS timeout | WARN | subject, timeout_ms, request_id |
| Provider fallback | WARN | from_provider, to_provider, reason |
| Parse error (SSE) | ERROR | line_index, raw_content, correlation_id |
| DB error | ERROR | operation, table, correlation_id |

### Build Order for OneVoice

1. Update `pkg/logger/New()` to use `slog.NewJSONHandler` with `LOG_FORMAT` env toggle
2. Add `middleware/correlation.go` to API and orchestrator chi routers
3. Add correlation ID propagation to orchestrator's HTTP call to agents (via `X-Correlation-ID`)
4. Wire `request_id` in `ToolRequest` from correlation ID
5. Update all `slog.Error/Warn/Info` call sites to include context-derived fields
6. Document log schema for log aggregator ingestion

---

## Graceful Shutdown

### Problem Statement

The API service (`cmd/main.go`) has partial graceful shutdown: it calls `srv.Shutdown()` with
30s timeout after receiving SIGINT/SIGTERM. However:
- NATS connection is closed via `defer nc.Close()` which is not graceful (in-flight messages dropped)
- Agent services (`agent-vk`, `agent-yandex-business`) use `signal.NotifyContext` but do not
  drain NATS subscriptions before exit
- In-flight SSE streams are dropped when the HTTP server shuts down

### Full Graceful Shutdown Sequence

The correct order for a Go microservice with NATS + HTTP:

```
SIGTERM received
    │
    ├── 1. Stop accepting new HTTP requests (server.Shutdown begins)
    │        HTTP server stops accepting; existing connections drain up to timeout
    │
    ├── 2. Stop accepting new NATS messages (unsubscribe)
    │        Existing in-flight NATS handlers complete
    │
    ├── 3. Wait for in-flight work to complete (WaitGroup)
    │
    ├── 4. Flush pending writes (MongoDB message saves, billing logs)
    │
    ├── 5. Close database connections
    │        pgPool.Close(), mongoClient.Disconnect(), redisClient.Close()
    │
    └── 6. Close NATS connection (nc.Drain() not nc.Close())
             NATS Drain: sends remaining outbound messages, then closes
```

### NATS Drain vs Close

`nc.Close()` is immediate and drops in-flight messages. `nc.Drain()` waits for pending
outbound messages and subscriptions to finish. Always use `nc.Drain()` in shutdown:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

<-ctx.Done() // wait for signal

slog.Info("drain NATS connection")
if err := nc.Drain(); err != nil {
    slog.Error("NATS drain error", "error", err)
}
```

### Agent Graceful Shutdown Pattern

The `a2a.Agent.Start(ctx)` blocks until `ctx` is cancelled. After cancellation, the agent
should stop accepting new messages and wait for the current handler to finish.

Current A2A agent (`pkg/a2a/agent.go`) should be updated to:

```go
func (a *Agent) Start(ctx context.Context) error {
    sub, err := a.transport.Subscribe(a.subject(), a.handleMessage)
    if err != nil {
        return err
    }
    <-ctx.Done()
    sub.Drain() // stop new messages, allow current to finish
    a.wg.Wait() // wait for in-flight handlers
    return nil
}
```

### HTTP Server Shutdown (API Service — already partially implemented)

The API's `cmd/main.go` already has:
```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
srv.Shutdown(shutdownCtx)
```

This is correct. SSE streams are long-lived — the 30s timeout may cut them short. For chat
endpoints, a longer timeout (60s) or a separate drain signal to the chat handler is preferable.

**Enhancement:** Track active SSE streams with a WaitGroup:

```go
type chatProxy struct {
    activeStreams sync.WaitGroup
    // ...
}

func (h *chatProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    h.activeStreams.Add(1)
    defer h.activeStreams.Done()
    // ... existing SSE logic
}

// In shutdown:
h.chatProxy.activeStreams.Wait() // drain active chats first
srv.Shutdown(ctx)
```

### Review Syncer Shutdown

`service.NewReviewSyncer` uses `syncCtx` which is cancelled by `defer syncCancel()`. The
syncer's goroutine must respect context cancellation and flush any pending work before returning.

### Build Order for OneVoice

1. Update `pkg/a2a/agent.go`: add `sync.WaitGroup` for in-flight handlers, drain subscription on ctx cancel
2. Update all agent `cmd/main.go`: replace `defer nc.Close()` with post-signal `nc.Drain()`
3. Add `activeStreams sync.WaitGroup` to `ChatProxyHandler`, wait on shutdown
4. Verify `ReviewSyncer` exits cleanly on context cancellation
5. Integration test: send SIGTERM during active SSE stream, verify message is saved to MongoDB

---

## Recommendations for OneVoice

### Priority Order (aligned with PROJECT.md active requirements)

**Phase A — VK/Yandex Agent Hardening (prerequisite for validation)**

1. Implement all four Yandex RPA stub tools (`GetReviews`, `ReplyReview`, `UpdateHours`,
   `UpdateInfo`) using `page.Locator()` selectors
2. Refactor `browser.go` into a `BrowserPool` with shared Playwright instance
3. Add `canaryCheck()` before each RPA action; distinguish `RPAErrorSessionExpired` vs
   `RPAErrorTransient` in `withRetry`
4. Move Yandex cookie storage from env var to API integrations table
5. Add VK `ErrTokenRevoked` detection: if VK API returns error_code 5 (invalid token),
   return permanent failure without retry

**Phase B — Health Checks (prerequisite for production readiness)**

6. Add `pkg/health/` package with standard check factories
7. Add `/health/live` + `/health/ready` to all six services
8. Update docker-compose.yml healthchecks to use HTTP endpoints
9. Add health check to NATS subscription monitoring in orchestrator

**Phase C — Graceful Shutdown (prevents data loss)**

10. Update `pkg/a2a/agent.go` to drain subscriptions on context cancellation
11. Replace `nc.Close()` with `nc.Drain()` in all agent `cmd/main.go` files
12. Add `sync.WaitGroup` to `ChatProxyHandler` for active SSE stream tracking
13. Verify review syncer shuts down cleanly

**Phase D — Structured Logging & Tracing**

14. Update `pkg/logger/New()` to output JSON with `service` field
15. Add `X-Correlation-ID` middleware to API and orchestrator
16. Wire correlation ID into NATS `ToolRequest.request_id`
17. Update high-traffic log sites to include correlation ID

**Phase E — Token Lifecycle (for Yandex long-term reliability)**

18. Implement `OAuthService.RefreshIfNeeded` with PostgreSQL advisory lock
19. Add `ErrTokenExpired` domain error; propagate correctly from token endpoint
20. Add proactive token refresh for Yandex (7-day advance warning in logs)

### Component Boundary Summary

```
pkg/health/         → shared health check primitives (new)
pkg/a2a/agent.go    → add WaitGroup + Drain on shutdown
pkg/logger/         → add JSON handler, CorrelationIDFromCtx helper

services/api/
  internal/middleware/correlation.go  → new: X-Correlation-ID propagation
  internal/service/oauth.go           → add RefreshIfNeeded + advisory lock
  internal/handler/chat_proxy.go      → add activeStreams WaitGroup
  cmd/main.go                         → wire health checks, extend shutdown

services/orchestrator/
  internal/handler/chat.go            → propagate X-Correlation-ID to agents
  cmd/main.go                         → add health check, NATS drain

services/agent-yandex-business/
  internal/yandex/browser.go          → BrowserPool, canaryCheck, RPAError types
  internal/yandex/get_reviews.go      → implement stub
  internal/yandex/reply_review.go     → implement stub
  internal/yandex/update_hours.go     → implement stub
  internal/yandex/update_info.go      → implement stub
  cmd/main.go                         → initialize BrowserPool, NATS drain

services/agent-vk/
  internal/agent/handler.go           → add VK error code detection (permanent vs transient)
  cmd/main.go                         → NATS drain on shutdown
```

### Key Invariants to Preserve

- Agents never hold long-lived tokens — always fetch from API token endpoint per request
- NATS request/reply timeout (30s) must be shorter than HTTP server shutdown timeout (30s)
  to avoid NATS timeout masking clean shutdown
- Health check endpoints must not require auth (bypass JWT middleware)
- Correlation IDs must be generated at the API boundary, not inside the orchestrator,
  so they match what the client sees in `X-Correlation-ID` response headers
- `nc.Drain()` blocks until all pending outbound messages are flushed — ensure it is called
  with a timeout context to avoid hanging shutdown indefinitely
