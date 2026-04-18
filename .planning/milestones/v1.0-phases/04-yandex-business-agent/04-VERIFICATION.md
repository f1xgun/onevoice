# Phase 4 Verification: Yandex.Business Agent

**Date:** 2026-03-20
**Status:** human_needed
**Reason:** PLAN-4.0 (VPS feasibility spike) was intentionally skipped. All code and tests were implemented without live validation against Yandex.Business. VPS deployment and manual end-to-end testing must be performed by a human before this phase can be marked complete.

---

## Phase Goal

> Validate Yandex RPA feasibility on VPS, then replace all stub tools with working automation via a shared BrowserPool with per-business contexts and session validation.

---

## Requirement Verification

### YBZ-01 — GetReviews RPA tool
**Status:** implemented (not live-validated)

`GetReviews` is defined at line 243 of `services/agent-yandex-business/internal/yandex/pool.go`.
Full RPA implementation: navigates to the Yandex.Business reviews page, runs `checkSessionAndEvict`, scrapes review cards (id, rating, author, text, date), supports pagination via "Load more" button with fallback selectors. Limit capped at 50, defaults to 20.

### YBZ-02 — ReplyReview RPA tool
**Status:** implemented (not live-validated)

`ReplyReview` is defined at line 449 of `services/agent-yandex-business/internal/yandex/pool.go`.
Locates review by `data-review-id`, clicks reply button, fills textarea, submits. Non-retryable errors for missing review and unavailable reply form. Session canary runs before any DOM interaction.

### YBZ-03 — UpdateInfo RPA tool
**Status:** implemented (not live-validated)

`UpdateInfo` is defined at line 564 of `services/agent-yandex-business/internal/yandex/pool.go`.
Navigates to `/settings/contacts`, runs session canary, waits for contacts form, fills `phone`/`website`/`description` fields with per-field fallback CSS selectors, clicks save.

### YBZ-04 — UpdateHours RPA tool
**Status:** implemented (not live-validated)

`UpdateHours` is defined at line 722 of `services/agent-yandex-business/internal/yandex/pool.go`.
Parses `hoursJSON` upfront (non-retryable on invalid JSON), navigates to `/settings/hours`, runs session canary, iterates days Mon–Sun via `setDayHours` helper (handles closed toggle and open/close time inputs), clicks save.

### YBZ-05 — BrowserPool with sync.Map
**Status:** implemented

`BrowserPool` struct is defined at line 32 of `services/agent-yandex-business/internal/yandex/pool.go`:

```go
type BrowserPool struct {
    ...
    contexts  sync.Map // businessID -> *pooledContext
    ...
}
```

Lazy Chromium initialization via `ensureBrowser()`. Per-business mutex serializes page access within a context. Background `evictLoop` removes idle contexts after `maxIdle` (default 15 min). `BusinessBrowser` wrapper implements the `YandexBrowser` interface and delegates all 4 methods to the pool.

### YBZ-06 — ErrSessionExpired + checkSession
**Status:** implemented

`ErrSessionExpired` sentinel and `checkSession` are in `services/agent-yandex-business/internal/yandex/canary.go` (lines 14 and 25).

```go
var ErrSessionExpired = errors.New("yandex session expired")

func checkSession(page playwright.Page, expectedURLPrefix string) error { ... }
func checkSessionAndEvict(page playwright.Page, expectedURLPrefix string, pool ContextEvictor, businessID string) error { ... }
```

Detects passport.yandex redirect and unexpected URL patterns. Returns `NonRetryableError` wrapping `ErrSessionExpired`. `checkSessionAndEvict` additionally evicts the browser context from the pool on expiry.

### TST-02 — Unit tests with mocked Playwright
**Status:** implemented (33 tests, all passing)

All four required test files exist:

| File | Location | Tests |
|------|----------|-------|
| `mock_page_test.go` | `services/agent-yandex-business/internal/yandex/` | mockLocator, mockPage, mockBrowserContext |
| `canary_test.go` | `services/agent-yandex-business/internal/yandex/` | 6 tests (valid session, passport redirect, CAPTCHA, unexpected redirect, eviction, no eviction) |
| `pool_test.go` | `services/agent-yandex-business/internal/yandex/` | 9 tests (context reuse, isolation, eviction, close lifecycle) |
| `handler_test.go` | `services/agent-yandex-business/internal/agent/` | 14 tests (all 4 tools success, default limit, token error, unknown tool, session expired, CAPTCHA, review not found, reply form unavailable, playwright timeout, transient error) |

`go test -race -count=1 ./...` and `golangci-lint run` both pass with 0 issues (per SUMMARY 04-05).

---

## ROADMAP Success Criteria Assessment

| # | Criterion | Result |
|---|-----------|--------|
| 1 | Spike: headless Chromium on VPS without anti-bot blocks for 10 consecutive loads | **NOT VALIDATED** — PLAN-4.0 skipped |
| 2 | First tool call after startup has no 2–4s Chromium launch delay; subsequent calls reuse shared context | **Implemented** — BrowserPool lazy init + context reuse verified by pool tests |
| 3 | Multi-user: each business gets isolated cookies; concurrent requests don't interfere | **Implemented** — per-business `pooledContext` with mutex; verified by pool isolation tests |
| 4 | User can ask for Yandex.Business reviews and receive real data | **Implemented code** — not validated against live Yandex.Business DOM |
| 5 | User can reply to a review via chat and it appears on Yandex.Business | **Implemented code** — not validated against live Yandex.Business DOM |
| 6 | User can update business info and hours via chat | **Implemented code** — not validated against live Yandex.Business DOM |
| 7 | Expired session cookies return clear `ErrSessionExpired` rather than cryptic failure | **Implemented and unit-tested** |
| 8 | All 4 tools pass unit tests covering success, session expired, selector-not-found | **Implemented** — 33 tests pass |

---

## PLAN-4.0 — VPS Spike (Skipped)

**Status:** human_needed

This plan was intentionally skipped per `04-CONTEXT.md` decision:

> Skip VPS spike (PLAN-4.0) in autonomous execution — implement all code + mocked tests. Mark VPS feasibility validation as deferred manual step in VERIFICATION.md.

**Manual validation required before Phase 4 can be declared complete:**

1. Deploy `services/agent-yandex-business` to a VPS
2. Run `playwright install chromium` in the deployment environment
3. Inject real Yandex.Business session cookies via the integration token API
4. Execute each of the 4 tools against a test Yandex.Business account and confirm results
5. Verify anti-bot measures (fingerprinting, CAPTCHA gates) do not block headless Chromium during normal usage
6. Verify Chromium launches once on first call and is reused on subsequent calls (check timing)
7. Verify that expired cookies produce the `ErrSessionExpired` message in the chat UI
8. Document selector accuracy against the live Yandex.Business DOM and note any selectors that need updating
9. If persistent anti-bot blocking occurs, descope YBZ-01–YBZ-04 to v2 per the ROADMAP gate

---

## Files Verified

- `services/agent-yandex-business/internal/yandex/pool.go` — BrowserPool, BusinessBrowser, all 4 tool implementations
- `services/agent-yandex-business/internal/yandex/canary.go` — ErrSessionExpired, checkSession, checkSessionAndEvict, ContextEvictor interface
- `services/agent-yandex-business/internal/yandex/canary_test.go` — 6 canary unit tests
- `services/agent-yandex-business/internal/yandex/pool_test.go` — 9 pool lifecycle tests
- `services/agent-yandex-business/internal/yandex/mock_page_test.go` — mock Playwright types
- `services/agent-yandex-business/internal/agent/handler_test.go` — 14 handler dispatch tests
