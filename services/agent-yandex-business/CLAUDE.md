# services/agent-yandex-business/ — Yandex.Business RPA Agent

Listens on NATS subject `tasks.yandex_business`, automates Yandex.Business via Playwright browser automation (RPA).

## Architecture

```
cmd/main.go                  → wiring (NATS, Playwright browser, A2A handler)
internal/
├── agent/handler.go         → A2A handler: dispatches tool calls to Yandex client
└── yandex/
    ├── browser.go           → Playwright browser pool, withPage, withRetry, humanDelay
    ├── get_reviews.go       → Fetch reviews from Yandex.Business
    ├── reply_review.go      → Reply to a review
    ├── update_hours.go      → Update business hours
    └── update_info.go       → Update business info
```

## Environment Variables

- `YANDEX_COOKIES_JSON` (required) — session cookies as JSON array
- `NATS_URL` — NATS server (default: localhost:4222)

## Tool Names

All tools prefixed with `yandex_business__`:
- `yandex_business__get_reviews`
- `yandex_business__reply_review`
- `yandex_business__update_hours`
- `yandex_business__update_info`

## RPA Patterns

This agent uses browser automation, not API calls. Key patterns:

- **`withRetry`** — retry with exponential backoff + canary check between attempts
- **`withPage`** — acquire page from pool, execute action, release
- **`humanDelay`** — random 500-1500ms delay to avoid bot detection
- **Canary check** — verify session is still valid before retrying

## Build & Test

```bash
cd services/agent-yandex-business && GOWORK=off go test -race ./...
cd services/agent-yandex-business && golangci-lint run --config ../../.golangci.yml ./...
```
