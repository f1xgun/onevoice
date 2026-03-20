# Phase 1: Security Foundation -- Research

**Researched:** 2026-03-15
**Phase requirements:** SEC-01, SEC-02, SEC-03, SEC-04, SEC-05, SEC-06

---

## 1. Current State Analysis

### Authentication Flow (Today)

**Backend** (`services/api/internal/service/user.go`):
- Login returns `(user, accessToken, refreshToken)` as strings in JSON body.
- Access token: 15min expiry, HS256, typed `AccessTokenClaims` struct (user_id, email, role + `jwt.RegisteredClaims`).
- Refresh token: 7-day expiry, HS256, typed `RefreshTokenClaims` struct (user_id, token_id + `jwt.RegisteredClaims`).
- Refresh tokens stored in Redis keyed by `onevoice:auth:refresh_token:{tokenID}` with value = userID string.
- Token rotation exists: old token DEL'd from Redis, new token SET'd. **Not atomic** -- GET then DEL is two separate Redis commands with a race window.

**Frontend** (`services/frontend/lib/auth.ts`, `services/frontend/lib/api.ts`):
- Zustand store calls `localStorage.setItem('refreshToken', refreshToken)` on login/register.
- Axios 401 interceptor reads `localStorage.getItem('refreshToken')` and POSTs to `/api/v1/auth/refresh` with `{ refreshToken }` in JSON body.
- On logout, `localStorage.removeItem('refreshToken')`.

### Vulnerabilities Identified

| # | Vulnerability | Severity | Location |
|---|--------------|----------|----------|
| V1 | Refresh token in localStorage -- accessible to any XSS | HIGH | `services/frontend/lib/auth.ts:28-29` |
| V2 | Auth middleware uses `jwt.MapClaims` (untyped) -- no issuer/audience validation, no explicit `ValidMethods` | MEDIUM | `services/api/internal/middleware/auth.go:40-46,59-63` |
| V3 | `generateAccessToken` omits `Issuer` and `Audience` registered claims | MEDIUM | `services/api/internal/service/user.go:337-345` |
| V4 | No rate limiting on `/auth/login`, `/auth/register`, `/auth/refresh` | HIGH | `services/api/internal/router/router.go:50-52` (public routes, no middleware) |
| V5 | No per-user rate limiting on `/chat/{conversationID}` | MEDIUM | `services/api/internal/router/router.go:86` |
| V6 | No security headers (CSP, X-Content-Type-Options, X-Frame-Options) | MEDIUM | `services/api/internal/router/router.go` -- only CORS middleware |
| V7 | Refresh token rotation is non-atomic: `redis.Get` then `redis.Del` has a TOCTOU race | MEDIUM | `services/api/internal/service/user.go:176-190` |
| V8 | Login/register responses include `refreshToken` in JSON body -- client stores it in JS-accessible storage | HIGH | `services/api/internal/handler/auth.go:125-129,167-171` |

---

## 2. Implementation Approach by Requirement

### SEC-01: httpOnly Cookie Migration for Refresh Token

**Goal:** Refresh token never accessible to JavaScript. Stored in httpOnly + Secure + SameSite cookie.

**Cookie specification** (from 01-CONTEXT.md decisions):
- Name: `__Host-refresh_token`
- Flags: `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/api/v1/auth`
- Max-Age: 604800 (7 days, matching `RefreshTokenExpiry`)

**`__Host-` prefix requirements:** The browser enforces: (1) `Secure` flag must be set, (2) `Path=/` is normally required by spec. However, since we want `Path=/api/v1/auth` to limit cookie scope, we have two options:
- **Option A:** Use `__Host-refresh_token` with `Path=/`. Cookie is sent on all requests but only read by `/auth/refresh` and `/auth/logout` handlers. Simpler, spec-compliant.
- **Option B:** Use `__Secure-refresh_token` with `Path=/api/v1/auth`. `__Secure-` only requires the `Secure` flag, allows custom Path. More restrictive scope.

**Recommendation:** Option B (`__Secure-refresh_token` with `Path=/api/v1/auth`) -- limits cookie transmission to auth endpoints only, reducing exposure surface. The `__Secure-` prefix still guarantees HTTPS-only transport.

**Dev environment caveat:** `__Secure-` and `__Host-` prefixes require HTTPS. In local dev (http://localhost:3000), the browser will reject these cookies. Options:
- Use plain `refresh_token` name in dev mode (controlled by env var).
- Or use `mkcert` for local HTTPS. The context doc says `__Host-`, so this needs a decision. Implementation should check `cfg.SecureCookies` (bool from env) and conditionally set the prefix + Secure flag.

**Backend changes:**

1. **`services/api/internal/handler/auth.go`** -- `Login()`, `Register()`, `RefreshToken()`:
   - Stop returning `refreshToken` in JSON body.
   - After getting tokens from service, call `setRefreshTokenCookie(w, refreshToken)` helper.
   - `LoginResponse` and `RefreshTokenResponse` structs: remove `RefreshToken` field.

2. **New helper** in `services/api/internal/handler/auth.go`:
   ```go
   func setRefreshTokenCookie(w http.ResponseWriter, token string, secure bool) {
       name := "refresh_token"
       if secure {
           name = "__Secure-refresh_token"
       }
       http.SetCookie(w, &http.Cookie{
           Name:     name,
           Value:    token,
           Path:     "/api/v1/auth",
           MaxAge:   int(7 * 24 * time.Hour / time.Second), // 604800
           HttpOnly: true,
           Secure:   secure,
           SameSite: http.SameSiteLaxMode,
       })
   }
   ```

3. **`services/api/internal/handler/auth.go`** -- `RefreshToken()`:
   - Read refresh token from cookie instead of JSON body: `r.Cookie("refresh_token")` (or `__Secure-refresh_token`).
   - `RefreshTokenRequest` struct becomes unused for refresh; keep for backward compat or remove.
   - Must try both cookie names (with and without prefix) to handle cookie set under either name.

4. **`services/api/internal/handler/auth.go`** -- `Logout()`:
   - Read refresh token from cookie.
   - After revoking, clear cookie: `setRefreshTokenCookie(w, "", secure)` with `MaxAge: -1`.

5. **`AuthHandler` struct needs `secureCookies bool` field** -- injected from config.

**Frontend changes:**

6. **`services/frontend/lib/auth.ts`**:
   - Remove all `localStorage.setItem('refreshToken', ...)` and `localStorage.removeItem('refreshToken')`.
   - `setAuth` signature changes: remove `refreshToken` parameter.
   - Store only `user` and `accessToken` in Zustand.

7. **`services/frontend/lib/api.ts`**:
   - Remove `localStorage.getItem('refreshToken')`.
   - Refresh interceptor: `axios.post('/api/v1/auth/refresh', {}, { withCredentials: true })` -- empty body, cookie sent automatically.
   - Response only contains `{ user, accessToken }` -- no `refreshToken` to store.

8. **`services/frontend/app/(public)/login/page.tsx`** and **`register/page.tsx`**:
   - Update `setAuth` call: `setAuth(res.data.user, res.data.accessToken)` -- two args instead of three.

9. **`services/frontend/lib/api.ts`** -- Axios instance:
   - Add `withCredentials: true` to the base Axios config so cookies are sent on all requests (needed for cross-origin if API is on different port in dev).

10. **`services/frontend/next.config.js`** -- rewrites:
    - The Next.js dev server proxies `/api/v1/*` to `localhost:8080`. Cookies set by the API response will have the proxy's origin. This works because the browser sees the cookie from the same origin (localhost:3000). **No changes needed** -- Next.js rewrites forward Set-Cookie headers transparently.

**CORS update** (`services/api/internal/router/router.go`):
- `AllowCredentials: true` is already set.
- Must ensure `AllowedHeaders` includes cookie-related headers if needed (it doesn't need to -- cookies are sent automatically, not via custom headers).

---

### SEC-02: Typed JWT Claims with Full Validation

**Goal:** Auth middleware uses typed `AccessTokenClaims` struct with explicit validation of expiration, signing method, issuer, and audience.

**Current state:** Auth middleware (`services/api/internal/middleware/auth.go:40`) uses `jwt.Parse` with `jwt.MapClaims`. The service layer already has `AccessTokenClaims` and `RefreshTokenClaims` structs in `services/api/internal/service/user.go`.

**Changes:**

1. **Move claims types to shared location.** Currently `AccessTokenClaims` is in `services/api/internal/service/user.go`. The middleware needs it too. Options:
   - **Option A:** Move to `services/api/internal/middleware/claims.go` -- middleware becomes the source of truth, service imports from middleware. Breaks layering (service importing middleware).
   - **Option B:** Move to a new `services/api/internal/auth/claims.go` package. Both middleware and service import from it. Clean separation.
   - **Option C:** Move to `pkg/domain/claims.go`. Available across all services. Overkill -- only API service needs it.
   - **Recommendation:** Option B -- `services/api/internal/auth/` package with `claims.go` and constants.

2. **Define constants for issuer and audience:**
   ```go
   // services/api/internal/auth/claims.go
   const (
       TokenIssuer   = "onevoice-api"
       TokenAudience = "onevoice"
   )
   ```

3. **Update `generateAccessToken`** (`services/api/internal/service/user.go:336-354`):
   - Add `Issuer: TokenIssuer` and `Audience: jwt.ClaimStrings{TokenAudience}` to `RegisteredClaims`.
   - Same for `generateRefreshToken`.

4. **Update auth middleware** (`services/api/internal/middleware/auth.go:29-101`):
   - Replace `jwt.Parse` with `jwt.ParseWithClaims` using `&auth.AccessTokenClaims{}`.
   - Add `jwt.WithValidMethods([]string{"HS256"})` parser option.
   - Add `jwt.WithIssuer(auth.TokenIssuer)` parser option.
   - Add `jwt.WithAudience(auth.TokenAudience)` parser option.
   - Remove manual MapClaims extraction (lines 59-90) -- replaced by typed struct field access.
   - On parse error, always return 401 with typed error message (never 500).

5. **Update `RefreshToken` and `Logout` in service** (`services/api/internal/service/user.go:157-163, 227-232`):
   - Add `jwt.WithValidMethods`, `jwt.WithIssuer`, `jwt.WithAudience` options to `jwt.ParseWithClaims` calls.

**Error handling detail:** The `jwt.Parse` function already returns specific error types (`jwt.ErrTokenExpired`, `jwt.ErrTokenNotValidYet`, etc.). The middleware should map these to typed 401 error messages:
- Expired: `{"error": "token_expired"}`
- Invalid signature: `{"error": "token_invalid"}`
- Malformed: `{"error": "token_malformed"}`

This allows the frontend to distinguish between "need to refresh" (expired) vs. "need to re-login" (invalid/tampered).

---

### SEC-03: Rate Limiting on Auth Endpoints

**Goal:** `/auth/login` limited to 10/min/IP, `/auth/register` limited to 5/min/IP.

**Current state:** `services/api/internal/middleware/ratelimit.go` has a working `RateLimit(redisClient, limit, window)` middleware using Redis INCR + EXPIRE (fixed-window counter). It keys on `ratelimit:{ip}:{path}`.

**Changes:**

1. **Update 429 response format.** Current `writeJSONError(w, 429, "rate limit exceeded")` needs to include `retryAfter` field per context decisions:
   ```go
   type RateLimitError struct {
       Error      string `json:"error"`
       RetryAfter int    `json:"retryAfter"` // seconds
   }
   ```
   Also add `Retry-After` HTTP header (already have TTL from Redis).

2. **Apply per-endpoint rate limiters in router** (`services/api/internal/router/router.go:50-52`):
   ```go
   // Currently:
   r.Post("/auth/register", handlers.Auth.Register)
   r.Post("/auth/login", handlers.Auth.Login)
   r.Post("/auth/refresh", handlers.Auth.RefreshToken)

   // Change to:
   r.With(middleware.RateLimit(redisClient, 5, time.Minute)).Post("/auth/register", handlers.Auth.Register)
   r.With(middleware.RateLimit(redisClient, 10, time.Minute)).Post("/auth/login", handlers.Auth.Login)
   r.With(middleware.RateLimit(redisClient, 10, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)
   ```

3. **No structural changes to the rate limiter itself.** The existing INCR/EXPIRE pattern with per-IP + per-path keying works correctly for this use case. The fixed-window approach is acceptable -- sliding window would be more precise but adds complexity for minimal gain on auth endpoints.

**Note on `/auth/refresh` rate limiting:** Not explicitly in SEC-03 but should be rate limited to prevent token grinding. 10/min/IP is reasonable.

---

### SEC-04: Chat Rate Limiting Per User

**Goal:** `/chat/{conversationID}` limited to 10/min per user (not per IP).

**Current state:** The existing `RateLimit` middleware keys on IP + path. Need a new variant that keys on user ID.

**Changes:**

1. **New middleware function** in `services/api/internal/middleware/ratelimit.go`:
   ```go
   func RateLimitByUser(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler
   ```
   - Extracts `userID` from context (set by `Auth` middleware, which runs before this).
   - Redis key: `ratelimit:user:{userID}:{path}` or simpler `ratelimit:user:{userID}:chat`.
   - Same INCR/EXPIRE logic as `RateLimit`.
   - Falls back to IP-based limiting if userID not in context (shouldn't happen since Auth middleware runs first).
   - Same 429 response format with `retryAfter` and `Retry-After` header.

2. **Apply in router** (`services/api/internal/router/router.go:86`):
   ```go
   // Inside the protected group, after Auth middleware:
   r.With(middleware.RateLimitByUser(redisClient, 10, time.Minute)).Post("/chat/{conversationID}", handlers.ChatProxy.Chat)
   ```

3. **Router signature change:** `Setup` function currently takes `(handlers, jwtSecret, redisClient, uploadDir)`. The `redisClient` is already available. No signature change needed -- just use it in a new `r.With(...)` call.

---

### SEC-05: Security Headers

**Goal:** All API responses include CSP, X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Permissions-Policy.

**Changes:**

1. **New middleware** in `services/api/internal/middleware/security.go`:
   ```go
   func SecurityHeaders() func(http.Handler) http.Handler {
       return func(next http.Handler) http.Handler {
           return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
               w.Header().Set("X-Content-Type-Options", "nosniff")
               w.Header().Set("X-Frame-Options", "DENY")
               w.Header().Set("Content-Security-Policy",
                   "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'")
               w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
               w.Header().Set("Permissions-Policy", "camera=(), microphone=()")
               next.ServeHTTP(w, r)
           })
       }
   }
   ```

2. **Apply globally** in `services/api/internal/router/router.go`:
   ```go
   r.Use(middleware.SecurityHeaders())
   ```
   Add after CORS middleware (CORS must run first to handle preflight).

**CSP details for Next.js compatibility:**
- `script-src 'self' 'unsafe-inline'` -- Next.js injects inline scripts for hydration. Without `'unsafe-inline'`, the dashboard breaks. (Context doc confirms this decision.)
- `style-src 'self' 'unsafe-inline'` -- Tailwind may inject inline styles via `style` attribute on some components.
- `img-src 'self' data: https:` -- Allow data URIs (base64 images) and external HTTPS images (platform logos, user uploads).
- `connect-src 'self'` -- All API calls are same-origin via Next.js proxy.

**Important:** These headers are set on the Go API. The Next.js frontend also serves HTML pages. Since the browser loads pages from Next.js (port 3000) and the API is proxied, the API's CSP headers only apply to API responses (JSON, SSE). The actual page-level CSP should come from Next.js. However, since the browser is making fetch requests to the API through the Next.js proxy, the proxy forwards the API response headers. This is fine -- CSP on JSON responses is ignored by browsers (CSP applies to document responses). Still, setting them is defense-in-depth and satisfies SEC-05.

**Note:** If the frontend is ever served directly from the Go API (not via Next.js proxy), these headers become critical for the HTML document response.

---

### SEC-06: Atomic Refresh Token Rotation

**Goal:** Prevent replay attacks by making token revocation + validation atomic using Redis operations.

**Current state** (`services/api/internal/service/user.go:175-190`):
```go
// Non-atomic: GET then DEL
userID, err := s.redis.Get(ctx, oldKey).Result()
// ... validate ...
s.redis.Del(ctx, oldKey)
```

**Race condition:** Two concurrent requests with the same refresh token can both GET successfully before either DEL runs. Both get new token pairs. The old token is effectively used twice.

**Fix -- use Redis `GETDEL` command (Redis 6.2+):**
```go
userID, err := s.redis.GetDel(ctx, oldKey).Result()
```
`GETDEL` atomically returns the value and deletes the key. If two concurrent requests race, only the first gets the value; the second gets `redis.Nil`.

**Alternative -- Lua script (for Redis < 6.2):**
```lua
local val = redis.call('GET', KEYS[1])
if val then redis.call('DEL', KEYS[1]) end
return val
```
Execute via `s.redis.Eval(ctx, script, []string{oldKey})`.

**Recommendation:** Use `GETDEL` -- simpler, no Lua needed, Redis 6.2+ is standard. The project uses `go-redis/v9` which supports `GETDEL`.

**Changes in `services/api/internal/service/user.go`:**

1. **`RefreshToken` method** (line 175-190):
   - Replace `s.redis.Get(ctx, oldKey)` + `s.redis.Del(ctx, oldKey)` with single `s.redis.GetDel(ctx, oldKey)`.
   - Remove the separate `s.redis.Del` call.

2. **`Logout` method** -- no change needed (single DEL is fine for logout; idempotent).

---

## 3. Codebase Integration Points

### Files to Modify

| File | Changes |
|------|---------|
| `services/api/internal/handler/auth.go` | Cookie-based refresh token (read/write), remove refreshToken from response bodies, add `secureCookies` to AuthHandler, clear cookie on logout |
| `services/api/internal/service/user.go` | Add Issuer/Audience to token generation, atomic GETDEL for refresh rotation |
| `services/api/internal/middleware/auth.go` | Typed `AccessTokenClaims`, `ParseWithClaims`, ValidMethods, Issuer, Audience validation, typed error messages |
| `services/api/internal/middleware/ratelimit.go` | Add `RateLimitByUser` function, update 429 response to include `retryAfter` + `Retry-After` header |
| `services/api/internal/router/router.go` | Apply per-endpoint rate limiters, apply `SecurityHeaders` middleware, pass `secureCookies` config |
| `services/api/cmd/main.go` | Pass `secureCookies` config to AuthHandler |
| `services/api/internal/config/config.go` | Add `SecureCookies bool` field (from `SECURE_COOKIES` env, default `true`) |
| `services/frontend/lib/auth.ts` | Remove localStorage for refreshToken, update `setAuth` signature |
| `services/frontend/lib/api.ts` | Cookie-based refresh (empty POST body, `withCredentials: true`), remove localStorage reads |
| `services/frontend/app/(public)/login/page.tsx` | Update `setAuth` call (2 args) |
| `services/frontend/app/(public)/register/page.tsx` | Update `setAuth` call (2 args) |

### New Files

| File | Purpose |
|------|---------|
| `services/api/internal/auth/claims.go` | Shared `AccessTokenClaims`, `RefreshTokenClaims` types + issuer/audience constants |
| `services/api/internal/middleware/security.go` | Security headers middleware |

### Function Signatures Affected

**Handler layer:**
- `NewAuthHandler(userService UserService)` -> `NewAuthHandler(userService UserService, secureCookies bool)`
- `LoginResponse` struct: remove `RefreshToken` field
- `RefreshTokenResponse` struct: remove `RefreshToken` field
- `RefreshTokenRequest` struct: removed (token comes from cookie)
- `LogoutRequest` struct: removed (token comes from cookie)

**Middleware layer:**
- New: `RateLimitByUser(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler`
- New: `SecurityHeaders() func(http.Handler) http.Handler`
- `Auth(jwtSecret []byte)` -- internal implementation changes (typed claims), signature unchanged.

**Service layer:**
- `generateAccessToken(user *domain.User, secret []byte)` -- internal changes (add Issuer/Audience), signature unchanged.
- `generateRefreshToken(userID uuid.UUID, secret []byte)` -- internal changes (add Issuer/Audience), signature unchanged.
- `RefreshToken(ctx, refreshToken)` -- internal changes (GETDEL), signature unchanged.

**Router layer:**
- `Setup(handlers, jwtSecret, redisClient, uploadDir)` -- signature unchanged, internal routing logic changes.

**Frontend:**
- `setAuth: (user: User, accessToken: string) => void` -- refreshToken parameter removed.
- `useAuthStore` -- no `refreshToken` state.

---

## 4. Dependencies and Ordering

### Implementation Order

```
PLAN-1.4 (SEC-05: Security headers)
    |-- No dependencies, pure additive middleware
    |-- Can be done first as it's isolated

PLAN-1.2 (SEC-02: Typed JWT claims)
    |-- Create auth/claims.go package first
    |-- Then update service (token generation)
    |-- Then update middleware (token validation)
    |-- Must be done BEFORE SEC-01 (cookie migration needs proper token handling)

PLAN-1.1 (SEC-01 + SEC-06: Cookie migration + atomic rotation)
    |-- Depends on SEC-02 (typed claims) for consistent token generation
    |-- SEC-06 (GETDEL) is a one-line change inside this plan
    |-- Backend cookie handling first, frontend migration second
    |-- Must be a coordinated backend+frontend change

PLAN-1.3 (SEC-03 + SEC-04: Rate limiting)
    |-- Independent of cookie migration
    |-- SEC-04 depends on Auth middleware being in the chain (already is)
    |-- Can run in parallel with PLAN-1.1 but safest after
```

### Recommended sequence:
1. **PLAN-1.4** (security headers) -- zero risk, pure addition
2. **PLAN-1.2** (typed JWT claims) -- foundation for other changes
3. **PLAN-1.1** (cookie migration + atomic rotation) -- biggest change, needs careful testing
4. **PLAN-1.3** (rate limiting) -- independent, can be last

### External Dependencies

- **Redis 6.2+** required for `GETDEL` command (SEC-06). Verify deployment Redis version. Fallback: Lua script.
- **go-redis/v9** already in `go.mod` -- supports `GetDel` method.
- **No new Go dependencies** needed.
- **No new npm dependencies** needed.

---

## 5. Risks and Gotchas

### Cookie + SameSite + Next.js Proxy

- **Risk:** Next.js rewrites proxy requests to the API. The browser sends the request to `localhost:3000`, which proxies to `localhost:8080`. Cookies set by the API response (`Set-Cookie` header) are forwarded back by Next.js. The cookie's domain will be `localhost` (from the browser's perspective). `SameSite=Lax` allows this because it's same-site (both on localhost).
- **Gotcha:** In production, if API and frontend are on different subdomains (e.g., `api.onevoice.com` vs `app.onevoice.com`), they share the same registrable domain, so `SameSite=Lax` works. If they're on completely different domains, `SameSite=None` + `Secure` would be needed. Current architecture (Next.js proxy) avoids this.

### `__Host-` vs `__Secure-` Prefix in Development

- **Risk:** Both prefixes require HTTPS (`Secure` flag). Local dev runs on `http://localhost`. Browsers reject `Secure` cookies on HTTP (except Chrome allows `Secure` cookies on `localhost` as a special case, but Firefox and Safari may not).
- **Mitigation:** Use `SECURE_COOKIES=false` env var in dev, which drops the prefix and `Secure` flag. Production always uses `SECURE_COOKIES=true`.

### Concurrent Refresh Race (SEC-06)

- **Risk:** After switching to `GETDEL`, if two tabs simultaneously try to refresh, only one succeeds. The other gets 401 and must redirect to login.
- **Mitigation:** This is the desired behavior -- replay protection. The frontend's refresh interceptor already has a queue mechanism (`refreshing` boolean + `queue` array in `api.ts`) that serializes concurrent 401 retries through a single refresh call. Only one refresh request should fire at a time. Second tab's request waits in queue, gets the new access token from the first refresh.
- **Edge case:** If the first refresh fails after GETDEL consumed the old token, the user is logged out. This is correct security behavior.

### CORS with Credentials

- **Risk:** `AllowCredentials: true` is already set in CORS config, but `AllowedOrigins` is `["http://localhost:3000"]`. With credentials, `AllowedOrigins` cannot be `*` -- must be explicit. This is already correct.
- **Gotcha:** Production must update `AllowedOrigins` to the actual frontend domain. Should be configurable via env var (`CORS_ALLOWED_ORIGINS`).

### WriteTimeout and SSE

- **Risk:** The chat endpoint streams SSE. `WriteTimeout: 15 * time.Second` in `cmd/main.go:156` will kill long-running SSE connections. This is a pre-existing issue, not introduced by this phase, but rate limiting the chat endpoint draws attention to it.
- **Note:** Out of scope for Phase 1, but worth flagging. The SSE handler should use `http.Hijacker` or set a longer timeout for SSE routes.

### Auth Middleware Error Leakage

- **Risk:** Current middleware does `writeJSONError(w, 401, "invalid token: "+err.Error())` (line 49). The `err.Error()` from jwt library can leak internal details (e.g., "token is expired by 5m30s").
- **Fix (part of SEC-02):** Map jwt errors to typed codes, don't expose raw error strings.

### Fixed-Window vs Sliding-Window Rate Limiting

- **Risk:** The current INCR/EXPIRE approach is a fixed-window counter. A user can send 10 requests at :59 and 10 more at :00, effectively getting 20/min burst at the window boundary.
- **Mitigation:** Acceptable for auth endpoints (low-volume, high-value). Sliding window (Redis sorted sets) is more accurate but adds complexity. The context doc says "Redis sliding window" for SEC-04 -- consider implementing sliding window for chat rate limiting specifically.
- **Recommendation:** Keep fixed-window for auth endpoints (SEC-03). Implement sliding window for chat (SEC-04) if the context doc requires it, otherwise fixed-window is sufficient for MVP.

### Frontend `withCredentials` Across All Requests

- **Risk:** Setting `withCredentials: true` on the Axios instance means cookies are sent with every request, including non-auth requests. The refresh token cookie is scoped to `Path=/api/v1/auth`, so it won't be sent on other paths. No security issue.
- **Note:** The access token continues to be sent via `Authorization: Bearer` header (from Zustand store). No change to that mechanism.

---

## 6. Validation Architecture

### SEC-01: httpOnly Cookie Verification

**Manual verification:**
1. Log in via browser.
2. Open DevTools > Application > Cookies.
3. Verify `__Secure-refresh_token` (or `refresh_token` in dev) exists with `HttpOnly` checked, `Secure` checked (production), `SameSite=Lax`.
4. In Console, run `document.cookie` -- refresh token must NOT appear.
5. Open DevTools > Application > Local Storage -- no `refreshToken` key.
6. Refresh the page -- app should silently refresh the access token via cookie and remain authenticated.

**Automated test approach:**
- Integration test: login, inspect `Set-Cookie` response header, verify `HttpOnly` flag, verify no `refreshToken` in JSON body.
- Test refresh endpoint: send request without JSON body but with cookie header, verify 200 + new access token.
- Test refresh endpoint: send request without cookie, verify 401.

### SEC-02: Typed JWT Validation

**Automated tests:**
1. **Expired token:** Generate token with `ExpiresAt` in the past, send to protected endpoint, expect `401` with `{"error": "token_expired"}`.
2. **Wrong signing method:** Create token signed with RS256 (or none), send to endpoint, expect `401`.
3. **Missing issuer:** Generate token without `Issuer` claim, expect `401`.
4. **Wrong audience:** Generate token with wrong `Audience`, expect `401`.
5. **Tampered token:** Modify payload of valid token, expect `401`.
6. **Valid token:** Generate proper token, expect `200`.
7. **Verify no 500 responses** for any malformed input -- always 401.

### SEC-03: Auth Rate Limiting

**Automated tests:**
1. Send 10 POST requests to `/auth/login` from same IP within 1 minute -- all should succeed (or fail with 401 for wrong creds, but not 429).
2. Send 11th request -- expect `429` with `Retry-After` header and `{"error": "rate limit exceeded", "retryAfter": N}`.
3. Send 5 POST requests to `/auth/register` -- all should succeed.
4. Send 6th request -- expect `429`.
5. Verify `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` headers on all responses.
6. Wait for window to expire, verify requests are allowed again.

### SEC-04: Chat Rate Limiting Per User

**Automated tests:**
1. Authenticate as user A.
2. Send 10 POST requests to `/chat/{id}` -- all should succeed (or return normal responses).
3. Send 11th request -- expect `429`.
4. Authenticate as user B, send request to same endpoint -- should succeed (different user).
5. Verify rate limit headers.

### SEC-05: Security Headers

**Automated test:**
1. Send any request to the API (e.g., `GET /health`).
2. Verify response headers contain:
   - `X-Content-Type-Options: nosniff`
   - `X-Frame-Options: DENY`
   - `Content-Security-Policy` (contains `default-src 'self'`)
   - `Referrer-Policy: strict-origin-when-cross-origin`
   - `Permissions-Policy: camera=(), microphone=()`
3. Repeat for authenticated endpoints to ensure headers are present regardless of auth status.

### SEC-06: Atomic Refresh Rotation

**Automated tests:**
1. **Single refresh:** Login, get refresh token, call refresh -- old token revoked, new token works.
2. **Replay detection:** Login, get refresh token, call refresh (success), call refresh again with OLD token -- expect `401`.
3. **Concurrent replay simulation:** In test, manually call `redis.GetDel` twice on same key -- first returns value, second returns nil. Verify the service returns `ErrInvalidToken` for the second call.

### End-to-End Smoke Test

1. Register new user (rate limited to 5/min).
2. Login (rate limited to 10/min).
3. Verify no `refreshToken` in localStorage.
4. Verify httpOnly cookie in response.
5. Wait 15 min (or use short-lived test token) for access token to expire.
6. Make authenticated request -- interceptor refreshes via cookie, request succeeds.
7. Check all response headers include security headers.
8. Spam chat endpoint 11 times -- 11th returns 429.
9. Try to reuse old refresh token after rotation -- 401.

---

## RESEARCH COMPLETE
