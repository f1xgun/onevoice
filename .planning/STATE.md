---
gsd_state_version: 1.0
milestone: v1.3
milestone_name: Chats & Projects
current_phase: 19
status: executing
stopped_at: Phase 19 context gathered
last_updated: "2026-04-27T11:10:15.494Z"
last_activity: 2026-04-27
progress:
  total_phases: 5
  completed_phases: 4
  total_plans: 38
  completed_plans: 33
  percent: 87
---

# Project State

**Project:** OneVoice
**Milestone:** v1.3 Chats & Projects
**Current Phase:** 19
**Status:** Executing Phase 19
**Last activity:** 2026-04-27

## Current Position

Phase: 19 (Search & Sidebar Redesign) — EXECUTING
Plan: 1 of 5
Phase 15: COMPLETE — UAT passed 2026-04-19 (all 7 items, zero open gaps).
Phase 16: COMPLETE — UAT passed 2026-04-20 (7/7 items, zero open gaps).

  - Backend goals delivered: POLICY-01..07 + HITL-01..13 (20/20 REQ-IDs).
  - Scope promotion: HITL-L4 (per-tool editable-field whitelist) pulled from v1.4 into v1.3.
  - Pitfall 6 override: parallel fan-out dispatch instead of sequential (D-14 trade recorded).
  - Anti-footgun gates all green: #1-#7 asserted in tests + greps.
  - UX polish: displayName + userDescription chain, `/settings/tools` nav, ProjectForm tabs, simplified create flow, `vk__delete_comment` Forbidden → Manual.
  - One regression caught during UAT: `useChat.ts` ListMessages envelope hydration — closed in `79a906b`.
  - In-scope deferral: inline ToolApprovalCard UX is Phase 17 by planning contract (documented in 16-OVERVIEW.md).

Progress: [██████████] 100% of Phases 15 + 16 (16/16 plans). Phase 17: 0%.

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-18)
**Core value:** Business owners can manage digital presence across platforms through a single conversational interface.
**Current focus:** Phase 19 — Search & Sidebar Redesign

## Milestone Summary

| Phase | Name | Requirements | Status |
|-------|------|--------------|--------|
| 15 | Projects Foundation | 14 (PROJ-01..11, UI-07, UI-10, UI-11) | **COMPLETE** |
| 16 | HITL Backend | 20 (POLICY-01..07, HITL-01..13) | Needs research-phase first (highest technical risk) |
| 17 | HITL Frontend | 2 (UI-08, UI-09) | Depends on Phase 16 SSE contracts |
| 18 | Auto-Title | 9 (TITLE-01..09) | Logically parallel with 17; serialized for planning |
| 19 | Search & Sidebar | 13 (SEARCH-01..07, UI-01..06) | Russian-stemming lighter research pass recommended |

**Coverage:** 58 / 58 v1.3 REQ-IDs mapped.

## Phase 15 Summary

| Plan | Subsystem | Focus | Commits |
|------|-----------|-------|---------|
| 15-01 | Data layer | pkg/domain.Project + WhitelistMode + Conversation fields + Postgres migration + Mongo backfill | `8d9784e`, `7f9fd72`, `1accac5` |
| 15-02 | Orchestrator | prompt layering + tool-whitelist filtering | (see SUMMARY) |
| 15-03 | API | /api/v1/projects CRUD + hard-delete cascade | (see SUMMARY) |
| 15-04 | API | POST /conversations/{id}/move + chat-proxy enrichment | `225aeb3`, `9bff785`, `a67cc62` |
| 15-05 | Frontend | /projects/* pages + WhitelistRadio + ToolCheckboxGrid + DeleteProjectDialog + tools-catalogue.ts | `f67c63d`, `d293f28`, `0d33f1b` |
| 15-06 | Frontend | Sidebar project subtree + move submenu + chips + WhitelistWarningBanner + UI-07 quick-actions | `9506c5c`, `ce79626`, `79ce265` |

## Accumulated Context

### From v1.0

- VK ID tokens cannot call VK API methods — need old-style VK app.
- `metrics.responseWriter` must implement `http.Flusher` for SSE streaming.

### From v1.1

- `slog.ErrorContext(ctx, ...)` over `slog.Error(...)` for all error logging.
- Grafana on port 3001 to avoid conflict with frontend on 3000.
- Observability stack as docker-compose overlay.
- Frontend telemetry is fire-and-forget: errors silently swallowed.

### From v1.2

- Token refresh via refresh-on-read in `GetDecryptedToken()` with `sync.Mutex` per integration ID.
- GBP client creates per-request instances bound to access token (same pattern as VK/Telegram).
- `readMask` / `updateMask` pattern for field-scoped reads and writes.
- Per-API base URL separation for Google Business (v4 reviews/media, v1 info, Performance).

### From Phase 15 (all plans)

- `pkg/domain.Project`, `WhitelistMode` enum, `ProjectRepository.HardDeleteCascade`, `MaxProjectSystemPromptChars=4000` across 3 layers.
- `Conversation` carries `BusinessID`, `ProjectID`, `TitleStatus`, `Pinned`, `LastMessageAt`. `bson:"project_id"` omits `omitempty` (explicit null); JSON `projectId` with `omitempty`.
- Move-chat: `POST /api/v1/conversations/{id}/move` atomic update + best-effort system note. Undo toast intentionally appends a second move-note so the trail is preserved (D-13).
- `lib/tools-catalogue.ts` is a single-file tool catalogue owned by Plan 15-05, consumed unchanged by Plan 15-06's `WhitelistWarningBanner`. Refactored to a live feed in Phase 16 (POLICY-05).
- Hardcoded `QUICK_ACTIONS` in `ChatWindow.tsx` removed; quick actions come from `currentProject?.quickActions ?? DEFAULT_QUICK_ACTIONS` (UI-07).
- Sidebar "Без проекта" renders first (italic muted, `FolderMinus` icon); real projects alphabetical via `localeCompare('ru')`.
- WhitelistWarningBanner dismissal persisted under `projects:whitelistWarning:<businessId>:<integrationId>` in localStorage.

### From Phase 16 (in progress)

- **16-01 (domain + policy resolver) committed** (commits `d6d5c42`, `551837b`): `pkg/domain.ToolFloor` enum with `ValidToolFloor`/`ToolFloorRank` helpers; `Message.Status` tri-state (empty-string-means-complete backward-compat — **no backfill write required**); `ToolCall.ApprovalID` + `ToolCall.Status`; `Business.ToolApprovals()` typed accessor over `Settings["tool_approvals"]` (defensive parsing, skips malformed entries, never panics on nil Settings); `Project.ApprovalOverrides map[string]ToolFloor` JSONB-backed (inherit encoded as KEY ABSENCE, never a literal string — Overview invariant #8); `PendingToolCallBatch` struct with `ProjectID` threaded for TOCTOU re-check in 16-05/16-07; `PendingToolCallRepository` interface declares all 10 atomic primitives.
- **Pure `hitl.Resolve`** in `services/orchestrator/internal/hitl/policy.go` has zero persistence/cache/bus imports (grep-verified; docstring reworded to avoid literal `mongo`/`redis`/`nats` substrings for grep-clean acceptance). Strictest-wins algorithm: `floor → biz layer → project layer`, each `strictest(running, override)` raises but never lowers. Malformed enum values rank `-1` via `ToolFloorRank` so they can never dominate a valid registered floor.
- **Testing gotcha surfaced:** `Project.ID` and `Project.BusinessID` are `uuid.UUID` in `pkg/domain/project.go` (not `string` as the plan snippet showed). Anyone writing new Project tests or repository code must use `uuid.UUID` — PendingToolCallBatch intentionally uses `string` for `ProjectID` because the batch document is Mongo-side (string IDs everywhere on that side).
- **Project.ApprovalOverrides persistence NOT wired in the Postgres project repo yet** — the field has a `db:"approval_overrides"` tag but `services/api/internal/repository/project.go`'s SELECT/INSERT/UPDATE column lists do NOT include `approval_overrides`. Zero-value nil map is safe for 16-01's compile-check; 16-07 must add the column (migration), extend the repo's SQL, and add a JSONB scanner.

### v1.3 Inputs (from research)

- HITL pause/resume is the single largest architectural risk — `Orchestrator.Run` must be refactored into `Run` + `Resume` sharing `stepRun`, with full `ModelMessages` snapshot persisted to `pending_tool_calls` before emitting the SSE pause event.
- Double-execution prevention requires atomic `findOneAndUpdate` status transition + `approval_id` NATS header + Redis dedupe at each agent (TTL 24h).
- Proxy's synthetic `tc-N` tool-call IDs must be replaced with the LLM's real `ToolCall.ID` before HITL ships; SSE scanner buffer bumped 64KB → 1MB in `chat_proxy.go`.
- Mongo `$text` indexes must opt into `default_language: "russian"` at creation; English stemmer on Russian content silently breaks search.
- Auto-title must use atomic conditional update gated on `title_status ∈ {null, "auto_pending"}`; never log prompt/response bodies (PII).
- Whitelist semantics use a typed `WhitelistMode` enum (`inherit | all | explicit | none`) to eliminate `null` vs `[]` ambiguity.
- Moving a chat between projects appends a visible system note (Option A) rather than silently swapping the system prompt.
- Only one new frontend dep: `@uiw/react-json-view` (pin exact alpha version). No new backend deps, no Temporal, no LangGraph, no Meilisearch.

### Pending Todos

None from Phase 15. Phase 16 kickoff needs:

- `/gsd:research-phase 16` before planning: exact atomic resolve contract + 409 shape, NATS header propagation across agents, resume-after-partial-batch state recovery.

### Blockers/Concerns

- Phase 16 needs `/gsd:research-phase` before planning (see above).
- Phase 19 warrants a lighter research pass on Russian stemming edge cases against a known corpus before declaring search done.
- Google API access still requires pre-approval; v1.2 human E2E verification remains deferred (not a v1.3 blocker).

## Session Continuity

Last session: 2026-04-27T09:27:40.151Z
Stopped at: Phase 19 context gathered
Resume file: .planning/phases/19-search-sidebar-redesign/19-CONTEXT.md

---
*State updated: 2026-04-18 — Phase 15 COMPLETE; all 6 plans delivered; 14 Phase-15 requirements satisfied end-to-end.*
