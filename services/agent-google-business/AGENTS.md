# services/agent-google-business/ — Google Business Profile Agent

Listens on NATS subject `tasks.google_business`, calls Google Business Profile API for review management.

> **Status: unverified.** This agent is written but has not been exercised end-to-end against live Google Business Profile accounts. Unlike `agent-yandex-business` (which is verified), treat this service as experimental — expect auth/OAuth refresh, locationName format, and error-classification details to need tuning on first live run.
>
> Historical note: earlier project docs stated "Google Business is NOT MVP." The code was written anyway. Scope status should be clarified with the user before any production use.

## Architecture

```
cmd/main.go                  → wiring (NATS, token fetcher, A2A handler)
internal/
├── agent/handler.go         → A2A handler: dispatches tool calls, classifies Google API errors
├── config/                  → env-based config
└── gbp/
    ├── client.go            → Google Business Profile HTTP client
    └── types.go             → Request/response types (Review, ReviewReply, ListReviewsResponse, ...)
```

## Environment Variables

- `NATS_URL` — NATS server (default: localhost:4222)
- API tokens are fetched per-request via the `TokenFetcher` interface from the API service's internal endpoint (OAuth access tokens, refreshed centrally)

## Tools

Tool dispatch lives in `internal/agent/handler.go` (switch on `req.Tool`); registration for the LLM is in `services/orchestrator/cmd/main.go` (search for `"google_business__"`). Read those files for the current tool set.

All tool names are prefixed with `google_business__`.

## Token Flow

Unlike other agents, this one does not hold a long-lived token in env. It asks the API service for the decrypted OAuth access token per request via `TokenFetcher.GetToken(ctx, businessID, "google_business", "")`. The returned `ExternalID` is the location resource name (e.g. `accounts/X/locations/Y`) used as the target for GBP API calls.

## A2A Pattern + Error Classification

Same shape as the other platform agents (NATS → `ToolRequest` → switch → `ToolResponse`). Additionally, `classifyGBPError` wraps permanent failures (401/403/404, `PERMISSION_DENIED`, `UNAUTHENTICATED`, `NOT_FOUND`) as `a2a.NonRetryableError` so the A2A base agent doesn't retry them.

## Build & Test

```bash
cd services/agent-google-business && GOWORK=off go test -race ./...
cd services/agent-google-business && golangci-lint run --config ../../.golangci.yml ./...
```
