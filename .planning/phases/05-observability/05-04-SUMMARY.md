---
plan: "05-04"
title: "Correlation ID middleware and NATS propagation"
status: complete
completed: "2026-03-20"
---

## Summary

Implemented end-to-end correlation ID propagation across all services. A unique correlation ID is generated (or preserved from incoming `X-Correlation-ID` header) at the API gateway and flows through the orchestrator into platform agents via NATS.

## Changes

### Task 1: Correlation ID middleware (`services/api/internal/middleware/correlation.go`)
- New `CorrelationID()` middleware: reads `X-Correlation-ID` from request header or generates UUID, stores in context via `logger.WithCorrelationID`, echoes in response header.

### Task 2: Wired into API router (`services/api/internal/router/router.go`)
- Added `middleware.CorrelationID()` to both `Setup` and `SetupInternal` middleware chains, immediately after `chimiddleware.RequestID`.

### Task 3: Chat proxy forwards header (`services/api/internal/handler/chat_proxy.go`)
- After building the proxy request to orchestrator, sets `X-Correlation-ID` header from context.

### Task 4: Orchestrator extracts header (`services/orchestrator/internal/handler/chat.go`)
- Extracts `X-Correlation-ID` from incoming request and stores in context via `logger.WithCorrelationID`.

### Task 5: NATS propagation (`services/orchestrator/internal/natsexec/executor.go`)
- Populates `ToolRequest.RequestID` from `logger.CorrelationIDFromContext(ctx)`.

### Task 6: Agent-side extraction (`pkg/a2a/agent.go`)
- After unmarshaling `ToolRequest`, injects `req.RequestID` as correlation ID into handler context.

### Task 7: Orchestrator router middleware (`services/orchestrator/cmd/main.go`)
- Added inline correlation ID middleware to chi router for non-chat routes.

### Task 8: Unit tests
- `services/api/internal/middleware/correlation_test.go`: 4 tests covering generation, preservation, context injection.
- `services/orchestrator/internal/natsexec/executor_test.go`: 2 new tests for `RequestID` propagation from context.

## Verification

- [x] `cd services/api && GOWORK=off go build ./...` — passes
- [x] `cd services/orchestrator && GOWORK=off go build ./...` — passes
- [x] `cd pkg && go build ./...` — passes
- [x] `cd services/api && GOWORK=off go test -race ./internal/middleware/...` — passes
- [x] `cd services/orchestrator && GOWORK=off go test -race ./internal/natsexec/...` — passes

## Flow

```
Client → API (middleware generates/preserves X-Correlation-ID)
       → Chat proxy (forwards header to orchestrator)
       → Orchestrator (extracts header, stores in context)
       → NATSExecutor (sets ToolRequest.RequestID from context)
       → NATS → Agent (extracts RequestID, stores as correlation ID in handler context)
```
