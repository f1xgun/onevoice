# Orchestrator: Single Iteration (`stepRun`)

`services/orchestrator/internal/orchestrator/step.go` implements the shared
loop body called by `Run` (fresh turn) and `Resume` (post-approval
continuation). One `stepRun` invocation may execute multiple iterations of
the LLM agent loop — its termination condition is one of four terminal
`StepOutcome` values, never a synchronous wait for human approval.

See [`docs/orchestrator/run.md`](./run.md) for the surrounding goroutine
lifecycle and [`docs/orchestrator/resume.md`](./resume.md) for the resume
entry that re-enters `stepRun` after HITL approval.

## Public API

- `type StepOutcome int` — terminal-state discriminator returned to the
  caller goroutine. Constants:
  - `OutcomeDone` — LLM returned a terminal response with no tool calls;
    `EventDone` already emitted.
  - `OutcomePaused` — at least one manual-floor tool call was classified;
    the batch was persisted via `pendingRepo.Persist` and
    `EventToolApprovalRequired` was emitted. Goroutine **must** exit.
  - `OutcomeError` — unrecoverable error; an `EventError` has already been
    emitted.
  - `OutcomeMaxIterations` — safety cap hit; an `EventError` has been
    emitted.
- `type RunState` — the mutable loop state (see *Input / state* below).
  Serialized to `PendingToolCallBatch.ModelMessages` at pause time;
  reconstructed by `Resume`.
- `var ErrConversationTokenCap` — sentinel returned when either per-
  conversation token axis is exhausted. Distinct from
  `OutcomeMaxIterations` because the cap is a softer / earlier guard.

The function itself:

```go
func (o *Orchestrator) stepRun(
    ctx context.Context,
    state *RunState,
    out chan<- Event,
) (StepOutcome, string, error)
```

The string return is the `batchID` produced on `OutcomePaused`, empty
otherwise.

## Input / state

`RunState` is the single source of truth threaded through every iteration.

| Field | Purpose |
|---|---|
| `Messages` | Conversation history forwarded to the LLM. **Must not** carry a leading `role:"system"` entry — system content lives on `SystemPlatform`/`SystemBusiness`. Legacy Resume snapshots with a leading system message fall back to the scrub path in `resume.go`. |
| `SystemPlatform` | Block 1 of the two-block system prompt — the platform-wide prefix Anthropic caches via `cache_control:ephemeral`. Byte-stable per locale. |
| `SystemBusiness` | Block 2 — the per-business prefix. **Never** carries `cache_control`; every business has distinct bytes and stamping would defeat cross-business cache reuse. |
| `AvailableTools` | LLM-visible tool catalog for this turn (whitelist already applied). |
| `BusinessApprovals` / `ProjectApprovalOverrides` | Snapshots of `settings.tool_approvals` / `projects.approval_overrides`. Nil maps OK; treated as empty. |
| `ConversationID`, `BusinessID`, `ProjectID`, `UserID`, `MessageID` | Identity fields persisted on `PendingToolCallBatch` for business-scoped access control at resolve time. `ProjectID` empty ⇒ no project. |
| `Model`, `Tier` | Route subsequent iterations (including post-resume) to the same provider+tier as the initial request. |
| `UserUUID` | `uuid.UUID` form taken by `LLMClient.Chat`; kept alongside string `UserID` for legacy callers. |
| `Iter` | 0-based iteration counter; resume continues at `Iter+1`. |
| `AccumulatedInputTokens`, `AccumulatedOutputTokens` | Running per-conversation sums persisted in the pause snapshot so resume doesn't reset the budget to zero. |

## Output

The function writes onto the `out chan<- Event` channel. The caller closes
the channel after `stepRun` returns; `stepRun` never closes it.

Per-iteration event sequence on the wire (events for one LLM response):

1. `EventError` (terminal) — on LLM error, token cap, dispatch failure,
   HITL guard, or max-iterations.
2. `EventText` then `EventDone` — terminal happy path (no tool calls).
3. `EventToolRejected` per forbidden call (in tool_calls order).
4. `EventToolCall` + `EventToolResult` per auto call (parallel; arrival
   order independent of tool_calls order — correlate by `ToolCallID`).
5. `EventToolApprovalRequired` — at most one per turn; emitted **after**
   `pendingRepo.Persist` commits `status=pending`. Goroutine exits.

## Single-iteration semantics

Each iteration of the for-loop performs:

1. **Build `llm.ChatRequest`**. `SystemBlocks` are set only if at least
   one of `SystemPlatform` / `SystemBusiness` is non-empty. Block 1
   carries `CacheBoundary: true` so Anthropic stamps `cache_control` on
   it; Block 2 does not.
2. **Resolve BusinessID**. `parseBizID` degrades malformed `BusinessID` to
   `uuid.Nil` so the router's nil-guard skips billing instead of writing
   a corrupt row. `uuid.MustParse` would panic — unacceptable on the
   hot-path `llm.Chat` call.
3. **Call `o.llm.Chat(ctx, req)`**. On error, `translateChatError` maps
   rate-limiter sentinels (`ErrDailySpendExceeded`,
   `ErrRateLimitUnavailable`, `ErrRateLimitExceeded`) to coded
   `EventError` frames with a localized fallback message; everything else
   falls through with `err.Error()` text.
4. **Accumulate tokens** *before* the no-tool-calls branch, so the cap
   fires uniformly on both terminal and tool-call iterations. Mid-iter
   overshoot is caught because the comparison is against the running sum.
5. **Check the cap**. If either `ConversationInputCap > 0 &&
   AccumulatedInputTokens >= cap` or the output equivalent, emit
   `EventError{Code: "conversation_token_cap"}`, increment
   `LLMConversationCapHit{axis="input|output"}` metric, return
   `OutcomeError, "", ErrConversationTokenCap`.
6. **Terminal branch**: if `len(resp.ToolCalls) == 0`, emit `EventText`
   (when non-empty) then `EventDone`, return `OutcomeDone`. The branch is
   gated solely on the absence of tool calls — `FinishReason` is **not**
   consulted, because some providers report `finish_reason == "stop"` on a
   completion that still carries `tool_calls`.
7. **Append assistant message** with `tool_calls` to `state.Messages`.
8. **Bucket tool calls** via `hitl.Bucket(o.tools.Floor,
   BusinessApprovals, ProjectApprovalOverrides, resp.ToolCalls)` →
   `(autoCalls, manualCalls, forbiddenCalls)`. `hitl.Bucket` is a pure
   function so this loop and the resolve-path TOCTOU re-check cannot
   diverge.
9. **Forbidden calls** → synthetic rejection tool-role message appended to
   `state.Messages` + `EventToolRejected` emitted. **Not** dispatched. The
   LLM sees the outcome on the next iteration. The payload is built by
   `rejection.go:buildRejectionMessage` and is self-describing —
   `{"rejected":true,"by":"policy","reason":"policy_forbidden","note":"…"}`.
   The `note` states that OneVoice (not the platform) blocked the call and
   forbids both retrying it and substituting another channel; without it the
   model reliably invents a platform-side cause and offers a retry that can
   never succeed. The `reason` token on the wire `EventToolRejected` is
   unchanged — the frontend maps it to badge copy.
10. **Auto calls** → `dispatchToolCalls(ctx, out, autoCalls,
    &state.Messages)` (parallel fan-out; appends tool-role messages in
    original `tool_calls` order regardless of completion order — preserves
    the `assistant.tool_calls[i].id ↔ tool[i]` provider contract).
    `false` return means context canceled → return `OutcomeError`.
11. **Manual calls** → see *HITL pause* below. Returns from `stepRun`.
12. **Loop**: only auto or forbidden calls remained → `state.Iter++`,
    continue.

After `MaxIterations` iterations, emit `EventError "max iterations (N)
reached"` and return `OutcomeMaxIterations`.

## HITL pause

When `len(manualCalls) > 0`:

1. Guard: if `o.pendingRepo == nil`, emit `EventError` with `"HITL not
   configured: ..."` and return `OutcomeError`. The `New` constructor
   leaves `pendingRepo` nil by default; callers that need HITL must use
   `NewWithHITL` — this is the fail-loud at-use point of that contract.
2. Generate `batchID := uuid.NewString()`.
3. Build the snapshot via `buildPendingBatch(batchID, state,
   manualCalls)`. The snapshot envelope is `modelMessagesSnapshotV2`:
   ```json
   {"v": 2,
    "messages": [...],
    "system_platform": "...",
    "system_business": "...",
    "pdn_allowlist": ["+7 (843) 555-12-34"],
    "accumulated_input_tokens": 12345,
    "accumulated_output_tokens": 678}
   ```
   The `accumulated_*` fields use `omitempty` so pre-cap snapshots stay
   byte-identical; legacy batches hydrate at 0 (correct — pre-cap turns
   weren't subject to the cap). `pdn_allowlist` (also `omitempty`) carries
   the business's own contact values so a resumed turn scrubs outbound
   personal data exactly like the paused one — see *Business-owned contact
   allowlist* in `docs/orchestrator/config.md`. On marshal failure (only
   theoretical for `llm.Message` + strings) we fall back to
   `{"v":2,"messages":[]}` and log — Resume will then emit `corrupt
   snapshot` if it ever loads such a batch.
4. Build `PendingCall[]` from `manualCalls`. JSON args fall back to
   `{"raw": <original-string>}` on unmarshal failure. `FloorAtPause` is
   the constant `ToolFloorManual` — only manual-floor calls reach this
   branch (the `hitl.Bucket` invariant), so the constant is correct
   without a per-call registry lookup.
5. **`o.pendingRepo.Persist(ctx, batch)` MUST succeed before emitting the
   pause event.** Persist's internal `preparing → pending` two-step is
   the crash-recovery seam for the orphan-reconcile sweep.
6. Emit a single `EventToolApprovalRequired{BatchID, Calls:
   summarizeManualCalls(...)}` — one card per batch, not per call.
   `summarizeManualCalls` projects the raw tool calls into
   `sse.ApprovalCall` with `EditableFields` resolved from the registry.
7. Return `OutcomePaused, batchID, nil`.

## Side effects

- `state.Messages` is mutated in place — append-only; the post-pause
  snapshot reads from the same slice.
- `state.AccumulatedInputTokens` / `OutputTokens` are mutated each
  iteration.
- `state.Iter` is incremented per loop iteration.
- `o.pendingRepo.Persist(ctx, batch)` writes a Mongo document on the
  pause branch.
- `metrics.LLMConversationCapHit{axis="input|output"}` is incremented on
  cap hit.
- `dispatchToolCalls` emits tool events and may record
  `metrics.RecordToolDispatch` per call.
- Events are pushed onto the `out` channel; the channel is owned by the
  caller and not closed here.

## Sentinel errors / fail modes

| Outcome | Sentinel / `Code` | Trigger |
|---|---|---|
| `OutcomeError` | `ErrConversationTokenCap` / `Code="conversation_token_cap"` | Either token axis exhausted |
| `OutcomeError` | (LLM error verbatim) / `Code="daily_spend_exceeded"` | `llm.ErrDailySpendExceeded` |
| `OutcomeError` | / `Code="rate_limit_unavailable"` | `llm.ErrRateLimitUnavailable` |
| `OutcomeError` | / `Code="rate_limit_exceeded"` | `llm.ErrRateLimitExceeded` |
| `OutcomeError` | `"HITL not configured: ..."` | Manual-floor call classified with `pendingRepo == nil` |
| `OutcomeError` | `"failed to persist approval batch: ..."` | `pendingRepo.Persist` returned an error — pause event is **not** emitted |
| `OutcomeError` | `ctx.Err()` | Context canceled mid-dispatch or mid-event-emit |
| `OutcomeMaxIterations` | `"max iterations (N) reached"` | Loop exhausted `MaxIterations` |

Tool execution errors are **not** terminal — they become `EventToolResult`
payloads with `tool_error` set so the LLM can react on the next iteration
(see `dispatchToolCalls` in [`run.md`](./run.md)).

## Retry boundary

`stepRun` is **not** an automatic retry boundary. Each iteration is one
LLM round-trip; transient provider errors propagate immediately as
`OutcomeError`. The LLM Router (`pkg/llm`) handles its own provider
fallback chain — once it surrenders, `stepRun` surfaces the failure.

Tool dispatch is the one place that does have retry-adjacent semantics,
and that retry lives outside this file: agents apply Redis SetNX dedupe so
crash-recovery resends of approved batches don't double-execute, and
`Resume` skips calls already marked `Dispatched`.

## Snapshot helpers

- `buildPendingBatch(batchID, state, manualCalls) *PendingToolCallBatch`
  — assembles the pause-time batch; `ModelMessages` carries the
  versioned `modelMessagesSnapshotV2` JSON so `Resume` can rebuild
  `RunState` after a process restart. `Status` / `CreatedAt` /
  `UpdatedAt` / `ExpiresAt` are set by `pendingRepo.Persist`, not here.
- `summarizeManualCalls(reg, calls) []sse.ApprovalCall` — projects raw
  tool calls into the `tool_approval_required` wire shape. `Floor` is
  always `ToolFloorManual` because only paused calls reach here.

## Cross-references

- [`docs/orchestrator/run.md`](./run.md) — surrounding goroutine
  lifecycle, `dispatchToolCalls`, `Event` shapes.
- [`docs/orchestrator/resume.md`](./resume.md) — `Resume` re-enters
  `stepRun` at `Iter = batch.IterationIdx + 1`.
- [`docs/orchestrator/toolregistry.md`](./toolregistry.md) — `Floor`,
  `EditableFields`.
- `pkg/hitl` — `Bucket` pure classifier.
- `pkg/llm` — `ChatRequest`, `SystemBlock`, rate-limiter sentinels.
- `pkg/metrics` — `LLMConversationCapHit`.
- `pkg/domain` — `PendingToolCallBatch`, `PendingCall`, `ToolFloorManual`.
- `pkg/sse` — `ApprovalCall`.
