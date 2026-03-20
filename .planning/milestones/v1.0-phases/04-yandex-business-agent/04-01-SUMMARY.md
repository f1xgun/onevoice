# SUMMARY 04-01: BrowserPool with Per-Business Contexts

## Status: Complete

## What Was Done

### Task 04-01-01: Create BrowserPool struct with lazy Chromium init
- Created `services/agent-yandex-business/internal/yandex/pool.go`
- `BrowserPool` struct with `sync.Map` for per-business `pooledContext` objects
- Lazy Chromium initialization via `ensureBrowser()` — no startup cost
- `WithPage(ctx, businessID, cookiesJSON, fn)` — primary API for tool implementations
- Per-business mutex serializes page access within a business context
- `EvictContext(businessID)` for explicit context removal on session expiry
- `Close()` shuts down all contexts, browser, and Playwright cleanly
- Background `evictLoop()` removes idle contexts after `maxIdle` (default 15 min)
- Extracted `injectCookies` as package-level function (moved from `Browser.setCookies`)

### Task 04-01-02: Create BusinessBrowser wrapper
- `BusinessBrowser` struct in pool.go implements `agent.YandexBrowser` interface
- `ForBusiness(businessID, cookiesJSON)` factory method on `BrowserPool`
- All 4 interface methods (`GetReviews`, `ReplyReview`, `UpdateInfo`, `UpdateHours`) delegate to pool's `WithPage`

### Task 04-01-03: Remove old Browser struct and wire pool
- Deleted 4 old stub files: `get_reviews.go`, `reply_review.go`, `update_info.go`, `update_hours.go`
- Refactored `browser.go` — removed `Browser` struct, `NewBrowser`, `withPage`, `setCookies`; kept `humanDelay`, `withRetry`, `businessURL`
- Updated `cmd/main.go` — replaced `BrowserFactory` lambda with `BrowserPool` + `poolAdapter`
- `handler.go` already had `BrowserPool` interface from prior state — no changes needed

## Verification

- `go build ./...` compiles without errors (GOWORK=off)
- All 4 `TestWithRetry_*` tests pass
- Old stub files deleted
- Old `Browser` struct removed from `browser.go`

## Files Changed

- `services/agent-yandex-business/internal/yandex/pool.go` (new)
- `services/agent-yandex-business/internal/yandex/browser.go` (refactored)
- `services/agent-yandex-business/cmd/main.go` (updated wiring)
- `services/agent-yandex-business/internal/yandex/get_reviews.go` (deleted)
- `services/agent-yandex-business/internal/yandex/reply_review.go` (deleted)
- `services/agent-yandex-business/internal/yandex/update_info.go` (deleted)
- `services/agent-yandex-business/internal/yandex/update_hours.go` (deleted)

## Commit

`feat(04-01): BrowserPool with per-business contexts`
