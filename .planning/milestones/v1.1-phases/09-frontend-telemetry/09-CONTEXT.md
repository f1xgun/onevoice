# Phase 9: Frontend Telemetry - Context

**Gathered:** 2026-03-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Add frontend telemetry: structured event logging for user actions, correlation_id forwarding from API error responses, and a backend telemetry ingestion endpoint. Events are batched/debounced to avoid UI impact.

</domain>

<decisions>
## Implementation Decisions

### Claude's Discretion
All implementation choices are at Claude's discretion — pure infrastructure phase.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `services/frontend/lib/api.ts` — Axios instance with interceptors (request: auth token, response: 401 refresh). Good place to add correlation_id capture from error responses.
- `services/frontend/hooks/useChat.ts` — SSE parsing hook, example of custom hook pattern
- Axios interceptors already handle all request/response lifecycle — can capture correlation_id in error interceptor
- `services/api/internal/middleware/correlation.go` — Backend already sets X-Correlation-ID header on responses

### Established Patterns
- Zustand for global state, React Query for server data
- `"use client"` only when needed (hooks/events)
- `function` declarations, typed props interfaces
- API proxy rewrites: `/api/v1/*` → API service

### Integration Points
- New `POST /api/v1/telemetry` endpoint in API service (Go handler + router)
- New `lib/telemetry.ts` module in frontend for event collection + batched sending
- Axios response interceptor to capture X-Correlation-ID on errors
- Telemetry endpoint writes JSON to stdout → picked up by Promtail/Loki (from Phase 8)

</code_context>

<specifics>
## Specific Ideas

No specific requirements — infrastructure phase.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
