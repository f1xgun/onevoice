# OneVoice Codebase — Technical Debt & Concerns

**Date:** 2026-03-15
**Scope:** Architecture, security, performance, code quality, and operational risks

---

## Table of Contents

1. [Critical Issues](#critical-issues)
2. [Security Concerns](#security-concerns)
3. [Performance & Scaling](#performance--scaling)
4. [Code Quality](#code-quality)
5. [Testing & Coverage](#testing--coverage)
6. [Frontend Issues](#frontend-issues)
7. [Operational & DevOps](#operational--devops)
8. [Dependencies](#dependencies)

---

## Critical Issues

### 1. Incomplete Yandex.Business RPA Implementation

**Severity:** HIGH
**Location:**
- `services/agent-yandex-business/internal/yandex/get_reviews.go:22`
- `services/agent-yandex-business/internal/yandex/update_hours.go:28`
- `services/agent-yandex-business/internal/yandex/update_info.go:21`
- `services/agent-yandex-business/internal/yandex/reply_review.go:21`

**Description:**
Four critical tool functions return stub implementations with `TODO` comments:
- `GetReviews()` returns empty array (line 24)
- `UpdateHours()` returns error "not yet implemented" (line 30)
- `UpdateInfo()` has no implementation
- `ReplyReview()` has no implementation

These tools will fail when called by the LLM orchestrator, providing poor user experience and potential data loss (hours/info updates will silently fail).

**Suggested Fix:**
1. Implement DOM selectors via `page.Locator()` for Yandex.Business forms
2. Add validation for input data (hours format, info fields)
3. Test with real Yandex.Business sandbox account
4. Add unit tests with mocked Playwright pages
5. Document selector maintenance requirements (Yandex may change DOM)

---

### 2. Stale Cookies Not Detected in Yandex RPA

**Severity:** HIGH
**Location:** `services/agent-yandex-business/internal/yandex/browser.go:54-56`

**Description:**
The `setCookies()` function silently discards missing/invalid cookie fields (via underscore assignments). If cookies expire or become invalid during a multi-step operation, the code will fail mid-action without clear diagnostics. The retry logic in `withRetry()` will mask the real issue (expired session) as a transient error.

**Suggested Fix:**
1. Add validation of critical cookie fields before setting (e.g., `expires`, `session_id`)
2. Implement a "canary request" at page load to verify session is valid (e.g., navigate to profile page, check for login button)
3. Return specific error types for session expiry vs. RPA failures
4. Log cookie metadata (count, domain coverage) on startup for debugging

---

### 3. Missing Error Context in Chat Proxy Streaming

**Severity:** MEDIUM
**Location:** `services/api/internal/handler/chat_proxy.go:211-212`

**Description:**
JSON parsing errors in the SSE stream are silently skipped (`continue` on line 212):
```go
if err := json.Unmarshal([]byte(line[6:]), &ev); err != nil {
    continue
}
```

This causes:
- Tool call results to be lost without logging
- Tool call tracking (toolCallIDByName) to become corrupted
- Subsequent tool results mapped to wrong tool calls
- No diagnostic visibility when orchestrator sends malformed SSE

**Suggested Fix:**
1. Log parsing errors with context: line content, index in stream
2. Add metrics counter for malformed SSE events
3. Return partial results with error instead of silently dropping them
4. Validate SSE format before processing (must have "data: " prefix)

---

## Security Concerns

### 1. JWT Claims Not Type-Safe

**Severity:** MEDIUM
**Location:** `services/api/internal/middleware/auth.go:59-90`

**Description:**
JWT claims are extracted as `jwt.MapClaims` then type-asserted to strings without nil checks:
```go
userIDStr, ok := claims["user_id"].(string)
if !ok {
    writeJSONError(w, http.StatusUnauthorized, "invalid token claims: missing user_id")
    return
}
```

While checks exist, this pattern is fragile. A malicious JWT with wrong types (e.g., `user_id: 123` instead of string) could bypass checks if assertion logic changes.

**Suggested Fix:**
1. Use a typed JWT claims struct:
   ```go
   type CustomClaims struct {
       UserID string `json:"user_id"`
       Email  string `json:"email"`
       Role   string `json:"role"`
       jwt.RegisteredClaims
   }
   ```
2. Replace `jwt.Parse()` with `jwt.ParseWithClaims()`
3. Validate claim presence at token creation time (in user service)

---

### 2. Refresh Token Stored in Browser LocalStorage

**Severity:** MEDIUM
**Location:**
- `services/frontend/lib/auth.ts:28, 39`
- `services/frontend/lib/api.ts:52, 68`

**Description:**
Refresh tokens are stored in browser LocalStorage, which is vulnerable to XSS attacks. Any inline script can access `localStorage['refreshToken']` and steal it.

**Risk:** If an attacker injects JavaScript via:
- Markdown rendering in chat (if user content is rendered unsanitized)
- Compromised dependency
- CSP bypass

They can impersonate the user indefinitely.

**Suggested Fix:**
1. Store refresh token in HttpOnly, Secure cookie instead (requires backend changes)
2. Add Content Security Policy headers:
   ```
   Content-Security-Policy: script-src 'self'; object-src 'none'; base-uri 'self'
   ```
3. Sanitize all user-generated content (especially markdown in chat)
4. Implement token binding (IP-based or device-based rotation)

---

### 3. OAuth State Validation Only Checks Existence

**Severity:** LOW
**Location:** `services/api/internal/service/oauth.go:51-67`

**Description:**
OAuth state is validated only by Redis key existence (`GetDel`). There's no verification of:
- State format consistency
- State age (timing attack mitigation)
- Cross-origin usage

If Redis is cleared or state key expires before callback, error message reveals nothing about why it failed.

**Suggested Fix:**
1. Add timing window validation: ensure state was generated within last 10 minutes
2. Include timestamp + hash in state generation for debugging
3. Log all state validation failures (not just success path)
4. Return generic "invalid state" error to client, log details server-side

---

### 4. Cookies JSON Parsing Doesn't Validate Domain

**Severity:** MEDIUM
**Location:** `services/agent-yandex-business/internal/yandex/browser.go:71-91`

**Description:**
When parsing session cookies from environment variable, the code accepts any domain without validation:
```go
domain, _ := c["domain"].(string)
pwCookies = append(pwCookies, playwright.OptionalCookie{
    Domain: playwright.String(domain),
    ...
})
```

An attacker with environment variable access could inject cookies for unrelated domains (e.g., gmail.com) into the Playwright context.

**Suggested Fix:**
1. Whitelist allowed domains: only accept `"business.yandex.ru"` or similar
2. Validate `expires` field and reject expired cookies at load time
3. Add length checks on cookie values (detect injection attempts)
4. Log cookie metadata on startup for audit trail

---

### 5. No Rate Limiting on Chat Endpoint

**Severity:** MEDIUM
**Location:** `services/api/internal/handler/chat_proxy.go:83` (no rate limit middleware applied)

**Description:**
The `/chat/{conversationID}` endpoint is not protected by rate limiting. An authenticated user can:
- Spam orchestrator with requests → memory exhaustion
- Trigger expensive LLM calls → billing attack
- Stress MongoDB with message writes

**Suggested Fix:**
1. Apply rate limiting middleware to chat endpoint (e.g., 5 requests/min per user)
2. Use Redis-based sliding window counter (already available in pool)
3. Add cost-based limiting: weight LLM calls by model (GPT-4 = higher weight)
4. Log rate limit violations for abuse detection

---

## Performance & Scaling

### 1. N+1 Query Risk in Integration Loading

**Severity:** MEDIUM
**Location:** `services/api/internal/handler/chat_proxy.go:113-127`

**Description:**
In `Chat()` handler:
```go
integrations, err := h.integrationService.ListByBusinessID(r.Context(), business.ID)
activeIntegrations := make([]string, 0)
seen := make(map[string]bool)
for _, integ := range integrations {
    if integ.Status == "active" && !seen[integ.Platform] {
        activeIntegrations = append(activeIntegrations, integ.Platform)
        seen[integ.Platform] = true
    }
}
```

This loads **all** integrations for a business every chat request. If a business has 100 integrations, this is wasteful. Better to cache active integrations or query only active ones.

**Suggested Fix:**
1. Add `ListActiveByBusinessID()` method to IntegrationRepository
2. Query: `SELECT DISTINCT platform FROM integrations WHERE business_id = $1 AND status = 'active'`
3. Cache result in Redis (TTL: 5 min) since integrations change infrequently
4. Invalidate cache on integration status changes

---

### 2. Missing Index on Refresh Token Hash

**Severity:** LOW
**Location:** `migrations/postgres/000001_init.up.sql:99`

**Description:**
Index exists on `token_hash`, but refresh token lookup by user is common. The index doesn't support range queries by `user_id + created_at` for cleanup operations.

**Suggested Fix:**
1. Add composite index: `CREATE INDEX idx_refresh_tokens_user_created ON refresh_tokens(user_id, created_at)`
2. Add automated cleanup: delete refresh tokens older than 90 days (cron job or trigger)
3. Monitor refresh_tokens table size growth

---

### 3. Message Repository Loads Full Message Content on List

**Severity:** LOW
**Location:** `services/api/internal/handler/chat_proxy.go:460-461`

**Description:**
`loadHistory()` loads 100 full message records including `Content` (which could be long), then filters to user/assistant only. This wastes bandwidth for system messages or tool calls.

**Suggested Fix:**
1. Add `ListByConversationIDSummary()` that projects only `role` and first 1000 chars of `content`
2. Load full content only when needed (e.g., tool call panel)
3. Implement pagination with cursor-based loading for large conversations

---

### 4. SSE Response Buffer Size Not Adaptive

**Severity:** LOW
**Location:** `services/api/internal/handler/chat_proxy.go:187`

**Description:**
Buffer is hardcoded to 64KB:
```go
scanner.Buffer(make([]byte, 64*1024), 64*1024)
```

If orchestrator sends a tool call with very large JSON arguments, it will fail. If response is small, memory is wasted.

**Suggested Fix:**
1. Make buffer size configurable via environment variable
2. Add metrics: measure actual buffer usage, alert if >80% full
3. Increase default to 256KB to handle larger tool arguments

---

## Code Quality

### 1. Panic in Production Code

**Severity:** MEDIUM
**Location:**
- `services/api/internal/handler/auth.go:30` (panic on nil userService)
- `services/api/internal/handler/business.go:40` (panic on nil businessService)
- `services/api/internal/handler/integration.go:41, 47` (panic on nil services)
- `services/api/internal/handler/agent_task.go:39`
- `services/api/internal/service/user.go` (panic on JWT secret < 32 bytes)

**Description:**
Multiple handlers use `panic()` for nil checks instead of returning errors:
```go
func NewAuthHandler(userService UserService, ...) *AuthHandler {
    if userService == nil {
        panic("userService cannot be nil")
    }
    ...
}
```

While these are caught in tests, panics in production crash the HTTP server request handler, taking down all concurrent requests.

**Suggested Fix:**
1. Replace panics with error returns: `NewAuthHandler(...) (*AuthHandler, error)`
2. Propagate errors to main.go (fail gracefully on startup if required service is nil)
3. Use linter rule to forbid `panic()` outside of main init

---

### 2. Silent Errors in Repository Methods

**Severity:** MEDIUM
**Location:** `services/api/internal/handler/chat_proxy.go:139-140, 252-254`

**Description:**
When saving messages fails, errors are logged but execution continues:
```go
if err := h.messageRepo.Create(r.Context(), userMsg); err != nil {
    slog.Error("failed to save user message", "error", err)
}
```

This means:
- User message is not persisted but user thinks it was sent
- Conversation history is incomplete
- Tool call results may be missing (line 252)

**Suggested Fix:**
1. Return error to client if message save fails
2. Implement transaction-like behavior: save message before proxying to orchestrator
3. Add circuit breaker: if MongoDB is down, fail fast instead of degrading silently

---

### 3. No Input Validation on Tool Arguments

**Severity:** MEDIUM
**Location:** `services/orchestrator/internal/tools/registry.go:71-80`

**Description:**
Tool arguments are passed directly to executors without validation:
```go
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
    e, ok := r.tools[name]
    if !ok {
        return nil, fmt.Errorf("unknown tool: %q", name)
    }
    if e.executor == nil {
        return map[string]interface{}{"status": "stub", "tool": name}, nil
    }
    return e.executor.Execute(ctx, args)  // No validation!
}
```

LLM can pass invalid/malicious arguments:
- `channel_id: "'; DROP TABLE channels; --"`
- `text: "x" * 10000000` (memory exhaustion)
- Missing required fields

**Suggested Fix:**
1. Add `Validate()` method to `ToolDefinition` that validates args schema
2. Validate all arguments before calling executor
3. Add `maxLength` for string fields in tool definitions
4. Use zod schemas for arg validation in Go (or json-schema)

---

### 4. Retry Logic Doesn't Differentiate Error Types

**Severity:** MEDIUM
**Location:** `services/agent-yandex-business/internal/yandex/browser.go:99-114`

**Description:**
`withRetry()` retries all errors equally:
```go
for i := range maxAttempts {
    lastErr = fn()
    if lastErr == nil {
        return nil
    }
    if i < maxAttempts-1 {
        time.Sleep(time.Duration(1<<uint(i)) * time.Second)
    }
}
```

Some errors should not be retried:
- Session expired (needs new cookies, retrying won't help)
- Invalid selector (DOM changed, need manual investigation)
- Network error (should retry)

**Suggested Fix:**
1. Define `NonRetryableError` type for permanent failures
2. Check error type before sleeping/retrying
3. Log retry attempt with error type for debugging
4. Add metrics: track retry counts by error type

---

### 5. Missing Nil Checks in Type Assertions

**Severity:** LOW
**Location:** `services/agent-telegram/internal/agent/handler.go:72-73`

**Description:**
Type assertions with underscore discards assume fields exist:
```go
text, _ := req.Args["text"].(string)
channelIDStr, _ := req.Args["channel_id"].(string)
```

If LLM generates tool call without `text` field, it silently becomes empty string instead of error.

**Suggested Fix:**
1. Validate required fields explicitly:
   ```go
   text, ok := req.Args["text"].(string)
   if !ok {
       return nil, fmt.Errorf("missing required field: text")
   }
   ```
2. Use schema validation at tool execution level

---

## Testing & Coverage

### 1. Missing Tests for Streaming Error Cases

**Severity:** MEDIUM
**Location:** `services/api/internal/handler/chat_proxy.go`

**Description:**
No tests for:
- Orchestrator network errors mid-stream
- Malformed SSE events
- MongoDB save failures
- Concurrent tool calls with race conditions

Current tests likely cover happy path only.

**Suggested Fix:**
1. Add test mocks for orchestrator HTTP errors
2. Test partial streaming (send N events, then error)
3. Mock MongoDB to return errors, verify graceful degradation
4. Test concurrent chat requests to same conversation

---

### 2. No Integration Tests for Cookie-Based Auth

**Severity:** MEDIUM
**Location:** `services/agent-yandex-business/internal/yandex/browser.go`

**Description:**
`setCookies()` is unit-tested but not integrated with real Playwright instance. Cookie loading could fail in production due to:
- Cookie format changes
- Domain mismatches
- Playwright API version incompatibility

**Suggested Fix:**
1. Add integration test with headless Chromium (run in CI)
2. Use test cookies or mock Playwright for faster tests
3. Document cookie format requirements and version compatibility

---

### 3. Insufficient Rate Limiter Tests

**Severity:** LOW
**Location:** Tests for `services/api/internal/middleware/ratelimit.go` likely incomplete

**Description:**
Rate limiting should be tested for:
- Burst requests (all at once)
- Distributed requests (across multiple IPs via proxy)
- Clock skew / time drift issues

**Suggested Fix:**
1. Add tests for concurrent requests exceeding limit
2. Test with mocked Redis time (use `miniredis` for local testing)
3. Add test for key expiration cleanup

---

## Frontend Issues

### 1. React Markdown Not Sanitized

**Severity:** MEDIUM
**Location:** `services/frontend/package.json` (react-markdown as dependency)

**Description:**
If chat messages are rendered with `react-markdown`, user-controlled content could include malicious HTML/scripts unless sanitized. While react-markdown escapes by default, custom renderers could be exploited.

**Suggested Fix:**
1. Add `remark-gfm` plugin for safe GitHub-flavored markdown
2. Add `rehype-sanitize` plugin to strip dangerous HTML
3. Render chat content as: `<Markdown rehypePlugins={[rehypeSanitize]}>{message}</Markdown>`
4. Add CSP headers to prevent inline scripts

---

### 2. No CSRF Protection on Forms

**Severity:** MEDIUM
**Location:** All forms in `services/frontend/app/(app)/`

**Description:**
Forms send requests to `/api/v1/` endpoints without CSRF tokens. If a user visits a malicious site while logged in, the attacker can:
- Create integrations
- Delete businesses
- Send posts via LLM

**Suggested Fix:**
1. Backend: Generate CSRF token in session, validate on POST/PUT/DELETE
2. Frontend: Include token in axios interceptor
3. Use SameSite=Strict cookie flag (already in NextAuth if used)

---

### 3. Missing Loading States and Error Boundaries

**Severity:** LOW
**Location:** Chat page, integrations page, etc.

**Description:**
If API request fails or takes long, UI doesn't indicate:
- Loading state (spinner)
- Error state (user sees stale data)
- Retry option

**Suggested Fix:**
1. Add React Query `useQuery()` with error/loading states
2. Add error boundary component for unexpected errors
3. Show toast notifications for API errors
4. Add retry button on error state

---

### 4. Axios Interceptor Doesn't Handle Network Errors

**Severity:** LOW
**Location:** `services/frontend/lib/api.ts:24-86`

**Description:**
If network is down (no response from server), axios catches it but only in catch block of `api.interceptors.response.use()`. This is correct, but there's no exponential backoff, no retry for safe methods (GET).

**Suggested Fix:**
1. Add retry logic: `axios-retry` plugin with exponential backoff
2. Retry only on 5xx and network errors, not 4xx
3. Add max retries: 3 for safe methods, 1 for unsafe

---

## Operational & DevOps

### 1. No Health Check Endpoints

**Severity:** MEDIUM
**Location:** All services

**Description:**
Kubernetes / load balancers need health checks to detect:
- Service hung (all responses slow)
- Database connection pool exhausted
- NATS connection lost

Without health checks:
- Requests route to dead services indefinitely
- Cascading failures (one service down → others affected)

**Suggested Fix:**
1. Add `/health` endpoint to each service
2. Check:
   - PostgreSQL connection pool: `PING` to test connection
   - MongoDB connection: `PING` to test connection
   - NATS connection: verify subscribed topics
   - Redis connection: `PING` to test
3. Return `503 Service Unavailable` if any check fails

---

### 2. No Metrics/Monitoring

**Severity:** MEDIUM
**Location:** All services

**Description:**
No Prometheus metrics for:
- Request latency (p50, p95, p99)
- Error rates by endpoint
- Active connections
- Queue depths (NATS)
- Database pool exhaustion

Incidents are invisible until users complain.

**Suggested Fix:**
1. Add Prometheus metrics to chi router (middleware)
2. Export metrics on `/metrics` endpoint
3. Track:
   - `http_request_duration_seconds` (histogram)
   - `http_requests_total` (counter by status)
   - `db_pool_connections` (gauge)
   - `nats_queue_depth` (gauge)
4. Set up alerts: p99 latency > 5s, error rate > 1%

---

### 3. No Structured Logging for ELK/Datadog

**Severity:** LOW
**Location:** All services use `slog` but not aggregated

**Description:**
Logs are printed to stdout as text. Without a log aggregator:
- Searching logs across services is impossible
- Correlation IDs don't flow through request chain
- Error traces are scattered across multiple log files

**Suggested Fix:**
1. Add correlation ID to all requests (middleware)
2. Implement JSON log formatter for `slog` (structured output)
3. Add ELK stack or Datadog agent to collect logs
4. Tag logs with service name, version, environment

---

### 4. Secrets Not Rotated

**Severity:** MEDIUM
**Location:** All services

**Description:**
- JWT_SECRET: if leaked, all tokens become invalid
- ENCRYPTION_KEY: if leaked, all encrypted tokens compromised
- Database passwords: if leaked, database exposed
- OAuth client secrets: if leaked, third-party integrations compromised

No rotation policy defined.

**Suggested Fix:**
1. Define secret rotation schedule: JWT_SECRET every 3 months
2. Support multiple keys in rotation: old key still valid for 24h after rotation
3. Use secrets manager (AWS Secrets Manager, HashiCorp Vault)
4. Audit secret access logs
5. Alert on secret rotation failures

---

### 5. No Graceful Shutdown

**Severity:** LOW
**Location:** `services/api/cmd/main.go:43-46`

**Description:**
Services exit immediately on signal. In-flight requests are dropped:
```go
if err := run(log, cfg); err != nil {
    log.Error("application error", "error", err)
    os.Exit(1)
}
```

**Suggested Fix:**
1. Implement signal handler (SIGTERM, SIGINT)
2. Stop accepting new requests (drain load balancer)
3. Wait up to 30s for in-flight requests to complete
4. Close connections gracefully (PostgreSQL, MongoDB, NATS)

---

## Dependencies

### 1. Dual MongoDB Drivers

**Severity:** MEDIUM
**Location:** `services/api/go.mod:21-22`

**Description:**
Both old and new MongoDB drivers are imported:
```
go.mongodb.org/mongo-driver v1.17.9
go.mongodb.org/mongo-driver/v2 v2.5.0
```

This increases binary size and maintenance burden. Only v2 should be used.

**Suggested Fix:**
1. Audit all MongoDB code: replace v1 imports with v2
2. Update API compatibility layer if needed
3. Remove v1.17.9 from go.mod
4. Test thoroughly after migration (v2 has API changes)

---

### 2. Unvendored Playwright Dependency Chain

**Severity:** LOW
**Location:** `services/agent-yandex-business/go.mod` (depends on playwright-community/playwright-go)

**Description:**
Playwright Go wrapper is community-maintained and may lag behind official releases. Security fixes in Playwright could be delayed.

**Suggested Fix:**
1. Monitor playwright-community/playwright-go releases
2. Set up automated dependency updates (Dependabot)
3. Consider rolling own Playwright wrapper if updates become infrequent
4. Pin specific version, don't use floating versions

---

### 3. Axioms Version Pinning Too Loose

**Severity:** LOW
**Location:** `services/frontend/package.json:30`

**Description:**
```json
"axios": "^1.13.5"
```

The `^` allows up to 2.x versions, which could introduce breaking changes.

**Suggested Fix:**
1. Pin to exact version: `"axios": "1.13.5"`
2. Use Dependabot for manual review of updates
3. Same for all critical dependencies (react, next)

---

### 4. No License Checking

**Severity:** LOW
**Location:** All services

**Description:**
No audit of dependency licenses. Could accidentally include GPL-licensed code that requires open-sourcing.

**Suggested Fix:**
1. Add FOSSA or Black Duck to CI/CD
2. Define allowed licenses: Apache 2.0, MIT, BSD
3. Fail CI if GPL or proprietary licenses detected

---

## Summary Table

| Issue | Severity | Component | Fix Effort |
|-------|----------|-----------|-----------|
| Incomplete Yandex RPA implementation | HIGH | agent-yandex-business | Medium |
| Stale cookies not detected | HIGH | agent-yandex-business | Medium |
| Missing error context in SSE | MEDIUM | api | Low |
| JWT claims not type-safe | MEDIUM | api | Low |
| Refresh token in localStorage | MEDIUM | frontend | Medium |
| OAuth state validation weak | LOW | api | Low |
| Cookies domain not validated | MEDIUM | agent-yandex-business | Low |
| No rate limiting on chat | MEDIUM | api | Low |
| N+1 query on integration load | MEDIUM | api | Low |
| Panic in production | MEDIUM | api | Low |
| Silent errors in repo | MEDIUM | api | Low |
| Tool args not validated | MEDIUM | orchestrator | Low |
| Retry logic doesn't differentiate errors | MEDIUM | agent-yandex-business | Low |
| No health check endpoints | MEDIUM | all | Medium |
| No metrics/monitoring | MEDIUM | all | Medium |
| Dual MongoDB drivers | MEDIUM | api | Medium |
| Missing CSRF protection | MEDIUM | frontend | Low |
| No structured logging | LOW | all | Medium |
| Secrets not rotated | MEDIUM | ops | Medium |
| No graceful shutdown | LOW | all | Low |

---

## Recommended Priority Order

1. **Week 1:** Fix HIGH severity issues (Yandex RPA, cookies)
2. **Week 2:** Fix MEDIUM security issues (JWT, localStorage, rate limiting, CSRF)
3. **Week 3:** Add health checks and basic metrics
4. **Ongoing:** Dependency updates, monitoring, test coverage

