# Project State

**Project:** OneVoice
**Milestone:** v1.1 Observability & Debugging
**Current Phase:** Phase 7 — Backend Logging Gaps (not started)
**Status:** Roadmap created, ready to plan Phase 7
**Last activity:** 2026-03-22 — Roadmap created

## Project Reference
See: .planning/PROJECT.md (updated 2026-03-22)
**Core value:** Business owners can manage digital presence across platforms through a single conversational interface
**Current focus:** Observability & debugging improvements

## Phase Progress

| Phase | Name | Requirements | Status |
|-------|------|-------------|--------|
| 7 | Backend Logging Gaps | BLG-01..06 | Not started |
| 8 | Grafana + Loki Stack | LOG-01..03 | Not started |
| 9 | Frontend Telemetry | FLG-01..03 | Not started |

## Accumulated Context

### From v1.0
- VK ID tokens (both user and service key) cannot call VK API methods — need old-style VK app
- Community tokens can write (wall.post, groups.edit) but cannot read (wall.get, wall.getComments)
- metrics.responseWriter must implement http.Flusher for SSE streaming
- chi Recoverer middleware does NOT break Flusher (it was metrics middleware)
- 16 logging gaps identified in v1.0 audit (4 critical, 6 medium, 6 low)

---
*State updated: 2026-03-22 — Roadmap created*
