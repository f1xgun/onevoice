# Phase 1: Security Foundation - Context

**Gathered:** 2026-03-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Eliminate highest-risk authentication vulnerabilities: migrate refresh tokens to httpOnly cookies, enforce typed JWT validation, add rate limiting to auth and chat endpoints, and set security response headers. Backend (Go API) and frontend (Next.js) changes only — no new services or infrastructure.

</domain>

<decisions>
## Implementation Decisions

### Cookie Transport Configuration
- Refresh token cookie named `__Host-refresh_token` — `__Host-` prefix enforces Secure + Path=/
- SameSite=Lax to allow top-level navigations (needed for OAuth callback redirects)
- Browser auto-sends cookie to `/auth/refresh` — no JavaScript access needed, endpoint reads from cookie
- Access token stays in memory (Zustand store), sent via Authorization header; page refresh triggers silent cookie-based refresh

### Rate Limiting Strategy
- Redis failure: fail open (allow request) — avoids total lockout if Redis goes down
- Chat endpoint rate limited per user ID: 10 requests/min (SEC-04)
- Rate limit 429 response includes JSON body `{"error": "rate limit exceeded", "retryAfter": N}` plus `Retry-After` header
- Separate middleware instances per auth endpoint: login 10/min/IP, register 5/min/IP (SEC-03)

### Security Headers & CSP
- Moderate CSP: `default-src 'self'; script-src 'self' 'unsafe-inline'` — Next.js requires inline scripts for hydration
- Additional headers beyond SEC-05: `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy: camera=(), microphone=()`
- Headers applied via Go middleware on API service (all API responses)
- `X-Frame-Options: DENY` — no framing use case in dashboard

### Claude's Discretion
- Exact CSP directives for style-src, img-src, connect-src (adapt to what frontend needs)
- Redis key format for per-user rate limiting
- Cookie Path and Max-Age values for refresh token cookie
- Whether to add CSRF token validation (not in SEC requirements, but natural complement to cookie auth)

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `services/api/internal/middleware/ratelimit.go` — existing per-IP rate limiter with Redis sliding window, X-RateLimit-* headers
- `services/api/internal/service/user.go` — already has typed `AccessTokenClaims` and `RefreshTokenClaims` structs with `jwt.RegisteredClaims`
- `services/api/internal/middleware/auth.go` — JWT validation middleware (currently uses MapClaims, needs migration to typed claims)
- `services/frontend/lib/auth.ts` — Zustand auth store (currently stores refreshToken in localStorage)
- `services/frontend/lib/api.ts` — Axios interceptor for token refresh (currently sends refreshToken in JSON body)

### Established Patterns
- Middleware chain in chi router (`services/api/internal/router/router.go`)
- Handler → Service → Repository layering
- Redis client already injected into middleware and services
- `writeJSONError()` helper for consistent error responses

### Integration Points
- `services/api/internal/router/router.go` — where new middleware is added to routes
- `services/api/cmd/main.go` — Redis client wiring
- `services/frontend/lib/api.ts` — Axios interceptors for cookie-based refresh
- `services/frontend/lib/auth.ts` — Remove localStorage refresh token storage
- `services/frontend/app/(public)/login/page.tsx` and `register/page.tsx` — Update to not store refresh token from response

</code_context>

<specifics>
## Specific Ideas

No specific requirements — standard security hardening patterns apply.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
