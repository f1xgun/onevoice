---
plan: "06-01"
title: "Auth flow test suite"
status: complete
completed: "2026-03-20"
commits:
  - 3c970f6: "feat(06-01): add ChangePassword, SecureCookies, NilService, and AutoLoginFailure handler tests"
  - 5f46b1c: "feat(06-01): add JWT signing method, issuer, audience, and none-algorithm validation tests"
  - 8491f95: "feat(06-01): add RateLimitByUser and Redis-failure rate limiter tests"
---

## Summary

Added comprehensive auth flow tests across three test files in `services/api/`:

### Task 6-01-01: Auth handler tests (`internal/handler/auth_test.go`)
- **TestChangePassword** — 8 sub-tests covering: success, missing userID (401), invalid JSON (400), newPassword too short (400), empty currentPassword (400), wrong current password (401), user not found (404), internal error (500 with no detail leak)
- **TestNewAuthHandler_NilService** — verifies nil service is rejected with error
- **TestSecureCookies** — 3 sub-tests: login sets `__Host-refresh_token` with `Secure=true`, refresh with `__Host-refresh_token` succeeds, refresh with plain `refresh_token` succeeds (upgrade path)
- **TestRegister_AutoLoginFailure** — covers the auto-login error branch returning 500

### Task 6-01-02: JWT validation tests (`internal/middleware/auth_test.go`)
- **TestAuth_WrongSigningMethod** — proves HS384 token rejected even with correct secret
- **TestAuth_WrongIssuer** — proves wrong issuer rejected
- **TestAuth_WrongAudience** — proves wrong audience rejected
- **TestAuth_NoneAlgorithm** — proves unsigned JWT (alg:none) rejected

### Task 6-01-03: Rate limiter tests (`internal/middleware/ratelimit_test.go`)
- **TestRateLimitByUser_WithinLimit** — user-based limiting with correct header decrement
- **TestRateLimitByUser_ExceedsLimit** — returns 429 with `rate limit exceeded` and `Retry-After`
- **TestRateLimitByUser_FallbackToIP** — IP-based limiting when no user context
- **TestRateLimitByUser_DifferentUsers** — independent rate limit counters per user
- **TestRateLimit_RedisDown** — fail-open behavior (200 when Redis is closed)
- **TestRateLimit_RetryAfterHeader** — positive integer in `Retry-After` header

### Verification
- `cd services/api && GOWORK=off go test -race ./internal/handler/...` — PASS
- `cd services/api && GOWORK=off go test -race ./internal/middleware/...` — PASS
