# services/agent-yandex-business/ — Yandex.Business RPA Agent

Listens on NATS subject `tasks.yandex_business`, automates Yandex.Business via Playwright browser automation (RPA).

**Status:** verified working end-to-end (the one RPA agent we've actually tested against a live Yandex.Business instance).

## Architecture

```
cmd/main.go                       → wiring (NATS, Playwright browser, A2A handler;
                                     consumes pkg/agentbase for token/dispatch/dedupe)
internal/
├── agent/handler.go              → A2A handler: routes tool calls (switch) and
│                                   delegates HITL dedupe to agentbase.Dispatcher
└── yandex/
    ├── browser.go                → Playwright browser lifecycle
    ├── pool.go                   → BrowserPool lifecycle ONLY (page acquire/release)
    ├── session.go                → Cookie + Yandex OAuth session exchange / injection
    ├── business_browser.go       → BusinessBrowser struct + ForBusiness factory
    ├── tool_get_reviews.go       → RPA: list reviews
    ├── tool_reply_review.go      → RPA: post owner reply
    ├── tool_get_info.go          → RPA: read business info
    ├── tool_update_info.go       → RPA: update business info
    ├── tool_update_hours.go      → RPA: update opening hours
    ├── tool_create_post.go       → RPA: publish a post
    ├── tool_upload_photo.go      → RPA: upload a photo
    ├── tool_list_companies.go    → RPA: enumerate accessible companies
    ├── helpers.go                → Shared free functions (withRetry, withPage, humanDelay)
    ├── canary.go                 → Session-validity check before actions
    ├── constants.go              → Selectors, timeouts, URL constants
    └── *_test.go                 → Unit tests (Playwright-mocked)
```

## Environment Variables

- `YANDEX_COOKIES_JSON` (required) — session cookies as JSON array
- `NATS_URL` — NATS server (default: localhost:4222)

## Tools

Tool dispatch lives in `internal/agent/handler.go` (switch on `req.Tool`); implementations are methods on `BusinessBrowser` in `internal/yandex/pool.go`; tool *registration* with descriptions is in `services/orchestrator/cmd/main.go` (search for `"yandex_business__"`). Read those for the current tool set.

All tool names are prefixed with `yandex_business__`.

## RPA Patterns

This agent uses browser automation, not API calls. Key patterns:

- **`withRetry`** — retry with exponential backoff + canary check between attempts
- **`withPage`** — acquire page from pool, execute action, release
- **`humanDelay`** — random 500–1500ms delay to avoid bot detection
- **Canary check** (`canary.go`) — verify the session is still logged in before running a tool; fail fast if cookies expired
- **Screenshot-on-error** — diagnostics written to `rpa-screenshots/` on failures
- **Resilient selectors** — CSS/XPath with fallback strategy, since the DOM changes

## Build & Test

```bash
cd services/agent-yandex-business && GOWORK=off go test -race ./...
cd services/agent-yandex-business && golangci-lint run --config ../../.golangci.yml ./...
```
