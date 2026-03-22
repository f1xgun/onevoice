---
phase: 07-backend-logging-gaps
plan: 02
subsystem: logging
tags: [slog, sse, nats, observability, correlation-id, timing]

requires:
  - phase: 07-backend-logging-gaps
    provides: "correlation ID infrastructure (pkg/logger)"
provides:
  - "SSE write failure logging with correlation_id in orchestrator"
  - "Structured timing logs for NATS tool dispatch (tool, business_id, duration_ms)"
affects: [08-grafana-loki-stack]

tech-stack:
  added: []
  patterns: [slog.ErrorContext for write failures, time.Since timing around NATS requests, WarnContext for agent-reported errors]

key-files:
  created: []
  modified:
    - services/orchestrator/internal/handler/chat.go
    - services/orchestrator/internal/natsexec/executor.go

key-decisions:
  - "Use slog.WarnContext (not Error) for agent-reported tool errors since the NATS transport itself succeeded"
  - "Log event_type on SSE write failure to distinguish which event was lost"

patterns-established:
  - "NATS dispatch timing: start/elapsed/log pattern with duration_ms field"
  - "SSE write error: check fmt.Fprintf return, log with ErrorContext, return early"

requirements-completed: [BLG-03, BLG-05]

duration: 2min
completed: 2026-03-22
---

# Phase 07 Plan 02: Orchestrator Logging Gaps Summary

**SSE write failures now logged with correlation_id and event type; NATS tool dispatch logs timing, tool name, business_id on all code paths**

## Performance

- **Duration:** 2 min
- **Started:** 2026-03-22T08:34:38Z
- **Completed:** 2026-03-22T08:36:22Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- SSE fmt.Fprintf failures are now caught and logged with slog.ErrorContext (carries correlation_id)
- NATS tool dispatch logs duration_ms, tool name, agent, and business_id on success, NATS error, decode error, and agent error paths
- Removed discarded write error pattern (_, _ = fmt.Fprintf)

## Task Commits

Each task was committed atomically:

1. **Task 1: Log SSE write failures with correlation_id** - `8c1b738` (fix)
2. **Task 2: Add structured timing logs to NATS tool dispatch** - `48815c5` (feat)

## Files Created/Modified
- `services/orchestrator/internal/handler/chat.go` - writeSSE now accepts context, logs write and marshal errors with ErrorContext
- `services/orchestrator/internal/natsexec/executor.go` - Structured timing logs on all NATS request paths (Info/Warn/Error)

## Decisions Made
- Used slog.WarnContext for agent-reported tool errors (not ErrorContext) since the NATS transport succeeded -- the error is application-level, not infrastructure
- Added event_type to SSE write failure logs to help identify which event was lost when client disconnects

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Orchestrator logging gaps (BLG-03, BLG-05) are closed
- Grafana/Loki stack (phase 08) can now search for duration_ms, SSE write failed, and correlation_id across orchestrator logs

---
*Phase: 07-backend-logging-gaps*
*Completed: 2026-03-22*
