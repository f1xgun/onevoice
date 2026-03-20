# Plan 01-03 Summary: httpOnly cookie migration + atomic rotation

**Status:** Complete
**Date:** 2026-03-15

## What was done

### Task 1-03-01: Add SecureCookies config field
- Added `SecureCookies bool` field to `Config` struct in `services/api/internal/config/config.go`
- Defaults to `true` via `SECURE_COOKIES` env var; set to `false` for local dev

### Task 1-03-02: Update AuthHandler to use cookie-based refresh tokens
- Added `secureCookies bool` field to `AuthHandler` struct
- Updated `NewAuthHandler` to accept `secureCookies` parameter
- Added cookie helper methods: `cookieName()`, `setRefreshTokenCookie()`, `clearRefreshTokenCookie()`, `readRefreshTokenCookie()`
- Cookie uses `__Host-refresh_token` name when secure, `refresh_token` when not
- All cookies set with `HttpOnly: true`, `SameSite: Lax`, `Path: /`
- Removed `RefreshTokenRequest` and `LogoutRequest` structs (token comes from cookie)
- Removed `RefreshToken` field from `LoginResponse` and `RefreshTokenResponse`
- Updated `Register`, `Login`, `RefreshToken`, `Logout` handlers
- Updated all handler tests to use cookie-based assertions

### Task 1-03-03: Atomic refresh token rotation with GETDEL
- Replaced separate `Get` + `Del` Redis calls with single `GetDel` in `RefreshToken` method
- Prevents TOCTOU race where two concurrent refresh requests could both succeed

### Task 1-03-04: Wire SecureCookies config into AuthHandler
- Updated `services/api/cmd/main.go` to pass `cfg.SecureCookies` to `NewAuthHandler`

### Task 1-03-05: Update frontend auth store
- Removed all `localStorage` references from `services/frontend/lib/auth.ts`
- Simplified `setAuth` to 2-arg signature: `(user, accessToken)`
- Removed `refreshToken` parameter entirely

### Task 1-03-06: Update frontend API client
- Added `withCredentials: true` to axios instance config
- Refresh interceptor sends empty POST body with `withCredentials: true`
- Removed all `localStorage` references from `services/frontend/lib/api.ts`
- Moved `RefreshResponse` interface to module level (removed `refreshToken` field)

### Task 1-03-07: Update login, register, and layout pages
- Updated login and register pages to use 2-arg `setAuth`
- Rewrote app layout to attempt silent refresh via httpOnly cookie instead of checking localStorage
- Updated auth tests to match new 2-arg signature and remove localStorage assertions

## Additional changes (not in plan)
- Updated `services/frontend/app/(app)/layout.tsx` — had a 3-arg `setAuth` call and localStorage usage
- Updated `services/frontend/lib/__tests__/auth.test.ts` — had 3-arg `setAuth` calls and localStorage assertions

## Decisions
- Cookie name: `__Host-refresh_token` (secure) / `refresh_token` (dev) per CONTEXT.md
- SameSite: `Lax` (not Strict) per CONTEXT.md — needed for OAuth callback redirects
- `readRefreshTokenCookie` tries both names for upgrade path compatibility
- App layout: no longer checks localStorage; attempts refresh via cookie, redirects to login on failure

## Verification
- `cd services/api && GOWORK=off go build ./...` — passes
- `cd services/api && GOWORK=off go test -race ./...` — all tests pass
- `cd services/frontend && pnpm exec tsc --noEmit` — passes
- 7 atomic commits created
