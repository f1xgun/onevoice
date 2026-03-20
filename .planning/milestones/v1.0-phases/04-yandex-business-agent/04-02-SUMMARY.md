# SUMMARY 04-02: Session Canary Check and ErrSessionExpired

## Status: Complete

## Tasks Completed

### 04-02-01: Create checkSession canary function and ErrSessionExpired sentinel
- Created `services/agent-yandex-business/internal/yandex/canary.go`
- Defined `ErrSessionExpired` sentinel error
- Implemented `checkSession(page, expectedURLPrefix)` — detects passport.yandex redirects, CAPTCHA gates, and unexpected URLs
- Implemented `checkSessionAndEvict(page, expectedURLPrefix, pool, businessID)` — pool-aware variant that evicts browser context on session expiry
- Defined `ContextEvictor` interface to decouple from concrete `BrowserPool` type (04-01 running in parallel)
- All errors wrapped as `NonRetryableError` from `pkg/a2a`

### 04-02-02: Extend classifyYandexError with new non-retryable cases
- Added `errors.Is(err, yandex.ErrSessionExpired)` sentinel check at top of function
- Added "review not found" as non-retryable
- Added "reply form unavailable" and "reply button not found" as non-retryable
- Added `errors` and `yandex` package imports
- `go build ./internal/...` compiles cleanly (cmd/main.go has pre-existing 04-01 wiring issues)

## Files Modified
- `services/agent-yandex-business/internal/yandex/canary.go` (new)
- `services/agent-yandex-business/internal/agent/handler.go` (extended classifyYandexError)

## Notes
- Plan 04-01 (BrowserPool) was running in parallel and had partially modified handler.go (added BrowserPool interface, changed Handler struct). The `cmd/main.go` wiring has a type mismatch that 04-01 needs to resolve.
- Used `ContextEvictor` interface instead of concrete `*BrowserPool` pointer to avoid compile dependency on pool.go which doesn't exist yet.
