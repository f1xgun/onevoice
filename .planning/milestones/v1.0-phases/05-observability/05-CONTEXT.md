# Phase 5: Observability - Context

**Gathered:** 2026-03-20
**Status:** Ready for planning

<domain>
## Phase Boundary

Add observability infrastructure: health check endpoints (/health/live, /health/ready) for all services, Prometheus metrics on API and orchestrator, structured JSON logging via pkg/logger, and correlation ID middleware with NATS propagation.

</domain>

<decisions>
## Implementation Decisions

### Claude's Discretion
All implementation choices are at Claude's discretion — pure infrastructure phase.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- All service `cmd/main.go` files — already have graceful shutdown from Phase 2
- `services/api/internal/middleware/` — existing middleware chain (auth, CORS, security headers, rate limiting)
- `services/api/internal/router/router.go` — chi router setup
- `pkg/` — shared package for cross-service utilities

### Established Patterns
- chi middleware chain for HTTP middleware
- slog for logging (currently text output, needs JSON)
- NATS request/reply with ToolRequest/ToolResponse types
- Config via environment variables

### Integration Points
- All `cmd/main.go` files — health check routes
- `services/api/internal/router/router.go` — metrics and correlation middleware
- `pkg/logger/` — new or existing logger package
- NATS ToolRequest — add correlation ID field

</code_context>

<specifics>
## Specific Ideas

No specific requirements — infrastructure phase.

</specifics>

<deferred>
## Deferred Ideas

None

</deferred>
