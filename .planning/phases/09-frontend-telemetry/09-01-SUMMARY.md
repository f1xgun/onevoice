---
phase: 09-frontend-telemetry
plan: 01
subsystem: api, ui
tags: [telemetry, slog, cors, axios, sendBeacon, batching]

# Dependency graph
requires:
  - phase: 08-grafana-loki-stack
    provides: Promtail picks up structured JSON from stdout
provides:
  - POST /api/v1/telemetry endpoint for frontend event ingestion
  - Frontend telemetry module with batched sending
  - X-Correlation-ID capture in Axios error interceptor
affects: [09-frontend-telemetry]

# Tech tracking
tech-stack:
  added: []
  patterns: [fire-and-forget telemetry, sendBeacon fallback, lazy import for circular deps]

key-files:
  created:
    - services/api/internal/handler/telemetry.go
    - services/api/internal/handler/telemetry_test.go
    - services/frontend/lib/telemetry.ts
  modified:
    - services/api/internal/router/router.go
    - services/api/cmd/main.go
    - services/frontend/lib/api.ts

key-decisions:
  - "Lazy dynamic import in api.ts to break circular dependency with telemetry.ts"
  - "sendBeacon for page unload instead of async fetch (works during navigation)"

patterns-established:
  - "Telemetry fire-and-forget: catch errors silently, never break user flow"
  - "Lazy import for circular module dependencies in frontend"

requirements-completed: [FLG-01, FLG-02, FLG-03]

# Metrics
duration: 3min
completed: 2026-03-22
---

# Phase 09 Plan 01: Telemetry Pipeline Summary

**POST /api/v1/telemetry handler with slog structured logging, frontend batched telemetry client with sendBeacon fallback, and X-Correlation-ID error capture in Axios interceptor**

## Performance

- **Duration:** 3 min
- **Started:** 2026-03-22T09:05:36Z
- **Completed:** 2026-03-22T09:08:10Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments
- Telemetry ingestion endpoint accepts event batches (max 100), logs each event as structured JSON via slog.InfoContext with correlation_id from request context
- Frontend telemetry module batches events (5s interval or 50-event threshold) with sendBeacon fallback on page hide
- Axios error interceptor captures X-Correlation-ID from error responses and tracks api_error events, with telemetry endpoint excluded to prevent infinite loops
- CORS ExposedHeaders updated to include X-Correlation-ID so browser JS can read it

## Task Commits

Each task was committed atomically:

1. **Task 1: POST /api/v1/telemetry handler (TDD RED)** - `9d86f72` (test)
2. **Task 1: POST /api/v1/telemetry handler (TDD GREEN)** - `c0cb66d` (feat)
3. **Task 2: Frontend telemetry module + error capture** - `15fadfc` (feat)

## Files Created/Modified
- `services/api/internal/handler/telemetry.go` - TelemetryHandler with Ingest method, logs events via slog
- `services/api/internal/handler/telemetry_test.go` - 4 unit tests covering valid, invalid, empty, and oversized batches
- `services/api/internal/router/router.go` - Added Telemetry to Handlers, POST route, CORS X-Correlation-ID
- `services/api/cmd/main.go` - Wired NewTelemetryHandler into Handlers struct
- `services/frontend/lib/telemetry.ts` - trackEvent/flushTelemetry with batching and sendBeacon
- `services/frontend/lib/api.ts` - X-Correlation-ID capture in error interceptor

## Decisions Made
- Used lazy dynamic `import('./telemetry')` in api.ts to break circular dependency (telemetry.ts imports api from api.ts)
- sendBeacon used for page unload flush instead of async fetch (more reliable during navigation)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Circular dependency between api.ts and telemetry.ts**
- **Found during:** Task 2 (frontend telemetry module)
- **Issue:** telemetry.ts imports `api` from api.ts; plan called for static `import { trackEvent }` in api.ts which would create circular import
- **Fix:** Used lazy `import('./telemetry')` dynamic import inside the error interceptor
- **Files modified:** services/frontend/lib/api.ts
- **Verification:** TypeScript compiles, ESLint passes, no circular dependency warnings
- **Committed in:** 15fadfc (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Essential fix for circular dependency. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Telemetry pipeline complete: frontend events flow to backend slog output
- Promtail (from Phase 08) will pick up structured JSON logs and forward to Loki
- Ready for dashboard queries in Grafana filtering on "frontend_event" log entries

## Self-Check: PASSED

All files exist, all commits verified, all acceptance criteria met.

---
*Phase: 09-frontend-telemetry*
*Completed: 2026-03-22*
