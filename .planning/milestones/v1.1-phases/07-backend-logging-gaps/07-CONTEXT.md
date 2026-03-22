# Phase 7: Backend Logging Gaps - Context

**Gathered:** 2026-03-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Close all 6 backend logging gaps identified in v1.0 audit: silent errors, context loss, missing logs across API and orchestrator services.

</domain>

<decisions>
## Implementation Decisions

### Claude's Discretion
All implementation choices are at Claude's discretion — pure infrastructure phase.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/logger/correlation.go` — WithCorrelationID / CorrelationIDFromContext helpers
- `pkg/logger/context_handler.go` — ContextHandler auto-injects correlation_id into slog records
- Structured logging via `log/slog` already in use across all services

### Established Patterns
- `slog.Error(msg, "key", val, ...)` used throughout — but many calls use `slog.Error` instead of `slog.ErrorContext` which loses correlation_id
- `context.Background()` used in `chat_proxy.go:96` for async persistence (already re-attaches corrID)
- `context.Background()` used in `sync.go:88` for fire-and-forget (no request context)
- Rate limiter already uses `r.Context()` (ratelimit.go lines 42, 120) — BLG-06 may already be satisfied

### Integration Points
- `services/api/internal/handler/chat_proxy.go` — SSE proxy, scanner.Err() check at line 256
- `services/orchestrator/internal/handler/chat.go` — writeSSE discards fmt.Fprintf errors (line 148)
- `services/orchestrator/internal/natsexec/executor.go` — no timing/structured logs on tool dispatch
- `services/api/internal/platform/sync.go` — fire-and-forget sync with recordTask helper

</code_context>

<specifics>
## Specific Ideas

No specific requirements — infrastructure phase.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
