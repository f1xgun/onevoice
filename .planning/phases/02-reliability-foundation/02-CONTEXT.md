# Phase 2: Reliability Foundation - Context

**Gathered:** 2026-03-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Replace panics and silent failures with a consistent error taxonomy and graceful shutdown across all Go services. Introduces NonRetryableError type in pkg/a2a, applies error classification to all agents, adds signal-based graceful shutdown to every service, and removes all panic() calls from production handler constructors.

</domain>

<decisions>
## Implementation Decisions

### Error Taxonomy Design
- Concrete `NonRetryableError` struct with `Is()` method — works with `errors.Is()` for simple checking
- Agent errors surface to user via existing SSE `tool_result` event with `tool_error` string — no new event types
- Per-agent error code mapping: VK Error 5 (invalid token) = permanent, network timeout = transient, rate-limited = backoff

### Graceful Shutdown & Panic Replacement
- Shutdown timeout: 30 seconds, configurable via `SHUTDOWN_TIMEOUT` env var
- Shutdown drain order: HTTP server stop → NATS drain → flush writes → close DB pools (follows dependency chain)
- Constructor panics replaced with `(*Handler, error)` return signatures, errors propagated to main.go — fail on startup not runtime
- SSE proxy silent errors: log + count but continue streaming — add structured logging only, don't break SSE stream

### Claude's Discretion
- Exact VK/Yandex error code → category mapping beyond the examples above
- Whether to use `context.WithTimeout` or `time.AfterFunc` for shutdown timer
- Signal handling implementation details (signal.NotifyContext vs manual channel)
- Whether to add a `Retryable() bool` method on the error or just use `errors.Is` negation

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/a2a/` — A2A framework types, Agent base, NATSExecutor
- `services/agent-yandex-business/internal/yandex/browser.go` — existing `withRetry()` function (needs NonRetryableError integration)
- `services/api/internal/handler/` — existing constructors with panic() calls
- All service `cmd/main.go` files — current startup wiring (no shutdown handling)

### Established Patterns
- Handler → Service → Repository layering with error wrapping via `fmt.Errorf("context: %w", err)`
- Domain sentinel errors in `pkg/domain/errors.go`
- NATS request/reply pattern in `natsexec/executor.go`
- SSE streaming in `chat_proxy.go` with `tool_error` events

### Integration Points
- `pkg/a2a/types.go` — where NonRetryableError type goes
- `services/agent-telegram/internal/agent/handler.go` — apply error classification
- `services/agent-vk/internal/agent/handler.go` — apply error classification
- `services/agent-yandex-business/internal/yandex/browser.go` — integrate with withRetry
- All `cmd/main.go` files — add graceful shutdown signal handling
- All handler constructors — change panic to error return

</code_context>

<specifics>
## Specific Ideas

No specific requirements — standard Go error handling and shutdown patterns apply.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
