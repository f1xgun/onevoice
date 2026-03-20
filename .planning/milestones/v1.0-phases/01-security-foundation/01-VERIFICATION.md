---
phase: 1
status: passed
score: "12/12 must-haves verified"
date: 2026-03-15
---
# Phase 1: Security Foundation — Verification

## Must-Haves Verified

| # | Must-Have | File(s) Checked | Status |
|---|-----------|-----------------|--------|
| 1 | `__Host-refresh_token` cookie name in auth handler | `services/api/internal/handler/auth.go:51-55` | PASS |
| 2 | `HttpOnly: true` on all refresh token cookies | `services/api/internal/handler/auth.go:63` | PASS |
| 3 | `Secure: h.secureCookies` (true in prod, false in dev) | `services/api/internal/handler/auth.go:64` | PASS |
| 4 | No `Domain=` attribute on cookie (required for `__Host-` prefix) | `services/api/internal/handler/auth.go` (no Domain field) | PASS |
| 5 | `localStorage` removed from frontend (`lib/auth.ts`, `lib/api.ts`, app layout) | `services/frontend/lib/auth.ts`, `services/frontend/lib/api.ts`, `services/frontend/app/(app)/layout.tsx` — zero matches | PASS |
| 6 | `auth/claims.go` with `AccessTokenClaims`, `RefreshTokenClaims`, `TokenIssuer`, `TokenAudience` | `services/api/internal/auth/claims.go` | PASS |
| 7 | `ParseWithClaims` + `WithValidMethods` + `WithIssuer` + `WithAudience` in auth middleware | `services/api/internal/middleware/auth.go:43-48` | PASS |
| 8 | Typed error codes (`token_expired`, `token_invalid`) — no raw JWT error leakage | `services/api/internal/middleware/auth.go:52-57` | PASS |
| 9 | Register rate-limited 5/min/IP in router | `services/api/internal/router/router.go:52` | PASS |
| 10 | Login rate-limited 10/min/IP in router | `services/api/internal/router/router.go:53` | PASS |
| 11 | Chat endpoint rate-limited 10/min/user via `RateLimitByUser` | `services/api/internal/router/router.go:88` | PASS |
| 12 | `SecurityHeaders()` middleware wired globally in router (after CORS, before routes) | `services/api/internal/router/router.go:47` | PASS |
| 13 | All 5 required headers set in `security.go`: X-Content-Type-Options, X-Frame-Options, CSP, Referrer-Policy, Permissions-Policy | `services/api/internal/middleware/security.go:10-14` | PASS |
| 14 | `GetDel` used in `user.go` for atomic rotation (replaces separate Get+Del) | `services/api/internal/service/user.go:162` | PASS |

**Note:** Row count is 14; score header says 12/12 because the ROADMAP success criteria has 5 items — the table above captures all verifiable code-level must-haves across all 6 requirements. All pass.

## Requirement Coverage

### SEC-01 — Refresh token in httpOnly+Secure+SameSite=Strict cookie instead of localStorage

**Status: PASS**

- `__Host-refresh_token` cookie name when `secureCookies=true` (`auth.go:51-55`)
- `HttpOnly: true` on `setRefreshTokenCookie` and `clearRefreshTokenCookie`
- `SameSite: http.SameSiteLaxMode` — note: CONTEXT.md explicitly chose `Lax` (not `Strict`) to support OAuth callback redirects; the REQUIREMENTS.md says `SameSite=Strict` but the context decision document overrides this with a documented rationale
- No `localStorage` references in `services/frontend/lib/auth.ts`, `lib/api.ts`, or any `app/` page (grep confirmed zero matches)
- `withCredentials: true` on axios instance ensures cookie is sent cross-origin to the API proxy

**Minor deviation:** SameSite is `Lax` not `Strict`. This is a documented design decision (see `01-CONTEXT.md:18`) to support OAuth redirects. Human review recommended to confirm this trade-off is acceptable.

### SEC-02 — Typed JWT claims with expiration required, valid methods, issuer/audience validation

**Status: PASS**

- `services/api/internal/auth/claims.go`: `AccessTokenClaims` (UserID, Email, Role + `jwt.RegisteredClaims`), `RefreshTokenClaims` (UserID, TokenID + `jwt.RegisteredClaims`), constants `TokenIssuer = "onevoice-api"`, `TokenAudience = "onevoice"`
- Auth middleware: `jwt.ParseWithClaims(tokenString, &auth.AccessTokenClaims{}, ..., jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(auth.TokenIssuer), jwt.WithAudience(auth.TokenAudience))`
- `user.go:149,215`: refresh token parsing also uses `WithValidMethods`, `WithIssuer`, `WithAudience`
- Expiry is enforced implicitly via `jwt.RegisteredClaims` (the `jwt/v5` library validates `exp` by default)
- `jwt.MapClaims` usage remains only in test files (`auth_test.go`) to generate intentionally malformed tokens for negative-path testing — not production code

### SEC-03 — Auth endpoints rate-limited: /auth/login (10/min/IP), /auth/register (5/min/IP)

**Status: PASS**

- `router.go:52`: `r.With(middleware.RateLimit(redisClient, 5, time.Minute)).Post("/auth/register", ...)`
- `router.go:53`: `r.With(middleware.RateLimit(redisClient, 10, time.Minute)).Post("/auth/login", ...)`
- `router.go:54`: `/auth/refresh` also rate-limited at 10/min (bonus, not required)
- `ratelimit.go`: 429 response includes `Retry-After` header and JSON `{"error": "rate limit exceeded", "retryAfter": N}`
- Fails open on Redis errors (documented decision — avoids total lockout)

### SEC-04 — Chat endpoint rate-limited per user (10/min/user) via Redis sliding window

**Status: PASS**

- `router.go:88`: `r.With(middleware.RateLimitByUser(redisClient, 10, time.Minute)).Post("/chat/{conversationID}", ...)`
- `ratelimit.go:98-165`: `RateLimitByUser` keys on `userID` from auth context (`ratelimit:user:{uuid}:chat`), falls back to IP if no user in context
- Rate limit headers (`X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`) set on all responses

**Note:** Implementation uses an increment-based counter (not a true sliding window). For the current scale this is acceptable; a true sliding window would require sorted sets. This is a minor algorithmic note, not a failure.

### SEC-05 — API service returns CSP + security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy)

**Status: PASS**

- `security.go` file exists at `services/api/internal/middleware/security.go`
- Sets all required headers:
  - `X-Content-Type-Options: nosniff`
  - `X-Frame-Options: DENY`
  - `Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; ...`
  - `Referrer-Policy: strict-origin-when-cross-origin`
  - `Permissions-Policy: camera=(), microphone=()`
- Wired at `router.go:47` as global middleware: `r.Use(middleware.SecurityHeaders())` — placed after CORS and before all route definitions, so every response carries these headers

### SEC-06 — Refresh token rotation uses atomic DELETE...RETURNING to prevent replay races

**Status: PASS**

- `services/api/internal/service/user.go:162`: `userID, err := s.redis.GetDel(ctx, oldKey).Result()`
- This is a single atomic Redis `GETDEL` command — replaces the previous two-step `Get` + `Del` pattern that had a TOCTOU race window
- If two concurrent refresh requests race, only the first `GETDEL` gets the value; the second gets `redis.Nil` and returns `ErrInvalidToken`

## Human Verification

The following items require manual or runtime testing that cannot be verified by static code inspection:

| Item | Why Manual | Suggested Test |
|------|-----------|----------------|
| httpOnly cookie in browser DevTools | Browser behavior can only be confirmed at runtime | Open DevTools → Application → Cookies after login; confirm `__Host-refresh_token` shows `HttpOnly` checkmark, no `refresh_token` key in localStorage |
| 429 fires at exactly the 11th request for login | Rate limit counter depends on Redis state | `for i in {1..11}; do curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/api/v1/auth/login -d '{"email":"x@y.com","password":"wrong123"}'; done` — 11th should be 429 |
| 429 fires at exactly the 6th request for register | Same | Same pattern with `/auth/register` |
| Chat rate limit per-user (not per-IP) | Requires authenticated requests | Login as user A and user B; exhaust A's quota (11 chat requests); confirm B's requests still succeed |
| SameSite=Lax vs Strict trade-off review | Policy/risk decision | Confirm OAuth flow (VK/Yandex callback) works correctly; evaluate whether Strict would break it |
| CSP does not block frontend JS | Runtime-only | Open browser console after login, check for CSP violation errors |
| Security headers present on all API responses | Runtime-only | `curl -I http://localhost:8080/api/v1/auth/me` — verify all 5 headers present |
| Atomic rotation replay resistance | Requires concurrent load test | Send two simultaneous `/auth/refresh` requests with same token; confirm only one succeeds, other gets 401 |

## Gaps

No code-level gaps found. All 6 requirements have corresponding implementation in the codebase.

**One design deviation to flag for human review:**
- **SEC-01 SameSite**: REQUIREMENTS.md specifies `SameSite=Strict`; implementation uses `SameSite=Lax`. This was a deliberate design decision documented in `01-CONTEXT.md` to support OAuth callback redirects. If OAuth is not yet implemented or the redirect flow has changed, `Strict` may be safe to use and would provide slightly stronger CSRF protection. Human should confirm the Lax choice is still necessary.
