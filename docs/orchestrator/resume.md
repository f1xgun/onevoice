# Orchestrator: HITL Resume

`services/orchestrator/internal/orchestrator/resume.go` continues a paused
agent turn from its persisted `PendingToolCallBatch` snapshot. Pause is
emitted by `stepRun` in `orchestrator.go`; resume is its mirror: load batch,
hydrate `RunState`, dispatch approved calls (parallel fan-out, TOCTOU
re-check, Redis-dedupe friendly), then re-enter `stepRun` at
`Iter = batch.IterationIdx + 1`.

See [`docs/orchestrator/run.md`](./run.md) for `Event` shapes and
`stepRun` lifecycle.

## Public API

- `type ResumeRequest` — fresh state passed to `Resume` at
  approval-resolution time. Fields:
  - `BatchID` — `PendingToolCallBatch._id` to resume.
  - `BusinessApprovals`, `ProjectApprovalOverrides` — the FRESH
    approval maps re-fetched from Postgres at resume time.
  - `ActiveIntegrations`, `WhitelistMode`, `AllowedTools` — recompute
    the LLM-visible tool set against the current project whitelist.
  - `Model`, `Tier` — preserved from the original request so
    post-resume iterations keep routing to the same provider tier.
- `Resume(ctx, req) (<-chan Event, error)` — returns a fresh event
  channel; the spawned goroutine runs `resumeGoroutine`.

The "fresh" qualifier on the approval maps is load-bearing for TOCTOU: the
caller (resolve handler + `chat_proxy`) re-fetches
`business.settings.tool_approvals` and `project.approval_overrides` from
Postgres at the moment of resume — they may have changed since the batch
was persisted. `dispatchApprovedCalls` re-runs `hitl.Resolve` against THESE
maps, never the maps embedded in the snapshot.

## Lifecycle

The spawned goroutine performs the following steps in order:

1. **Load batch** via `pendingRepo.GetByBatchID`. Emit `EventError` on
   missing or expired batches without advancing state. Expired batches
   surface as `Content="approval_expired"` so the API proxy can map the
   sentinel to a specific HTTP status.
2. **Reconstruct `RunState`** from the snapshot (`batch.ModelMessages`) —
   this is what makes pause survive process restarts.
3. **Inject `batch.BusinessID` into the dispatch context** via
   `a2a.WithBusinessID` so the NATS executor's
   `a2a.BusinessIDFromContext` lookup picks it up. The handler entrypoint
   for `/chat` does this on the regular path (`handler/chat.go`), but
   the resume handler does not — without this line, every HITL-approved
   tool call reaches platform agents with `business_id=""` and fails
   token resolution.
4. **Dispatch approved calls** in parallel (Redis dedupe absorbs retries)
   with TOCTOU re-check via `hitl.Resolve` against the FRESH approval
   maps in `ResumeRequest`. Forbidden-after-pause → synthetic
   `policy_revoked` rejection, no dispatch.
5. **Skip calls with `Dispatched==true`** — orchestrator crash-recovery
   (belt-and-suspenders with the agent's Redis SetNX dedupe).
6. **Reject verdicts** → synthetic rejection message + `tool_rejected`
   event, no NATS dispatch.
7. **MarkResolved** is called after all parallel dispatches complete.
   Best-effort — marking is hygienic, not load-bearing. If the mark
   fails the goroutine still continues `stepRun`: the conversation state
   is already correct; the batch will eventually be reaped by the TTL /
   reconciliation.
8. **Re-enter `stepRun`** at `Iter = batch.IterationIdx + 1`.

## Snapshot decoding

`decodeSnapshot` reads `batch.ModelMessages` and returns the `RunState`
fields it carries. Two shapes are accepted:

- **Versioned envelope** —
  `{"v":2,"messages":[...],"system_platform":"...","system_business":"..."}`.
  `Messages` is system-free; `SystemPlatform` / `SystemBusiness` travel
  separately and are wired into `llm.ChatRequest.SystemBlocks` on the next
  `stepRun`.
- **Legacy raw array** — `[{"role":"system",...}, ...]`. `Messages`
  still carries a leading `role:"system"` entry; `SystemPlatform` /
  `SystemBusiness` stay empty so `stepRun` falls through to provider-side
  legacy scrub.

Shape discrimination: a versioned envelope begins with `{` as the first
non-whitespace byte; a legacy raw array begins with `[`. Resume logs a
debug entry on legacy batches so operators can confirm in-flight legacy
batches drained naturally.

`snapshotDecoded` carries the decoded fields so the resume goroutine can
populate `RunState` without juggling a long return list. Legacy V1
snapshots leave most fields zero — the `Legacy` flag tells the caller to
expect a leading system message in `Messages` instead.

`PDnAllowlist` (`pdn_allowlist`) is hydrated so the resumed turn exempts
the same business-owned contact values from the outbound personal-data
scrub as the paused one. Snapshots written before the field existed decode
to nil — the resumed turn then redacts without an allowlist, which is the
old, safe behaviour.

Accumulated token counts (`AccumulatedInputTokens`,
`AccumulatedOutputTokens`) are hydrated so the per-conversation cap
continues to measure from the pre-pause budget. Legacy V1/V2 snapshots
without these fields land at zero — correct because pre-cap turns were
not subject to enforcement.

## Parallel fan-out (`dispatchApprovedCalls`)

Holds a `WaitGroup` to join all in-flight dispatches before returning; a
mutex around `state.Messages` keeps `llm.Message` appends race-safe (the
`go test -race` invariant). Emits `tool_call` / `tool_result` /
`tool_rejected` events in whatever order goroutines finish — the caller
(`chat_proxy` + frontend) associates them by `ToolCallID`, not by arrival
order.

`sendOrCancel` writes an event to the output channel but returns `false`
if `ctx` is canceled first. Mirrors the gate used by `dispatchToolCalls`
in `orchestrator.go` so a caller that hangs up mid-resume cannot leave
the per-call goroutines blocked indefinitely on a full channel buffer.

The pre-loop `ctx.Err()` check bails out early if the caller has gone
away — no point queueing further rejections or spawning goroutines that
will immediately hit the same cancellation gate.

### Per-call branches

- **`Verdict == "reject"`** → synthetic rejection message
  `{"rejected":true,"reason":"<reject_reason or user_rejected>"}`
  appended to `state.Messages`; `EventToolRejected` emitted; no
  dispatch.
- **TOCTOU re-check fails** (`effective == ToolFloorForbidden`) →
  synthetic `policy_revoked` rejection message; `EventToolRejected`
  emitted; no dispatch.
- **`Dispatched == true`** → silently skipped (crash recovery).
- **Edit verdict** → `EditedArgs` merged over the original `Arguments`
  map. The `EditableFields` whitelist was already enforced by the
  resolve handler, so any key present in `EditedArgs` is safe to
  overwrite.
- **Approved** → parallel goroutine emits `tool_call`, runs
  `tools.ExecuteWithApproval(ctx, name, args, "<batch_id>-<call_id>")`,
  appends the JSON-marshaled result (or `{"error":"..."}`) to
  `state.Messages` under the mutex, calls
  `pendingRepo.MarkDispatched` (best-effort log on error — Redis dedupe
  at the agent is the primary safety layer; the Mongo flag is
  belt-and-suspenders), and emits `tool_result`.

`DisplayName` + `DisplayNameKey` are populated on `tool_call` and
`tool_result` events so the `AgentTask` row created on the resume path
also carries the i18n key — matches the fresh-turn path in
`dispatchToolCalls`. This holds even when the row was first created by
a different orchestrator instance.

## Failure modes

- `o.pendingRepo == nil` → `Event{Type: EventError, Content: "HITL not configured"}`,
  channel closed.
- `pendingRepo.GetByBatchID` error →
  `Event{Type: EventError, Content: "batch not found: ..."}`.
- `batch.Status == "expired"` →
  `Event{Type: EventError, Content: "approval_expired"}`. The API proxy
  maps this sentinel to its public expired-batch status.
- Corrupt snapshot (`decodeSnapshot` JSON error) →
  `Event{Type: EventError, Content: "corrupt snapshot: ..."}`. The batch
  stays in its current status; operators reconcile manually.
- `MarkResolved` / `MarkDispatched` errors → logged via `slog.Warn`; the
  resume continues.
- Context cancel mid-fan-out → `sendOrCancel` returns false, in-flight
  goroutines exit, the `WaitGroup` drains, `stepRun` is NOT re-entered.

## Cross-references

- [`docs/orchestrator/run.md`](./run.md) — `Event` shapes, `RunState`,
  `stepRun` lifecycle.
- [`docs/orchestrator/toolregistry.md`](./toolregistry.md) —
  `ExecuteWithApproval` contract, approvalID format.
- `pkg/hitl` — `Resolve` (TOCTOU re-check primitive).
- `pkg/a2a` — `WithBusinessID`, `BusinessIDFromContext`.
- `pkg/domain` — `PendingToolCallBatch`, `PendingCall`, `ToolFloor`,
  `WhitelistMode`.
- `services/api/internal/service/hitl.go` — resolve-side coordinator
  that builds `ResumeRequest`.
- `services/api/internal/handler/hitl.go` — resume HTTP entrypoint.
- `docs/architecture.md` — top-level system flow.
