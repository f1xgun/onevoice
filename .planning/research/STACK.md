# Stack Research

**Date:** 2026-03-15
**Context:** OneVoice — Go 1.24 microservices, Next.js 14, PostgreSQL, MongoDB, NATS, Playwright RPA.
**Scope:** Hardening VK API integration, Yandex.Business RPA reliability, Go microservice observability, JWT/auth security.

---

## VK API Integration Patterns

### Current State

The project already uses `github.com/SevereCloud/vksdk/v3 v3.2.2`, which is the correct choice. The client wrapper in `services/agent-vk/internal/vk/client.go` is minimal: it covers `WallPost`, `GroupsEdit`, and `WallGetComments`. The handler dispatches three tools: `vk__publish_post`, `vk__update_group_info`, `vk__get_comments`.

### Library Recommendation

**Use `github.com/SevereCloud/vksdk/v3` (current, correct choice).**

Rationale:
- Only actively maintained Go VK SDK as of 2025-2026. The v1/v2 branches are frozen. v3 is maintained on a rolling basis.
- Covers the full VK API surface: wall, photos, messages, groups, video, stories.
- Typed request/response structs prevent the untyped `map[string]interface{}` anti-pattern the current code already falls into for `GetComments` return values.
- Has built-in rate limiting awareness and VK error code parsing.

**Do NOT use:**
- Raw HTTP calls to VK API — VK uses non-standard error embedding (errors inside 200 responses). `vksdk` handles this correctly via `api.Error` type.
- `SevereCloud/vksdk/v2` — deprecated, lacks photo upload support and newer API methods.

### Missing VK Tools (What to Add)

The current VK agent only covers 3 of the needed tools. For a working digital presence integration, also implement:

**`vk__publish_photo_post`** — Wall photo + caption post:
```
vk.PhotosSaveWallPhoto → vk.WallPost with "attachments": "photo{ownerID}_{photoID}"
```
This requires a two-step upload: get upload URL via `PhotosGetWallUploadServer`, upload binary to that URL, then call `PhotosSaveWallPhoto` to get the attachment string, then `WallPost`. The current `PublishPost` only handles text.

**`vk__reply_comment`** — Reply to wall comments:
```
vk.WallCreateComment with owner_id + comment_id + message
```

**`vk__delete_wall_post`** — Needed for content moderation:
```
vk.WallDelete with owner_id + post_id
```

### VK Error Handling Pattern

vksdk wraps VK API errors as `*api.Error`. The current code does `if err != nil { return 0, fmt.Errorf("vk wall.post: %w", err) }` which is correct, but callers should also classify errors:

```go
var vkErr *vkapi.Error
if errors.As(err, &vkErr) {
    switch vkErr.Code {
    case 5:  // Auth error — token revoked
        return nil, ErrTokenRevoked  // NonRetryable
    case 9:  // Flood control
        return nil, ErrRateLimited   // Retryable with backoff
    case 15: // Access denied
        return nil, ErrAccessDenied  // NonRetryable
    }
}
```

This classification feeds into retry logic (do not retry `ErrTokenRevoked` or `ErrAccessDenied`).

### VK Rate Limits (2025 Baseline)

- User token: 3 requests/second per token.
- Community token: 20 requests/second.
- Photo upload: counted separately, not rate-limited by API calls.
- If rate limit hit: VK returns error code 9. Do NOT retry immediately — back off 1 second minimum.

The current `withRetry` in the Yandex agent should be ported to the VK client for transient errors, but only after error classification is in place.

### VK Token Types

The project stores a single `access_token` per integration. VK has two relevant token types:

- **User token** — obtained via OAuth. Required for posting as a user, accessing private group data. Expires; needs refresh via OAuth flow.
- **Community token** — obtained from VK admin panel. Does not expire. Required for posting on behalf of a community. Preferred for automation.

**Recommendation:** Store token type alongside the token in the integration record. Community tokens should be preferred for all `wall.post` operations. The current schema stores only `access_token` and `external_id`; add a `token_type` field or use the integration `metadata` JSONB column if one exists.

---

## Playwright RPA Reliability

### Current State

`services/agent-yandex-business/internal/yandex/browser.go` implements three patterns correctly in structure but with gaps in practice:

- `withRetry` — exponential backoff, but retries all errors equally (session expiry retried pointlessly).
- `withPage` — creates a fresh browser instance per call (heavyweight, no pooling).
- `humanDelay` — `rand.Intn(3000)+1000` ms delay (correct approach, but `math/rand` not seeded — always same sequence in older Go; in Go 1.20+ auto-seeded, so acceptable).

All four tool implementations are stubs with `TODO`. The actual RPA work has not been done.

### Playwright Version

**Use `github.com/playwright-community/playwright-go` at the current pinned version.**

As of 2025-2026, this is the only maintained Go Playwright binding. The upstream is actively maintained. The community wrapper tracks Playwright releases with approximately a 2-4 week lag.

**Do NOT:**
- Use `chromedp` for this use case — it controls Chrome DevTools Protocol directly, has no test isolation, and is harder to manage for multi-step form interactions. Playwright's locator API is far more resilient.
- Upgrade Playwright browser binaries without pinning the playwright-go version: the Go wrapper version must match the installed browser version.

### Session Management

**Problem:** The current approach stores cookies as a JSON array in an environment variable (`YANDEX_COOKIES_JSON`). This works but has two failure modes:

1. Cookies expire silently mid-session.
2. Playwright relaunches a fresh browser per tool call (`withPage` calls `playwright.Run()` each time), so session cookies are re-injected fresh but still stale.

**Recommended Pattern — Canary Check:**

Add a session validation step before any RPA action:

```go
func (b *Browser) isSessionValid(page playwright.Page) bool {
    _, err := page.Goto("https://business.yandex.ru/dashboard",
        playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateNetworkidle, Timeout: playwright.Float(15000)})
    if err != nil {
        return false
    }
    // Check for login redirect or auth wall
    url := page.URL()
    return !strings.Contains(url, "passport.yandex.ru") && !strings.Contains(url, "/auth")
}
```

Call this at the start of each `withPage` invocation. If it returns false, return a typed `ErrSessionExpired` immediately without retrying — retrying with expired cookies is pointless.

**Recommended Pattern — Browser Reuse:**

The current `withPage` launches a new browser process on every call. For the Yandex agent (likely called serially, not concurrently), reuse a single browser context across calls within a session:

```go
type Browser struct {
    cookiesJSON string
    pw          *playwright.Playwright  // initialized once at startup
    browser     playwright.Browser       // reused across calls
    mu          sync.Mutex
}
```

Initialize `pw` and `browser` at agent startup in `cmd/main.go`, inject into `Browser`. Each tool call opens a new page from the shared browser context. This eliminates 2-3 seconds of browser launch overhead per call.

Note: Close and reopen the browser context (not the browser process) if `ErrSessionExpired` is returned, after refreshing cookies.

### Selector Resilience

Yandex.Business DOM selectors are the primary fragility. Best practices:

**Use semantic locators, not CSS paths:**

```go
// Fragile — breaks on any DOM restructure
page.Locator("div.content > div:nth-child(3) > button")

// Resilient — targets visible text and ARIA roles
page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Ответить"})
page.GetByText("Написать ответ")
page.GetByLabel("Текст ответа")
```

Priority order (most to least resilient):
1. `GetByRole` with `Name` — survives CSS changes, survives class renames.
2. `GetByLabel` — survives layout changes, requires accessible forms.
3. `GetByText` — survives class changes, breaks if text changes.
4. `GetByTestId` — ideal, but requires Yandex to add `data-testid` (not possible for RPA).
5. `Locator("css=...")` — last resort, most fragile.

**Use `page.Locator().WaitFor()` with explicit timeout instead of implicit waits:**

```go
replyBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Ответить"})
if err := replyBtn.WaitFor(playwright.LocatorWaitForOptions{
    State:   playwright.WaitForSelectorStateVisible,
    Timeout: playwright.Float(10000),
}); err != nil {
    return fmt.Errorf("reply button not found (DOM may have changed): %w", err)
}
```

Never use `page.WaitForTimeout()` (hardcoded sleep). Use `WaitForSelector` or `WaitFor` on locators.

### Retry Differentiation

**Critical gap in the current `withRetry`:** All errors are retried equally. Define error types:

```go
// NonRetryableError signals that retrying will not help.
type NonRetryableError struct {
    Cause error
}
func (e *NonRetryableError) Error() string { return e.Cause.Error() }
func (e *NonRetryableError) Unwrap() error { return e.Cause }

// In withRetry:
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error {
    var lastErr error
    for i := range maxAttempts {
        if err := ctx.Err(); err != nil {
            return err
        }
        lastErr = fn()
        if lastErr == nil {
            return nil
        }
        var nonRetryable *NonRetryableError
        if errors.As(lastErr, &nonRetryable) {
            return lastErr  // do not retry
        }
        if i < maxAttempts-1 {
            time.Sleep(time.Duration(1<<uint(i)) * time.Second)
        }
    }
    return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
```

Wrap `ErrSessionExpired`, selector-not-found after timeout, and invalid input errors in `NonRetryableError`. Wrap network errors and `WaitForSelector` timeouts (which may be transient page load issues) as plain errors that will be retried.

### Screenshot Diagnostics

The current `withPage` saves a screenshot to `/tmp/yandex_error_*.png` on error — correct approach. Improve by also saving the page HTML source:

```go
html, _ := page.Content()
_ = os.WriteFile(fmt.Sprintf("/tmp/yandex_error_%d.html", time.Now().UnixMilli()), []byte(html), 0o600)
```

HTML dump is more useful than screenshots for diagnosing selector failures.

### What NOT to Use

- **Do NOT use `WaitForNavigation`** — it is deprecated in Playwright Go and prone to race conditions. Use `WaitUntil: playwright.WaitUntilStateNetworkidle` in `Goto` options instead.
- **Do NOT use `page.Fill(selector, value)`** with bare CSS selectors for production code — prefer `page.GetByLabel(...).Fill(value)`.
- **Do NOT disable JavaScript** — Yandex.Business is a fully dynamic SPA; JS must be enabled.

---

## Go Microservice Observability

### Current State

All services use `log/slog` (Go standard library structured logging) via `pkg/logger`. No Prometheus metrics, no health check endpoints, no correlation IDs crossing service boundaries.

### Health Checks

**Pattern:** Add `/health` and `/ready` to each service using chi.

```go
// Liveness: service process is alive
r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write([]byte(`{"status":"ok"}`))
})

// Readiness: dependencies are available
r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
    if err := pgPool.Ping(r.Context()); err != nil {
        http.Error(w, `{"status":"not ready","reason":"postgres"}`, http.StatusServiceUnavailable)
        return
    }
    if err := redisClient.Ping(r.Context()).Err(); err != nil {
        http.Error(w, `{"status":"not ready","reason":"redis"}`, http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write([]byte(`{"status":"ok"}`))
})
```

Separate liveness from readiness: Kubernetes restarts on failed liveness; stops routing to pod on failed readiness. Use readiness for DB/Redis checks.

The API service already has an internal server (`internalAddr`). Mount health/ready there, not on the public port, to avoid leaking dependency status externally.

### Metrics

**Library: `github.com/prometheus/client_golang v1.22.x` (latest stable as of 2026-03)**

This is the only production-grade Prometheus client for Go. Do not use alternatives.

**Minimal set of metrics for OneVoice:**

```go
// HTTP request metrics (add as chi middleware)
httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "http_request_duration_seconds",
    Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
}, []string{"method", "path", "status"})

httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "http_requests_total",
}, []string{"method", "path", "status"})

// LLM-specific (orchestrator)
llmCallDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "llm_call_duration_seconds",
    Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
}, []string{"provider", "model"})

llmTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "llm_tokens_total",
}, []string{"provider", "model", "type"}) // type: prompt|completion

// Tool dispatch (orchestrator + agents)
toolCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "tool_calls_total",
}, []string{"tool", "status"}) // status: success|error

// NATS queue (agents)
natsQueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
    Name: "nats_queue_depth",
}, []string{"subject"})
```

Expose on `/metrics` endpoint (standard Prometheus scrape target), separate from public API — mount on the internal port, or on a dedicated metrics port (9090 by convention).

**Do NOT use:**

- `github.com/go-chi/chi/middleware` built-in logging as a metrics substitute — it only logs, not counts.
- OpenTelemetry metrics for this system at this stage — OTEL adds significant complexity (collector, OTLP pipeline) with no benefit over direct Prometheus for a single-team service.

### Structured Logging

The project already uses `log/slog` (correct choice — standard library, no external dependency). The gap is correlation IDs and consistent field naming.

**Correlation ID middleware:**

```go
func CorrelationID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := r.Header.Get("X-Correlation-ID")
        if id == "" {
            id = uuid.New().String()
        }
        w.Header().Set("X-Correlation-ID", id)
        ctx := context.WithValue(r.Context(), correlationIDKey, id)
        // Attach to slog default logger for this request
        logger := slog.Default().With("correlation_id", id)
        ctx = context.WithValue(ctx, loggerKey, logger)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Pass the correlation ID in NATS message headers when dispatching tool calls to agents, so agent logs can be correlated back to the originating chat request.

**JSON log output for production:**

The current `pkg/logger` uses the default slog text handler. Switch to JSON for production:

```go
func New(service string) *slog.Logger {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    })
    return slog.New(handler).With("service", service)
}
```

JSON to stdout is the standard 12-factor pattern. Log collectors (Loki, ELK, Datadog) parse JSON natively.

**Do NOT use:**

- `github.com/sirupsen/logrus` — archived, unmaintained since 2022. `slog` supersedes it.
- `go.uber.org/zap` — excellent library but unnecessary complexity now that stdlib has `slog`. Migration from slog to zap adds zero capability for this use case.
- `github.com/rs/zerolog` — same reasoning as zap.

### Distributed Tracing (Optional, Post-Hardening)

For diploma defense: skip. For production: add OpenTelemetry tracing with a Jaeger backend. The minimal setup is:

- `go.opentelemetry.io/otel v1.x` (SDK)
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` (OTLP exporter)
- Wrap NATS publish/subscribe with span context propagation

Do not add tracing until health checks + Prometheus are operational — tracing adds overhead and complexity that should come after baseline observability.

---

## JWT/Auth Security Hardening

### Current State

The auth implementation is architecturally sound:
- Access tokens: 15-minute expiry (correct)
- Refresh tokens: 7-day expiry, stored in Redis, rotated on each use (correct)
- bcrypt with timing-safe dummy hash comparison for non-existent users (correct)
- JWT signed with HS256 using a ≥32-byte secret (enforced at startup)

Two hardening gaps exist: token transport (localStorage) and type-unsafe claim extraction in middleware.

### Refresh Token Transport: httpOnly Cookie

**The most important security fix.** Refresh tokens stored in `localStorage` are accessible to any JavaScript on the page.

**Backend change:** On login and token refresh, set the refresh token as an httpOnly cookie instead of returning it in the JSON body:

```go
http.SetCookie(w, &http.Cookie{
    Name:     "refresh_token",
    Value:    refreshToken,
    Path:     "/api/v1/auth",      // Scope to auth endpoints only
    HttpOnly: true,
    Secure:   true,                 // HTTPS only
    SameSite: http.SameSiteStrictMode,
    MaxAge:   int(7 * 24 * time.Hour / time.Second),
})
```

**Frontend change:** Remove `localStorage.setItem("refreshToken", ...)` and `localStorage.getItem("refreshToken")`. The browser sends the cookie automatically on requests to `/api/v1/auth/*`. The refresh endpoint reads it from `r.Cookie("refresh_token")`.

**Backend logout change:** On logout, clear the cookie:

```go
http.SetCookie(w, &http.Cookie{
    Name:     "refresh_token",
    Path:     "/api/v1/auth",
    HttpOnly: true,
    Secure:   true,
    SameSite: http.SameSiteStrictMode,
    MaxAge:   -1,  // Delete
})
```

`SameSite: Strict` prevents CSRF on the refresh endpoint. The access token (short-lived, 15 min) can remain in memory on the frontend (not localStorage) — no persistent XSS risk.

### JWT Claims: Use Typed Struct in Middleware

The auth middleware uses `jwt.MapClaims` with manual type assertions. Replace with typed struct (already done correctly in the service layer, but not in the middleware):

```go
// In middleware/auth.go
type accessTokenClaims struct {
    UserID string `json:"user_id"`
    Email  string `json:"email"`
    Role   string `json:"role"`
    jwt.RegisteredClaims
}

token, err := jwt.ParseWithClaims(tokenString, &accessTokenClaims{},
    func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return jwtSecret, nil
    },
    jwt.WithExpirationRequired(),
    jwt.WithIssuedAt(),
)
```

`jwt.WithExpirationRequired()` and `jwt.WithIssuedAt()` are validation options added in `golang-jwt/jwt/v5` — use them explicitly. They are not enabled by default.

The project already uses `golang-jwt/jwt/v5 v5.3.1` — correct, this is the current maintained fork. Do NOT switch to `dgrijalva/jwt-go` (archived, CVEs) or `golang-jwt/jwt/v4` (superseded).

### Algorithm Confusion Prevention

The middleware already checks `token.Method.(*jwt.SigningMethodHMAC)` before accepting the token — this prevents the `alg: none` attack and RS256→HS256 confusion. Keep this check. Do not remove it for brevity.

### Issuer and Audience Validation

Currently missing. Add `iss` and `aud` claims:

```go
// In generateAccessToken:
claims := &AccessTokenClaims{
    RegisteredClaims: jwt.RegisteredClaims{
        Issuer:   "onevoice-api",
        Audience: jwt.ClaimStrings{"onevoice-frontend"},
        ExpiresAt: ...,
        IssuedAt:  ...,
    },
    ...
}

// In middleware validation:
jwt.WithAudience("onevoice-frontend"),
jwt.WithIssuer("onevoice-api"),
```

This prevents tokens issued by other services from being accepted (relevant if the system ever has multiple JWT issuers, e.g., a separate auth service).

### JWT Secret Rotation Strategy

Current: single `JWT_SECRET` environment variable. If leaked, all active sessions are valid until expiry.

**Minimal rotation approach (no Vault required):**

Support two secrets: `JWT_SECRET_CURRENT` and `JWT_SECRET_PREVIOUS`. On token validation, try current first, then previous. On token generation, always use current. Rotate by moving current → previous and setting a new current. Old tokens remain valid until they expire (max 15 minutes for access tokens). Refresh tokens in Redis are invalidated on rotation if needed (delete all `onevoice:auth:refresh_token:*` keys).

This is sufficient for diploma defense and initial production. For long-term: use AWS Secrets Manager or HashiCorp Vault with automatic rotation.

### Content Security Policy

Add CSP headers to the API service response to limit XSS blast radius. Add as a chi middleware:

```go
func SecurityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Security-Policy",
            "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'")
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
        next.ServeHTTP(w, r)
    })
}
```

This limits damage from any XSS vulnerability by preventing external script execution, even if the access token is somehow reachable in memory.

### CSRF Protection

With `SameSite: Strict` on the refresh token cookie, CSRF is largely mitigated for the cookie-based flow. For the access token (in-memory), no CSRF protection is needed because CSRF attacks cannot read JavaScript variables from a different origin.

If any future form uses session cookies (not Bearer tokens), add `github.com/gorilla/csrf` or implement the double-submit cookie pattern. At present this is not needed.

### Rate Limiting on Auth Endpoints

The current `ratelimit.go` middleware exists but is not applied to `/api/v1/auth/login`. Login brute force is a real threat. Apply rate limiting:

- `/auth/login`: 10 requests per IP per minute
- `/auth/refresh`: 30 requests per IP per minute (automated refresh is expected)
- `/auth/register`: 5 requests per IP per minute

The existing Redis-based rate limiter can handle this — it's already imported. Apply the middleware selectively to auth routes in `router.go`.

### What NOT to Use

- **Do NOT switch to RS256/ES256** for the current architecture — asymmetric signatures add key management complexity with no benefit when the verifier (middleware) and signer (service) are the same process. Use RS256 only if external services need to verify tokens without sharing the secret.
- **Do NOT use `github.com/form3tech-oss/jwt-go`** — abandoned.
- **Do NOT store any token material in cookie without `HttpOnly` and `Secure` flags** — both must be set together.
- **Do NOT use `SameSite: Lax`** for the refresh token cookie — Strict is required since the cookie carries long-lived credentials and there is no legitimate cross-site navigation that needs to carry it.

---

## Recommendations

### Priority 1 — Security (Do Before Any Public Use)

1. Move refresh token to httpOnly + Secure + SameSite=Strict cookie. This is the most impactful security fix; current localStorage storage is a significant vulnerability.
2. Add typed JWT claims struct in `middleware/auth.go` — replace `jwt.MapClaims` with `*accessTokenClaims` and add `jwt.WithExpirationRequired()`.
3. Apply rate limiting to `/auth/login` and `/auth/register` using the existing Redis rate limiter.
4. Add CSP + security headers middleware to the API service.

### Priority 2 — Observability (Do Before Production)

5. Add `/health` (liveness) and `/ready` (readiness) endpoints to all services. Mount on the internal port.
6. Add Prometheus metrics using `prometheus/client_golang v1.22.x`. Start with HTTP request duration histogram and error counter — 20 lines of code, high ROI.
7. Switch `pkg/logger` to JSON output (`slog.NewJSONHandler`) for production deployments.
8. Add correlation ID middleware and pass the ID through NATS message headers.

### Priority 3 — VK Integration (Do to Validate the Integration)

9. Add VK error classification (`vkErr.Code` switch) before retry logic. This is a prerequisite for any retry pattern to be useful.
10. Implement `vk__publish_photo_post` using the two-step `PhotosSaveWallPhoto` + `WallPost` flow. Text-only posts are insufficient for digital presence management.
11. Add VK-specific `withRetry` to the VK client that skips retry on auth errors (code 5) and access errors (code 15).

### Priority 4 — Yandex RPA (Do to Make Stubs Functional)

12. Implement session canary check before every RPA action. Without this, expired cookie failures are indistinguishable from DOM change failures.
13. Add `NonRetryableError` type to `withRetry` and wrap session-expired and selector-not-found errors in it.
14. Reuse a single browser process (not per-call `playwright.Run()`) to eliminate 2-3 second overhead per tool call.
15. Implement the four stub functions using semantic `GetByRole`/`GetByText` locators, not CSS paths. Start with `GetReviews` as it is read-only and lower risk.

### What to Defer

- OpenTelemetry distributed tracing — add after Prometheus metrics are operational and stable.
- HashiCorp Vault / AWS Secrets Manager — add when moving to multi-instance deployment. The two-secret rotation pattern described above is sufficient for single-node.
- RS256 JWT signing — not needed unless external token consumers are added.
- `axios-retry` on the frontend — lower priority than backend security fixes.
