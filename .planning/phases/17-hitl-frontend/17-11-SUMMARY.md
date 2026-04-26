---
phase: 17-hitl-frontend
plan: 11
type: execute
gap_closure: true
closes_gap: GAP-04
status: complete-pending-live-verify
verified: 2026-04-26
key-files:
  modified:
    - pkg/domain/repository.go
    - services/orchestrator/internal/orchestrator/step.go
    - services/orchestrator/internal/orchestrator/step_test.go
    - services/api/internal/service/hitl.go
    - services/api/internal/service/hitl_test.go
    - services/api/internal/handler/hitl.go
    - services/api/internal/handler/hitl_test.go
---

# 17-11 — GAP-04 Closure Summary

## Outcome

Two distinct root causes of GAP-04 closed end-to-end. Tasks 1 and 2
auto-completed; Task 3 (live human-verify) is run by the orchestrator
post-merge against the rebuilt Docker stack.

## Tasks

| Task | Status | Commit |
|------|--------|--------|
| 1. Persist `FloorAtPause` on `PendingCall`; consume from persisted batch in `service.HITLService.Resolve` | ✅ done | `3cbc7b8` |
| 2. Allow `status=resolving` in API `Resume` handler; reject only `status=resolved` (terminal) and `status=expired` (410) | ✅ done | `471439b` |
| 3. Live human-verify: send HITL message → click Approve → click Submit → expect resolve 200 + resume 200 + Telegram post visible | pending (orchestrator) | — |

## Root Causes Closed

### Cause 1 — Pause/resolve registry divergence

**Pre-fix:**
- Pause-time classifier (`services/orchestrator/internal/orchestrator/step.go:148-163`)
  reads `ToolFloor` from the orchestrator's in-process `tools.Registry` —
  always warm.
- Resolve-time classifier (`services/api/internal/service/hitl.go:294`)
  reads from `service.ToolsRegistryCache.Floor()` — HTTP-backed, lazily
  warmed by `GET /api/v1/tools` only.
- A cold cache returns `ToolFloorForbidden` for every tool, so the
  `pkghitl.Resolve(...)` call in `Resolve` triggers the HITL-06
  policy-revoked rewrite branch on every legitimate approve/edit.

**Fix (Task 1):**
- Added `FloorAtPause domain.ToolFloor` field to `PendingCall`
  (`pkg/domain/repository.go`), `bson:"floor_at_pause,omitempty"` so legacy
  batches still decode cleanly with empty value.
- Orchestrator's `buildPendingBatch` (`step.go`) now sets
  `FloorAtPause: domain.ToolFloorManual` on every persisted call —
  always correct because only manual-floor calls reach the manual-calls
  bucket (auto/forbidden are partitioned upstream).
- `services/api/internal/service/hitl.go:Resolve` now reads
  `c.FloorAtPause` instead of calling `s.toolsCache.Floor(c.ToolName)`.
- The orchestrator-side resume goroutine
  (`services/orchestrator/internal/orchestrator/resume.go:dispatchApprovedCalls`)
  remains the load-bearing TOCTOU primitive — defense in depth survives.

### Cause 2 — Resume handler 409 gate too strict

**Pre-fix:**
- `services/api/internal/handler/hitl.go:271-279` returned 409 whenever
  `batch.Status == "resolving"`.
- But `service.HITLService.Resolve` atomically transitions
  `pending → resolving` and the orchestrator's resume goroutine is the
  ONLY writer that transitions `resolving → resolved` (via `MarkResolved`
  after dispatch completes).
- Therefore the legitimate first resume call ALWAYS finds the batch in
  `resolving` and was always blocked. Pre-GAP-03 fix this was masked by
  the 403 short-circuit; once Plan 17-07 fixed the 403, every approval
  flow regressed to a 409 dead-end.

**Fix (Task 2):**
- Removed the `status == "resolving"` rejection.
- Added `status == "resolved"` rejection (true terminal conflict — the
  batch has already been dispatched).
- `status == "expired"` keeps its existing 410 Gone response.
- Per-call double-dispatch protection lives in the orchestrator's
  per-call `MarkDispatched` idempotence guard, not at the api boundary.

## Tests

- `services/orchestrator/internal/orchestrator/step_test.go`:
  `+57 lines` — assert `FloorAtPause == ToolFloorManual` is set on every
  persisted `PendingCall` for `manual`-floor LLM tool calls.
- `services/api/internal/service/hitl_test.go`: `+99 / -17 lines` —
  asserts `Resolve` consults `c.FloorAtPause` instead of the toolsCache;
  cold-cache scenarios no longer false-positive into `policy_revoked`.
- `services/api/internal/handler/hitl_test.go`: `+65 / -9 lines` —
  replaces `TestResume_BatchResolving_Returns409` with
  `TestResume_BatchResolving_Allowed` (200, forwards to orchestrator);
  adds `TestResume_BatchResolved_Returns409` (true terminal conflict).

## Verification

- `cd services/orchestrator && GOWORK=off go test -race ./...` — all
  packages pass
- `cd services/api && GOWORK=off go test -race ./...` — all packages pass
- Manual live verification (Task 3) is pending; orchestrator drives this
  against the rebuilt Docker stack.

## Files Modified

```
pkg/domain/repository.go                            +20 / -3
services/orchestrator/internal/orchestrator/step.go +8 /  -0
services/orchestrator/internal/orchestrator/step_test.go +57 / 0
services/api/internal/service/hitl.go                +12 / -1
services/api/internal/service/hitl_test.go           +84 / -15
services/api/internal/handler/hitl.go                +14 / -8
services/api/internal/handler/hitl_test.go           +51 / -9
```
