# Chat-turn HITL integration

`services/api/internal/service/chatturn/hitl.go` glues the chat-turn lifecycle (gate → enrich → stream → persist) to the HITL pause/resume protocol. Two responsibilities live here:

1. **`gateOnRequest`** — inspects the conversation's active message + pending batches at the start of every `POST /chat` and selects one of four actions (fresh / rejoin-resume / re-emit approval / inline error).
2. **`streamResume`** — proxies a resolved (or rejoin) batch through the orchestrator's `/chat/{id}/resume` SSE endpoint, folds `tool_result` events into the existing assistant `Message`, and finalizes the message status on terminal events.

Companion helpers `reemitApprovalEvent` and `sseInlineError` write standalone SSE events that close the stream without an orchestrator round-trip.

`Turn.Run` is the single public entry point that consumes these helpers — see `services/api/internal/service/chatturn/` and `docs/services/hitl.md` (the resolve-path companion).

## Gate: the four-action tri-case

`gateOnRequest(ctx, conversationID, headerBatchID)` returns `(action, activeMsg, batch, batchID)`. `headerBatchID` is the resolved batch id from `X-Onevoice-Resume-Batch-Id` (or `?batch_id` query) — empty means the client did not request explicit resume.

| Action | Trigger | Next step |
|---|---|---|
| `gateFresh` | no active msg, no resume header | start a new LLM turn |
| `gateRejoinResume` | explicit header set, OR implicit rejoin (active msg + `resolving` batch) | call `streamResume` |
| `gateReemitApproval` | active msg + `pending` batch (UI lost the approval card) | call `reemitApprovalEvent` (no orchestrator round-trip) |
| `gateInlineError` | active msg with no active batch (orphan `in_progress`) | call `sseInlineError` |

**Tri-case is locked.** The four constants must NOT collapse to two — the implicit-resume + explicit-resume contract is load-bearing.

**Soft-error fall-through.** `FindByConversationActive` failure (other than `ErrMessageNotFound`) and `ListPendingByConversation` failure both fall through to log-and-continue. The legacy `chat_proxy.go` behavior is preserved so a transient DB blip cannot brick a chat. No terminal error is ever surfaced from the gate; only the four-tuple is returned.

**Implicit-resume branch** (no header, active message exists): look up the conversation's active batches and apply the tri-case. The first `resolving` batch wins over the first `pending` batch — orchestrator mid-dispatch takes precedence over UI redraw. When the rejoin is `resolving`, the returned `batchID` is the resolving batch's ID and `activeMsg.ID` is preserved so `tool_result` events extend the same `Message` row.

**Explicit-resume branch** (header set AND active message): forward to `streamResume` keyed on the header's `batchID`.

## Pending-tool-call coordination

`reemitApprovalEvent` writes a `tool_approval_required` SSE event built from the persisted `PendingToolCallBatch`. The implicit-resume gate calls this when the client reopens the chat mid-approval (network flap, page reload) and the batch is still `status="pending"`. The `EditableFields` slice is left empty and `Floor` is set to `ToolFloorManual` — the resolve handler enforces the real per-tool allowlist via `ToolsRegistryCache` on submit. No orchestrator round-trip.

The SSE response headers (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`) are set on every standalone SSE path (re-emit, inline error) so an intermediate proxy does not buffer the stream.

## `streamResume` — orchestrator pause/resume protocol

`streamResume(ctx, w, conversationID, activeMsg, batchID) (TurnOutcome, error)`:

1. **Pre-flight batch lookup.** `Pending.GetByBatchID(batchID)` — must exist and have `ConversationID == conversationID`. Mis-scoped or missing → `sseInlineError("no_active_approval_for_conversation")` + return `OutcomeInlineError`.
2. **Local Message copy.** Work on a value copy of `*activeMsg` so we flush the full final state in one `Update`. `msg.Content` is intentionally NOT cleared — we preserve the `Content` builder semantics and start from whatever was persisted at pause time. `postText` accumulates new text from `text` events.
3. **Call-id index.** Build `callIdx map[string]int` over `msg.ToolCalls` so `tool_result` / `tool_rejected` events can update the matching `ToolCall.Status` in place.
4. **`StreamSSE` delegation.** The SSE plumbing (detached ctx via `OrchCtxBudget`, correlation propagation, response headers, scanner, drain loop) lives in `pkg/orchestratorclient.StreamSSE`. This function owns the per-event mutation closure and the post-stream persistence decision.

### Per-event mutation closure

| `ev.Type` | Action |
|---|---|
| `text` | append to `postText` |
| `tool_result` | append `domain.ToolResult` to `msg.ToolResults`; flip `msg.ToolCalls[idx].Status` to Approved/Rejected via `ToolError` empty-check |
| `tool_rejected` | flip `msg.ToolCalls[idx].Status` to Rejected |
| `error` | finalize: `msg.Status = Complete`, `msg.Content = postText.String()`, persist, mark `terminated = true` |
| `done` | finalize as above + `fireAutoTitle = true` |

Once `terminated`, any later frames the upstream emits are defensively ignored — keeps the persist decision idempotent.

**`error`-event finalization is load-bearing.** Resume can fail mid-stream (LLM error, ctx cancel, max-iterations cap). The error event is already forwarded to the client; here we MUST transition the assistant `Message` off `pending_approval`/`in_progress`, otherwise every subsequent `POST /chat` hits the gate's `turn_already_in_progress` branch and the conversation is permanently stuck.

`tool_result` `Content` decoding: when `ev.ToolResult` is a `map[string]interface{}` we pass it through verbatim; otherwise we wrap as `{"raw": ev.ToolResult}` so the persisted shape stays consistent.

### Post-stream persistence decision

After `StreamSSE` returns, three cases are distinguished:

- **Connect failure** (`streamErr != nil && !terminated && strings.Contains(streamErr.Error(), "stream resume:")`): no bytes written to `w` yet, so the handler can still emit a 502 JSON body. Returns `OutcomeOrchestratorUnavailable` with the wrapped error.
- **Mid-drain failure** (`streamErr != nil && terminated || other error shape`): bytes were already written, response is committed — log warning and fall through.
- **Terminated** (either `done` or `error` fired): if `fireAutoTitle`, dispatch `fireAutoTitleIfPendingResume`. Return `OutcomeRejoinedResume`.
- **Non-terminated exit** (scanner ended without a terminal event — transient network drop, orchestrator closed early, unhandled event): persist `msg` with `postText` content and force `Status = Complete` if still `PendingApproval`/`InProgress`. Leaving it active permanently bricks the conversation. Uses `persistContext` (fresh ctx, not the request ctx which was canceled when the SSE stream closed).

`persistResumeDone` is the in-closure persistence helper that runs on `persistContext` (NOT the request ctx) for the same reason — the request ctx is canceled when the SSE stream closes.

## Cross-references

- `docs/services/hitl.md` — the companion resolve path (`POST /pending-tool-calls/{batch_id}/resolve`).
- `pkg/orchestratorclient.StreamSSE` — SSE plumbing (detached ctx, scanner, drain loop).
- `pkg/sse` — `Event` shape, `Marshal`, `ApprovalCall`.
- `services/orchestrator/internal/resume` — the orchestrator-side resume goroutine that produces the SSE events consumed here.
