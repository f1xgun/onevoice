# Phase 4: Yandex.Business Agent - Context

**Gathered:** 2026-03-19
**Status:** Ready for planning

<domain>
## Phase Boundary

Replace stub Yandex.Business tools with working RPA automation. Build shared BrowserPool with per-business browser contexts, session canary validation, and implement 4 tools (get_reviews, reply_review, update_info, update_hours). VPS feasibility spike deferred to manual validation — code and mocked tests built first.

</domain>

<decisions>
## Implementation Decisions

### Spike & Gate Handling
- Skip VPS spike (PLAN-4.0) in autonomous execution — implement all code + mocked tests
- Mark VPS feasibility validation as deferred manual step in VERIFICATION.md
- If anti-bot blocks later discovered during manual testing, tools descope to v2 per ROADMAP gate

### RPA Selector Strategy
- CSS selectors with data-testid when available, semantic selectors as fallback
- Document all selectors in code comments for maintenance when Yandex DOM changes
- Yandex.Business pages are Russian-language — selectors should not depend on text content

### BrowserPool Design
- One shared Chromium browser instance, multiple contexts (one per business)
- `sync.Map` for context cache keyed by business ID
- Contexts carry per-business cookies (session isolation)
- Lazy initialization: browser launched on first request, not at startup
- Context cleanup on session expiry or explicit close

### Mocked Playwright Testing
- Interface-based: define `Page` interface with methods used (Goto, Locator, TextContent, Fill, Click, WaitForSelector)
- Mock implementation returns predefined HTML/values for success paths
- Error scenarios: session expired (login redirect), selector not found (DOM change), network timeout
- BrowserPool lifecycle tests: context creation, reuse, cleanup, concurrent access

### Claude's Discretion
- Exact CSS selectors for Yandex.Business pages (will need maintenance)
- BrowserPool max idle time before context cleanup
- Whether to use Playwright's `page.WaitForLoadState` or custom waits
- Review pagination strategy (scroll-based vs link-based)

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `services/agent-yandex-business/internal/yandex/browser.go` — existing withRetry, withPage, humanDelay, setCookies, canary check pattern
- `services/agent-yandex-business/internal/yandex/browser_test.go` — withRetry tests (from Phase 2)
- `services/agent-yandex-business/internal/agent/handler.go` — existing NATS handler with classifyYandexError
- `pkg/a2a/types.go` — NonRetryableError for session expiry

### Established Patterns
- RPA agents use `withRetry` + `withPage` + `humanDelay` + canary check
- Session cookies from encrypted integration tokens
- Tool naming: `yandex_business__get_reviews`, `yandex_business__reply_review`, etc.

### Integration Points
- `services/agent-yandex-business/internal/yandex/` — BrowserPool and tool implementations
- `services/agent-yandex-business/internal/agent/handler.go` — tool dispatch
- `services/orchestrator/internal/tools/registry.go` — tool registration
- `services/orchestrator/cmd/main.go` — tool wiring

</code_context>

<specifics>
## Specific Ideas

No specific requirements — standard Playwright RPA patterns apply.

</specifics>

<deferred>
## Deferred Ideas

- VPS feasibility spike (PLAN-4.0) — manual validation required
- Yandex.Business photo upload (YBZ-07) — v2
- Yandex.Business analytics (YBZ-08) — v2

</deferred>
