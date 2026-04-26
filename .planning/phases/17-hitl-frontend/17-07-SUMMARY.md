---
phase: 17-hitl-frontend
plan: 07
subsystem: backend
tags: [go, mongodb, hitl, regression, chi, gap-closure, orchestrator, api]
gap_closure: true
closes_gap: GAP-03

# Dependency graph
requires:
  - phase: 16-hitl
    plan: 06
    provides: chat_proxy.go orchReq forwarding shape (pre-Phase-16 baseline)
  - phase: 16-hitl
    plan: 07
    provides: hitl.Resolve business-scoped auth check (now reachable after 17-07)
provides:
  - "services/orchestrator/internal/handler/chat.go — chatRequest decodes user_id, message_id, tier, business_approvals, project_approval_overrides; chi.URLParam extracts conversationID; RunRequest carries all five fields end-to-end"
  - "services/api/internal/handler/chat_proxy.go — orchReq forwards user_id (JWT subject), message_id (just-saved userMsg.ID), tier, business_approvals (Business.ToolApprovals()), project_approval_overrides (proj.ApprovalOverrides) on every fresh-turn request"
  - "services/orchestrator/internal/repository/pending_tool_call.go — InsertPreparing rejects empty conversation_id / business_id with descriptive errors (regression net for any future wire-break)"
  - "services/orchestrator/internal/handler/chat_test.go — TestChatHandler_ThreadsPhase16Fields covers valid UUID, empty user_id, invalid user_id"
  - "services/api/internal/handler/chat_proxy_test.go — TestChatProxy_ForwardsPhase16Fields covers with-project (all five keys populated) and without-project ({} not null)"
  - "services/orchestrator/internal/repository/pending_tool_call_test.go — TestInsertPreparing_RejectsEmptyConversationID + RejectsEmptyBusinessID + HappyPath"
affects: [17-04, 17-05, 17-06, 17-08, 17-09, 17-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Defensive nil-coalesce on outbound JSON maps so the wire shape is always {} not null (matches the project_allowed_tools symmetry from Plan 15-04)"
    - "userMsg.ID assigned via uuid.NewString() BEFORE messageRepo.Create — guarantees a stable known message_id can be forwarded to the orchestrator and later reference a Message that is already on disk by the time the pause SSE fires (D-17 anti-footgun symmetry)"
    - "uuid.Parse on inbound user_id with WarnContext on failure (never panic on bad proxy input — coerce to uuid.Nil and continue, mirroring the whitelist_mode pattern from Plan 15-02)"
    - "Repository structural-floor guards (ConversationID + BusinessID required at InsertPreparing) — the two structural floors that make HITL-11 hydration filter and resolve-time auth check non-noop. UserID + MessageID intentionally NOT guarded (system flows may have empty UserID)"

key-files:
  created: []
  modified:
    - services/orchestrator/internal/handler/chat.go
    - services/orchestrator/internal/handler/chat_test.go
    - services/api/internal/handler/chat_proxy.go
    - services/api/internal/handler/chat_proxy_test.go
    - services/orchestrator/internal/repository/pending_tool_call.go
    - services/orchestrator/internal/repository/pending_tool_call_test.go

key-decisions:
  - "Repository guards on ConversationID + BusinessID only (NOT UserID / MessageID). System / anonymous flows may legitimately have empty UserID (no JWT subject); the HITL-11 hydration filter is keyed solely on conversation_id + status, and the resolve auth check uses business_id. Adding guards on UserID/MessageID would over-constrain and break legitimate edge cases."
  - "userMsg.ID is set explicitly (uuid.NewString()) BEFORE messageRepo.Create instead of relying on the repository to assign one. This makes message_id forwarded to the orchestrator unambiguous and matches the D-17 invariant that PendingToolCallBatch.MessageID references a Message that is on disk at SSE pause time."
  - "tier forwarded as empty string (\"\"). The v1.3 backend has no tier model yet, but the orchestrator's RunRequest declares the field for forward-compat. Sending \"\" is harmless and keeps the wire shape complete now."
  - "Defensive nil-coalesce of project_approval_overrides + business_approvals to map[string]domain.ToolFloor{} instead of nil. JSON marshals nil maps as null; the orchestrator's chatRequest expects an object. The {} shape matches the project_allowed_tools symmetry already established in Plan 15-04."

requirements-completed: [HITL-01, HITL-11]

# Metrics
completed: 2026-04-24
---

# Phase 17 Plan 07: GAP-03 Backend Regression Closure Summary

**Closes the Phase-16 wire-break that left every persisted PendingToolCallBatch with empty conversation_id / business_id / user_id / message_id. The orchestrator's HTTP handler now extracts conversationID via chi.URLParam and decodes five new Phase-16 body fields (user_id, message_id, tier, business_approvals, project_approval_overrides), threading them all into RunRequest. The API proxy's orchReq map carries the same five keys on every fresh-turn request, with defensive nil-coalesce so the wire shape is {} not null. The repository's InsertPreparing now rejects empty ConversationID / BusinessID with descriptive errors — a regression net for any future re-break of either chat handler or proxy. Six new tests across three files cover the wiring + the guard.**

## What Was Built

### Task 1 — Orchestrator chat handler (commit `fa994ee`)

`services/orchestrator/internal/handler/chat.go`:
- Added `chi` + `uuid` imports
- Extended `chatRequest` struct with five new JSON fields (`user_id`, `message_id`, `tier`, `business_approvals`, `project_approval_overrides`)
- Inside `Chat()`: extract `conversationID := chi.URLParam(r, "conversationID")` immediately after JSON decode
- `runReq` now populates all eight Phase-16 identity / policy fields (`ConversationID`, `BusinessID`, `ProjectID`, `UserIDString`, `MessageID`, `Tier`, `BusinessApprovals`, `ProjectApprovalOverrides`) plus the parsed UUID `runReq.UserID`
- Bad UUIDs log + leave `UserID = uuid.Nil` (defensive — never panic on bad proxy input)

`services/orchestrator/internal/handler/chat_test.go`:
- `TestChatHandler_ThreadsPhase16Fields` — three sub-tests: valid UUID populates RunRequest correctly; empty user_id leaves UserID zero; invalid user_id leaves UserID zero without panic

### Task 2 — API proxy forwarding (commits `66bb581` RED + `5917a64` GREEN)

`services/api/internal/handler/chat_proxy.go`:
- `userMsg.ID` assigned via `uuid.NewString()` before `messageRepo.Create` so message_id is stable
- Project-resolution branch now also captures `proj.ApprovalOverrides` into `projectApprovalOverrides`
- Defensive nil-coalesce: `businessApprovals` from `business.ToolApprovals()` (always non-nil) and `projectApprovalOverrides` materialized to empty map when no project
- `orchReq` map extended with five new keys (`user_id`, `message_id`, `tier`, `business_approvals`, `project_approval_overrides`)

`services/api/internal/handler/chat_proxy_test.go`:
- `TestChatProxy_ForwardsPhase16Fields` — two sub-tests: with-project (all five keys present + populated, message_id matches userMsg.ID); without-project (project_approval_overrides marshals as {} not null, business_approvals also {})

### Task 3 — Repository regression guard (commits `66f8499` RED + `fcba2a8` GREEN)

`services/orchestrator/internal/repository/pending_tool_call.go`:
- `InsertPreparing` now rejects empty `ConversationID` (error contains `"conversation_id is required"`) and empty `BusinessID` (error contains `"business_id is required"`) before any Mongo write
- UserID + MessageID intentionally NOT guarded (system flows may have empty UserID)

`services/orchestrator/internal/repository/pending_tool_call_test.go`:
- `TestInsertPreparing_RejectsEmptyConversationID` — confirms error mentions conversation_id and no document persisted
- `TestInsertPreparing_RejectsEmptyBusinessID` — same shape for business_id
- `TestInsertPreparing_HappyPath` — fully-populated batch inserts and round-trips

## The Five Fields Now Flowing API → Orchestrator → Mongo

| Field | Source (proxy) | Wire key | Orchestrator field | Persisted on PendingToolCallBatch |
|-------|----------------|----------|---------------------|-----------------------------------|
| `user_id` | `userID.String()` from `middleware.GetUserID` | `user_id` | `RunRequest.UserIDString` + parsed `UserID uuid.UUID` | `state.UserID` → `batch.user_id` |
| `message_id` | `userMsg.ID` (assigned via `uuid.NewString()` before Create) | `message_id` | `RunRequest.MessageID` | `state.MessageID` → `batch.message_id` |
| `tier` | `""` (reserved; no tier model in v1.3) | `tier` | `RunRequest.Tier` | (not persisted on batch — RunState only) |
| `business_approvals` | `business.ToolApprovals()` (typed accessor) | `business_approvals` | `RunRequest.BusinessApprovals` | (consulted by hitl.Resolve at pause time) |
| `project_approval_overrides` | `proj.ApprovalOverrides` (or `{}` when no project) | `project_approval_overrides` | `RunRequest.ProjectApprovalOverrides` | (consulted by hitl.Resolve at pause time) |

Plus the URL-extracted `conversationID` from `chi.URLParam` → `RunRequest.ConversationID` → `state.ConversationID` → `batch.conversation_id`.

## Repository Guards Added

Two structural-floor checks at `InsertPreparing`:

1. `b.ConversationID == ""` → `pending_tool_call: conversation_id is required (regression of plan 17-07 gap-03)`
2. `b.BusinessID == ""` → `pending_tool_call: business_id is required (regression of plan 17-07 gap-03)`

Both fire before any Mongo write, so a rejected batch leaves zero documents on disk (verified by `CountDocuments` in the tests).

## Verification

All automated checks green:
- `cd services/orchestrator && GOWORK=off go test -race ./...` — all packages pass
- `cd services/api && GOWORK=off go test -race ./...` — all packages pass
- `cd services/orchestrator && golangci-lint run --config ../../.golangci.yml ./...` — 0 issues
- `cd services/api && golangci-lint run --config ../../.golangci.yml ./...` — 0 issues
- Repository regression tests run with `MONGO_TEST_URI=mongodb://localhost:27017` against the local docker container — pre-fix RED (no error returned), post-fix GREEN (descriptive error + zero documents persisted)

## Live Verification (Task 4 — Checkpoint)

Task 4 is a `checkpoint:human-verify` task. The action is purely observational — Tasks 1–3 already wrote the code. The operator must drive a fresh paused turn through the live stack and verify:

1. Mongo `db.pending_tool_calls.findOne({status:"pending"})` shows non-empty `conversation_id`, `business_id`, `user_id`, `message_id` (compare to VERIFICATION.md §GAP-03 — all four were `""` before this plan)
2. Page reload mid-approval re-renders the approval card (HITL-11 hydration is now reachable)
3. Submit → `POST /pending-tool-calls/{batch_id}/resolve` returns 200 (the 403-due-to-empty-business_id regression is closed)
4. Resume SSE stream opens and the assistant message completes
5. `TestInsertPreparing_RejectsEmpty*` repository regression passes

This checkpoint runs against the live stack after the wave merge — the parallel executor cannot drive a docker compose flow against shared services. The orchestrator coordinates the human-verify pass post-merge.

## Deviations from Plan

None. Tasks 1–3 executed exactly as specified in 17-07-PLAN.md:

- Task 1 frontmatter `<action>` (a)–(e) all applied; chatRequest grew the five fields with the exact comment block, conversationID extracted via chi.URLParam, runReq carries all eight Phase-16 fields plus parsed UserID UUID
- Task 2 (TDD) RED test fails as predicted before fix → GREEN after fix; orchReq carries the five keys with the exact source mapping per the plan; messageID is captured via the userMsg.ID-up-front pattern the plan recommended
- Task 3 (TDD) repository guards rejecting ConversationID + BusinessID only (per plan's explicit instruction to NOT guard UserID/MessageID); error strings contain `conversation_id` / `business_id` as the done-criteria require

## GAP-03 Closure Confirmation

Per `17-VERIFICATION.md §GAP-03`, the regression's three structural causes are now closed:

1. **API → orchestrator forward** — chat_proxy.go forwards user_id, message_id, tier, business_approvals, project_approval_overrides on every fresh-turn request (was: missing all five)
2. **Orchestrator request decoding** — chat.go decodes the five body fields and extracts conversationID from the URL via chi.URLParam (was: never read)
3. **RunRequest construction** — runReq now carries all eight Phase-16 identity + policy fields (was: empty defaults)

The repository guard added in Task 3 prevents silent regression of either point #1 or #2 in the future — any future wire-break will fail loud at the persistence boundary instead of silently corrupting Mongo state.

## Self-Check

Files created/modified:
- `services/orchestrator/internal/handler/chat.go` — FOUND (commit `fa994ee`)
- `services/orchestrator/internal/handler/chat_test.go` — FOUND (commit `fa994ee`)
- `services/api/internal/handler/chat_proxy.go` — FOUND (commit `5917a64`)
- `services/api/internal/handler/chat_proxy_test.go` — FOUND (commit `66bb581`)
- `services/orchestrator/internal/repository/pending_tool_call.go` — FOUND (commit `fcba2a8`)
- `services/orchestrator/internal/repository/pending_tool_call_test.go` — FOUND (commit `66f8499`)

Commits in worktree:
- `fa994ee` — Task 1: orchestrator handler
- `66bb581` — Task 2 RED: failing proxy test
- `5917a64` — Task 2 GREEN: proxy forwarding
- `66f8499` — Task 3 RED: failing repository tests
- `fcba2a8` — Task 3 GREEN: repository guard

## Self-Check: PASSED
