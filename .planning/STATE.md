---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: Google Business Profile
current_phase: "10"
status: defining requirements
last_updated: "2026-04-08"
last_activity: 2026-04-08
progress:
  total_phases: 0
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
---

# Project State

**Project:** OneVoice
**Milestone:** v1.2 Google Business Profile
**Current Phase:** Not started (defining requirements)
**Status:** Defining requirements
**Last activity:** 2026-04-08 — Milestone v1.2 started

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-08)
**Core value:** Business owners can manage digital presence across platforms through a single conversational interface
**Current focus:** Google Business Profile API integration

## Phase Progress

| Phase | Name | Requirements | Status |
|-------|------|-------------|--------|
| (pending roadmap) | | | |

## Accumulated Context

### From v1.0

- VK ID tokens (both user and service key) cannot call VK API methods — need old-style VK app
- Community tokens can write (wall.post, groups.edit) but cannot read (wall.get, wall.getComments)
- metrics.responseWriter must implement http.Flusher for SSE streaming
- chi Recoverer middleware does NOT break Flusher (it was metrics middleware)
- 16 logging gaps identified in v1.0 audit (4 critical, 6 medium, 6 low)

### From Phase 07

- slog.ErrorContext(ctx, ...) over slog.Error(...) for all error logging — preserves correlation_id via ContextHandler
- Telegram sync functions return errors for per-operation AgentTask status tracking (sync_title, sync_description, sync_photo)
- Rate limiter confirmed using r.Context() — no context.Background() (BLG-06)

### From Phase 08

- Grafana on port 3001 to avoid conflict with frontend on 3000
- Observability stack as docker-compose overlay: `docker compose -f docker-compose.yml -f docker-compose.observability.yml up`
- Promtail uses Docker socket service discovery for automatic container log collection
- Grafana provisioning via YAML files in observability/grafana/provisioning/
- Grafana dashboards provisioned as JSON: Request Trace (Loki) and Metrics Overview (Prometheus)
- Datasource referenced by name string ("Loki", "Prometheus") for provisioned datasources

### From Phase 09

- Lazy dynamic import in api.ts to break circular dependency with telemetry.ts
- Frontend telemetry is fire-and-forget: errors silently swallowed, never breaks user flow
- sendBeacon used for page unload flush (more reliable than async fetch during navigation)
- page_view tracking gated on auth ready state to avoid tracking pre-redirect navigations
- trackClick in mutation onSuccess callbacks for accurate tracking of successful actions only

---
*State updated: 2026-04-08 — Milestone v1.2 Google Business Profile started.*
