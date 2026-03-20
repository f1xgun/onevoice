# Phase 4: Yandex.Business Agent — Research

**Date:** 2026-03-19
**Requirements:** YBZ-01, YBZ-02, YBZ-03, YBZ-04, YBZ-05, YBZ-06, TST-02

---

## 1. Current State

### Existing Code

**`browser.go`** contains the foundational RPA primitives:
- `Browser` struct — holds `cookiesJSON` string, creates fresh Playwright instance per call
- `withPage(ctx, fn)` — launches Playwright, creates Chromium browser+context, injects cookies, runs `fn(page)`, captures screenshot on error
- `setCookies(bCtx)` — parses JSON cookie array into `playwright.OptionalCookie` slice
- `humanDelay()` — random 1-4s sleep
- `withRetry(ctx, maxAttempts, fn)` — exponential backoff (2^i seconds), respects `NonRetryableError` and context cancellation

**Stub tool files** (`get_reviews.go`, `reply_review.go`, `update_info.go`, `update_hours.go`):
- All four follow identical structure: `withRetry` wrapping `withPage` wrapping `page.Goto` + `humanDelay` + TODO comment
- `reply_review`, `update_info` return hardcoded "not yet implemented" errors
- `get_reviews` returns empty slice (no scraping logic)
- `update_hours` has a partial canary check (`[data-testid='hours-form'], .hours-editor`) but no form interaction

**`handler.go`** is production-ready:
- `YandexBrowser` interface already defined with all 4 methods
- `BrowserFactory func(cookiesJSON string) YandexBrowser` for DI
- `classifyYandexError` detects session expiry ("passport.yandex", "login redirect") and CAPTCHA
- Tool dispatch switch is complete for all 4 tools
- Argument extraction handles type assertions with defaults

**`cmd/main.go`** wiring is complete:
- Token fetched via `tokenclient` from API service
- `BrowserFactory` creates `yandex.NewBrowser(cookies)` per call (current: no pooling)
- NATS transport + A2A agent lifecycle correct

**`orchestrator/cmd/main.go`** tool registration is complete:
- All 4 tools registered with Russian-language descriptions and correct parameter schemas

### What Needs Replacing

1. **`Browser.withPage`** — currently calls `playwright.Run()` + `pw.Chromium.Launch()` on every single tool call. Must be replaced with `BrowserPool` that reuses a single browser instance (YBZ-05).
2. **All 4 stub tool implementations** — must implement actual DOM interaction (YBZ-01 through YBZ-04).
3. **Session canary check** — must be extracted into a reusable function called before every page action, returning `NonRetryableError` on session expiry (YBZ-06).
4. **Tests** — only `withRetry` is tested. Need mocked Playwright tests for all 4 tools + canary (TST-02).

---

## 2. BrowserPool Design

### Architecture

```
BrowserPool
├── pw          *playwright.Playwright   // singleton, lazy-init on first call
├── browser     playwright.Browser       // one Chromium instance
├── contexts    sync.Map                 // key: businessID -> *pooledContext
├── mu          sync.Mutex               // protects pw/browser init
└── maxIdle     time.Duration            // context eviction threshold (e.g. 15 min)

pooledContext
├── ctx         playwright.BrowserContext
├── lastUsed    atomic.Int64             // unix millis
└── cookies     string                   // for re-init after eviction
```

### Lifecycle

1. **Lazy init:** First call to `BrowserPool.WithPage(businessID, cookiesJSON, fn)` acquires `mu`, launches Playwright + Chromium if nil, stores in struct fields. Subsequent calls skip init.
2. **Per-business context:** `contexts.LoadOrStore(businessID, newContext)` — each business gets its own `BrowserContext` with isolated cookies. If stored context is found, reuse it. If not, create new context with `browser.NewContext(...)` and inject cookies.
3. **Reuse:** The same `BrowserContext` is reused across calls for the same business. This preserves session cookies across tool invocations without re-injecting them every time.
4. **Eviction:** A background goroutine (or check-on-access) compares `lastUsed` against `maxIdle`. Expired contexts are closed and removed from the map.
5. **Shutdown:** `BrowserPool.Close()` iterates all contexts, closes them, closes browser, stops Playwright.

### Key Decisions

- **One browser, many contexts** — Chromium contexts share the same process but have independent cookie jars. This is the standard Playwright pattern for multi-tenant isolation.
- **`sync.Map`** — chosen over `map + sync.RWMutex` because access pattern is "read-heavy, write-rare" (contexts created once, reused many times).
- **Cookie refresh:** When a context is evicted and re-created, cookies must be re-injected from the stored `cookiesJSON`. The `BrowserFactory` in `handler.go` will be replaced with a `BrowserPool` reference.

### Impact on `handler.go`

Replace `BrowserFactory func(cookiesJSON string) YandexBrowser` with a `BrowserPool` that implements the `YandexBrowser` interface methods directly, or provide a `GetBrowser(businessID, cookiesJSON) YandexBrowser` method that returns a business-scoped wrapper. The second approach is cleaner because it preserves the existing `YandexBrowser` interface and handler dispatch logic.

Proposed flow:
```
handler.getBrowser(ctx, req)
  → tokens.GetToken(ctx, businessID, "yandex_business", "")
  → pool.ForBusiness(businessID, cookiesJSON) → returns *BusinessBrowser (implements YandexBrowser)
```

`BusinessBrowser` wraps the pool and businessID, delegates `GetReviews` / `ReplyReview` / `UpdateInfo` / `UpdateHours` to the pool's shared page management.

---

## 3. Session Canary

### Detection Strategy

After every `page.Goto(targetURL)`, before any DOM interaction, run a canary check:

```go
func checkSession(page playwright.Page, expectedURLPrefix string) error {
    currentURL := page.URL()
    // 1. Redirect to Yandex Passport login page
    if strings.Contains(currentURL, "passport.yandex") {
        return a2a.NewNonRetryableError(fmt.Errorf("session expired: redirected to %s", currentURL))
    }
    // 2. URL changed to something unexpected (e.g. error page, CAPTCHA)
    if !strings.HasPrefix(currentURL, expectedURLPrefix) {
        return a2a.NewNonRetryableError(fmt.Errorf("unexpected redirect: %s", currentURL))
    }
    // 3. Optional: check for a known DOM element that only appears when logged in
    // e.g., a user avatar or business name in the header
    return nil
}
```

### Why URL-Based

- Yandex.Business redirects unauthenticated requests to `passport.yandex.ru/auth` — this is the primary signal
- DOM-based canary (looking for a logged-in indicator) is fragile and language-dependent
- URL check is fast (no DOM query) and reliable

### Integration with `withRetry`

The canary returns `NonRetryableError`, so `withRetry` stops immediately on session expiry. No wasted retries on dead sessions.

### Context Eviction on Expiry

When canary detects expiry, the pool should evict that business's context from `sync.Map` so the next call creates a fresh one (with potentially refreshed cookies from the API).

---

## 4. Tool Implementations

### 4.1 `get_reviews` (YBZ-01)

**URL:** `https://business.yandex.ru/reviews`

**Flow:**
1. Navigate to reviews page
2. Run session canary (`checkSession(page, businessURL)`)
3. `humanDelay()`
4. Wait for reviews container to appear (CSS selector for the reviews list)
5. Query all review card elements
6. For each card (up to `limit`), extract:
   - Rating: star count or numeric rating from a data attribute or aria-label
   - Author name: text content of author element
   - Review text: text content of review body
   - Date: text content or datetime attribute of date element
   - Review ID: `data-review-id` attribute or derive from element index
7. Return `[]map[string]interface{}` with keys: `id`, `rating`, `author`, `text`, `date`

**Selector strategy:**
- Reviews list container: `[data-testid='reviews-list'], .reviews-list, [class*='ReviewsList']`
- Individual review: `[data-testid='review-card'], .review-card, [class*='ReviewCard']`
- Fallback: use `page.QuerySelectorAll` with multiple candidate selectors, try each until one returns results

**Pagination:** Yandex.Business reviews page likely uses scroll-based pagination (infinite scroll) or a "Load more" button. Strategy:
- After initial load, check if a "Load more" / "Show more" button exists
- If so, click it and wait for new items until `limit` is reached
- If scroll-based, use `page.Evaluate` to scroll to bottom and wait for new elements
- Cap at `limit` to avoid unbounded scraping

### 4.2 `reply_review` (YBZ-02)

**URL:** `https://business.yandex.ru/reviews`

**Flow:**
1. Navigate to reviews page
2. Run session canary
3. `humanDelay()`
4. Locate the review by `reviewID` — look for element with matching `data-review-id` or index
5. Within that review card, find the "Reply" button and click it
6. `humanDelay()`
7. Wait for reply textarea/input to appear
8. Fill the textarea with `text` using `page.Fill` or `locator.Fill`
9. `humanDelay()`
10. Click the "Submit" / "Send" button
11. Wait for confirmation (reply appears in the thread, or a success toast)

**Error cases:**
- Review not found → `NonRetryableError` ("review not found")
- Reply textarea not found (already replied?) → `NonRetryableError` ("reply form unavailable")

### 4.3 `update_info` (YBZ-03)

**URL:** `https://business.yandex.ru/settings/contacts` (or `/settings/edit`)

**Flow:**
1. Navigate to contacts settings
2. Run session canary
3. `humanDelay()`
4. Wait for the settings form to load
5. For each field in the `info` map (`phone`, `website`, `description`):
   - Find the corresponding input/textarea by label, name attribute, or `data-testid`
   - Clear existing value (`locator.Fill("")` then `locator.Fill(newValue)`)
   - `humanDelay()` between fields
6. Click the "Save" button
7. Wait for save confirmation (success toast, button state change, or URL change)

**Field mapping:**
- `phone` → input near phone label/icon
- `website` → input near website/URL label
- `description` → textarea near description label

### 4.4 `update_hours` (YBZ-04)

**URL:** `https://business.yandex.ru/settings/hours`

**Flow:**
1. Navigate to hours settings
2. Run session canary
3. `humanDelay()`
4. Wait for hours form to load (existing partial canary already checks for this)
5. Parse `hoursJSON` — expected format:
   ```json
   {
     "monday": {"open": "09:00", "close": "18:00"},
     "tuesday": {"open": "09:00", "close": "18:00"},
     ...
     "sunday": null  // closed
   }
   ```
6. For each day of the week:
   - Find the row for that day (by index or day label)
   - If value is `null`, toggle the "closed" checkbox/switch
   - Otherwise, fill open/close time inputs
   - `humanDelay()` between days
7. Click "Save"
8. Wait for save confirmation

**Complexity:** Hours forms often have complex UI (time pickers, toggles, dropdowns). Implementation will need to handle:
- Time input might be a text input, a dropdown, or a custom picker
- "Round the clock" (24h) toggle
- Break times within a day

---

## 5. Interface-Based Playwright Mocking

### Approach

Define minimal interfaces that mirror the Playwright API methods actually used by tool code:

```go
// page.go (interfaces file)

type PlaywrightPage interface {
    Goto(url string, opts ...playwright.PageGotoOptions) (playwright.Response, error)
    URL() string
    Locator(selector string, opts ...playwright.PageLocatorOptions) playwright.Locator
    QuerySelectorAll(selector string) ([]playwright.ElementHandle, error)
    Screenshot(opts ...playwright.PageScreenshotOptions) ([]byte, error)
    WaitForSelector(selector string, opts ...playwright.PageWaitForSelectorOptions) (playwright.ElementHandle, error)
    Close(opts ...playwright.PageCloseOptions) error
}
```

However, the `playwright.Page` return type from `bCtx.NewPage()` is already an interface in `playwright-go`. The challenge is that `withPage` currently creates the page internally.

### Recommended Structure

Instead of mocking at the Playwright level, mock at the `YandexBrowser` interface level (already defined in `handler.go`):

```go
type YandexBrowser interface {
    UpdateHours(ctx context.Context, hoursJSON string) error
    UpdateInfo(ctx context.Context, info map[string]string) error
    GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error)
    ReplyReview(ctx context.Context, reviewID, text string) error
}
```

This is the **handler-level mock** — tests in `agent/handler_test.go` verify that the handler correctly:
- Fetches tokens
- Calls the right browser method
- Classifies errors
- Returns correct ToolResponse

For **browser-level tests** (testing the actual DOM interaction logic), introduce a `PageFactory` interface:

```go
// In yandex package
type PageExecutor interface {
    WithPage(ctx context.Context, businessID, cookiesJSON string, fn func(page playwright.Page) error) error
}
```

Then each tool method (`GetReviews`, etc.) receives the page via callback and operates on it. The mock provides a fake `playwright.Page` that returns predetermined values.

Since `playwright-go` v0.4501.1 uses interface types for `Page`, `Locator`, `ElementHandle`, etc., they can be mocked directly. Create a `mockPage` struct implementing only the methods used:

```go
type mockPage struct {
    playwright.Page // embed for unused methods (will panic if called)
    url     string
    locators map[string]*mockLocator
}

func (m *mockPage) URL() string { return m.url }
func (m *mockPage) Goto(url string, opts ...playwright.PageGotoOptions) (playwright.Response, error) {
    m.url = url
    return nil, nil
}
func (m *mockPage) Locator(selector string, opts ...playwright.PageLocatorOptions) playwright.Locator {
    if loc, ok := m.locators[selector]; ok {
        return loc
    }
    return &mockLocator{err: fmt.Errorf("selector not found: %s", selector)}
}
```

### Test Coverage Plan (TST-02)

1. **`get_reviews`** — mock page returns HTML with 3 review cards, verify extracted data matches
2. **`reply_review`** — mock page verifies Fill and Click were called with correct args
3. **`update_info`** — mock page verifies each field is cleared and filled
4. **`update_hours`** — mock page verifies day rows are updated correctly
5. **Canary check** — mock page URL returns `passport.yandex.ru/auth`, verify `NonRetryableError`
6. **BrowserPool lifecycle** — test context creation, reuse (same businessID), eviction, Close()

---

## 6. Integration with `withRetry` and `NonRetryableError`

### Current Pattern (Preserved)

Each tool method wraps its logic in `withRetry(ctx, 3, fn)`. Inside `fn`, the canary check or any permanent failure returns `NonRetryableError`, causing `withRetry` to bail immediately.

### Error Classification in `handler.go`

`classifyYandexError` already handles:
- Session expiry → `NonRetryableError`
- CAPTCHA → `NonRetryableError`
- Everything else → transient (retried)

This is called in the handler *after* the browser method returns, providing a second layer of classification. The canary inside the browser method provides the first layer.

### New Error Cases

Add to `classifyYandexError`:
- "review not found" → `NonRetryableError` (no point retrying)
- "reply form unavailable" → `NonRetryableError`
- Selector timeout after canary passes → transient (DOM slow to load, retry may help)

### `withRetry` Change

No changes needed to `withRetry` itself. It correctly handles `NonRetryableError` via `errors.Is`. The exponential backoff (1s, 2s, 4s) is appropriate for transient browser timeouts.

---

## 7. Dependencies and Ordering

### Plan Execution Order

```
PLAN-4.1: BrowserPool          (no deps — foundational)
    │
    ├─→ PLAN-4.2: Session canary   (depends on 4.1 — canary operates on pooled page)
    │       │
    │       ├─→ PLAN-4.3: get_reviews + reply_review  (depends on 4.1 + 4.2)
    │       │
    │       └─→ PLAN-4.4: update_info + update_hours   (depends on 4.1 + 4.2)
    │
    └─→ PLAN-4.5: Mocked tests   (depends on 4.1–4.4, all implementations must exist)
```

### File Dependencies

| Plan | Files Created/Modified |
|------|----------------------|
| PLAN-4.1 | `yandex/pool.go` (new), `yandex/browser.go` (refactor withPage), `cmd/main.go` (wire pool) |
| PLAN-4.2 | `yandex/canary.go` (new), `yandex/browser.go` (integrate canary into withPage) |
| PLAN-4.3 | `yandex/get_reviews.go` (replace stub), `yandex/reply_review.go` (replace stub) |
| PLAN-4.4 | `yandex/update_info.go` (replace stub), `yandex/update_hours.go` (replace stub) |
| PLAN-4.5 | `yandex/mock_page_test.go` (new), `yandex/*_test.go` (new test files), `agent/handler_test.go` (new) |

### External Dependencies

- `playwright-go v0.4501.1` — already in `go.mod`, no version change needed
- `playwright install chromium` — must be run in Docker/VPS at deploy time (not at build time)
- No new Go module dependencies required

---

## 8. Risks

### DOM Fragility (HIGH)

Yandex.Business is a private web application that can change its DOM structure at any time without notice. Every CSS selector used is a potential breakpoint.

**Mitigation:**
- Use `data-testid` attributes when available (less likely to change)
- Fall back to structural selectors (`div > span:first-child`) over class names
- Document all selectors in code comments with date of last verification
- Log the full selector and page URL on failure for fast diagnosis
- Screenshot capture on error (already implemented in `withPage`)

### Anti-Bot Detection (HIGH)

Yandex employs anti-bot measures including:
- Browser fingerprinting (headless detection)
- Behavioral analysis (mouse patterns, typing speed)
- CAPTCHA challenges
- IP-based rate limiting

**Mitigation:**
- `--disable-blink-features=AutomationControlled` flag (already set)
- `humanDelay()` between interactions (already implemented)
- Realistic User-Agent string (already set)
- Low request frequency (tool calls are user-initiated, not automated bulk)
- VPS with residential-adjacent IP (deployment decision)
- CAPTCHA detection returns `NonRetryableError` (already implemented)
- If anti-bot blocks are persistent, tools descope to v2 per ROADMAP gate

### Session Cookie Lifetime (MEDIUM)

Yandex session cookies have an unknown expiration window. Could be hours, days, or until explicit logout.

**Mitigation:**
- Canary check before every action (YBZ-06) — fast detection
- Clear error message to user: "Yandex session expired, please re-authenticate"
- Context eviction from pool on expiry — fresh context on next call
- Future: could implement cookie refresh flow if Yandex supports it

### Concurrent Access to Same Business (LOW)

Two simultaneous tool calls for the same business could conflict on the same `BrowserContext` (e.g., both navigating to different pages).

**Mitigation:**
- Serialize access per business via a per-context mutex in `pooledContext`
- NATS request/reply is inherently sequential per agent (one handler goroutine per message), but the orchestrator could issue parallel tool calls
- Alternative: create a new page per call within the same context (pages are isolated within a context)

### Playwright Binary Size (LOW)

Chromium download is ~200MB. Affects Docker image size and cold start time.

**Mitigation:**
- Use Playwright's `install chromium` (not all browsers)
- Multi-stage Docker build with browser pre-installed
- BrowserPool lazy init means cold start only on first request, not at boot

---

## 9. Validation Architecture

### Unit Test Validation (TST-02)

Tests mock at two levels:

**Level 1: Handler tests** (`agent/handler_test.go`)
- Mock `YandexBrowser` interface — verify handler routing, error classification, response format
- Mock `TokenFetcher` — verify token fetch errors propagate correctly
- No Playwright dependency — pure Go unit tests

**Level 2: Browser logic tests** (`yandex/*_test.go`)
- Mock `playwright.Page` interface — verify DOM interaction logic (selectors queried, values extracted, buttons clicked)
- Test canary detection: mock page URL returning `passport.yandex.ru`, verify `NonRetryableError`
- Test selector fallback: mock primary selector returning empty, verify fallback selector is tried
- Test pagination: mock review list with "load more" button, verify multiple pages fetched

### BrowserPool Tests

- **Init:** verify lazy initialization — pool starts with nil browser, first call initializes
- **Context reuse:** two calls with same businessID get same context (verify `sync.Map` hit)
- **Context isolation:** two calls with different businessIDs get different contexts
- **Eviction:** set maxIdle to 1ms, wait, verify context is evicted
- **Close:** verify all contexts and browser are closed
- **Concurrent access:** goroutine safety with `sync.Map` and per-context mutex

### Manual Validation (Deferred)

VPS feasibility spike is deferred per 04-CONTEXT.md. Manual testing checklist:
- Deploy to VPS with residential IP
- Inject real Yandex session cookies
- Execute each tool against a test Yandex.Business account
- Verify anti-bot measures do not block headless Chromium
- Measure session cookie lifetime under automated usage
- Document selector accuracy against live DOM

---

## RESEARCH COMPLETE
