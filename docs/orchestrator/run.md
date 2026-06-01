# Orchestrator: Agent Loop (Run)

`services/orchestrator/internal/orchestrator/orchestrator.go` implements the
synchronous half of the LLM agent loop. `Run` starts a fresh turn from
`RunRequest`; the companion `Resume` (see `resume.go` /
[`docs/orchestrator/resume.md`](./resume.md)) rebuilds state from a persisted
batch snapshot. Both spawn the same `stepRun` goroutine — there are no
blocked goroutines on approval channels.

## Public API

- `type EventType string` — discriminator on the SSE event channel.
- `EventText`, `EventToolCall`, `EventToolResult`, `EventError`,
  `EventDone` — legacy event types.
- `EventToolApprovalRequired` — emitted once per paused LLM turn, carrying
  `BatchID` + summarized `Calls` that need human approval. Emitted AFTER
  `pendingRepo.Persist` completes (`status=pending` committed); never
  emitted on a partial-persist crash. The goroutine exits immediately
  after.
- `EventToolRejected` — emitted for each tool call that the policy resolver
  marks `ToolFloorForbidden` at pause time (synthetic rejection) OR that
  the resolve-time TOCTOU re-check marks `policy_revoked`. Content carries
  the reason.
- `type Event` — the shape pushed onto the output channel. Fields:
  - `Type` — `EventType`.
  - `Code` — machine-readable discriminator on error events so downstream
    consumers (api proxy / frontend) can branch on the failure mode
    without parsing `Content`. Projected onto `sse.Event.Code` on the
    wire; absent (omitempty) on non-error events.
  - `Content` — free-form payload (text, error message, reject reason).
  - `ToolCallID` — the LLM's real call ID; `chat_proxy` persists tool_call
    events on `Message.ToolCalls` keyed by it.
  - `ToolName`, `ToolDisplayName`.
  - `ToolDisplayNameKey` — i18n catalog key the frontend uses to render
    the `agent_tasks` task title in the user's locale. Populated from
    `toolregistry.Registry.DisplayNameKey` at dispatch time. Empty when
    the tool has no registered key — the FE falls back to
    `ToolDisplayName`. Surfaces on `EventToolCall` + `EventToolResult` so
    `chat_proxy` can stamp `AgentTask` documents with the localizable
    key from either event.
  - `ToolArgs`, `ToolResult`, `ToolError`.
  - `BatchID` — set on `EventToolApprovalRequired`; carries the
    `PendingToolCallBatch._id` so the frontend can POST to the resolve
    endpoint with the same identifier.
  - `Calls` — set on `EventToolApprovalRequired`; one
    `sse.ApprovalCall` per manual-floor tool call bundled into the
    batch. `sse.ApprovalCall` lives in `pkg/sse` so the same shape is
    consumed by the api's HITL coordinator and the implicit-resume
    re-emit path — single source of truth for the
    `tool_approval_required` wire contract.
- `type LLMClient interface` — abstracts the Router for testability.
- `type RunRequest` — everything needed to start an agent run. Legacy
  fields plus HITL identity fields (`ConversationID`, `BusinessID`,
  `ProjectID`, `UserIDString`, `MessageID`) and HITL policy inputs
  (`BusinessApprovals`, `ProjectApprovalOverrides`). All HITL fields are
  optional to preserve backward-compat with legacy callers. Empty
  identity strings are tolerated — the repo stores them verbatim and the
  resolve handler will 403/404 on missing context when it needs
  business-scoped auth. Nil policy maps are tolerated (treated as empty
  maps — the registry floor wins).
- `type Options` — `MaxIterations`, `ToolExecTimeout`,
  `ConversationInputCap`, `ConversationOutputCap`.
- `type Orchestrator struct { ... }` — runs the agent loop.
- `New(llm, registry)` — defaults `MaxIterations=10`, no HITL.
- `NewWithOptions(llm, registry, opts)` — custom options, no HITL.
- `NewWithHITL(llm, registry, pendingRepo, opts)` — HITL wired in;
  `pendingRepo` receives `Persist` + `MarkDispatched` + `MarkResolved`
  calls from `stepRun` / `Resume`. Use this in `cmd/main.go`.
- `Run(ctx, req) (<-chan Event, error)` — starts a fresh turn.

## Lifecycle

1. `Run` builds a fresh `RunState` from `RunRequest`:
   - `prompt.BuildSplit(req.BusinessContext, req.ProjectContext, req.Messages)`
     emits the two-block system prompt. Block 1 (platform) carries
     `cache_control` via `SystemBlocks[0].CacheBoundary` in `stepRun`;
     Block 2 (business) does not. `Messages` no longer carries a leading
     `role:"system"` entry — `SystemBlocks` is the canonical channel.
   - `o.tools.AvailableForWhitelist(ctx, ...)` resolves the LLM-visible
     tool set against the project whitelist.
2. A goroutine runs `stepRun(ctx, state, ch)`; the channel is closed on
   return (done / paused / error).
3. `Resume` (in `resume.go`) is the companion wrapper that rebuilds
   `RunState` from a persisted batch snapshot; both call into `stepRun`.

## State machine

Terminal exits from `stepRun`:

- `EventDone` — LLM signaled stop, no more iterations.
- `EventToolApprovalRequired` — paused for HITL; resume continues via
  `Resume`.
- `EventError` — LLM error, tool dispatch error, or a fail-loud HITL guard
  (`HITL not configured` when a manual-floor tool is classified without a
  `pendingRepo`).
- Max-iterations guard — hard cap at `Options.MaxIterations` (default 10)
  to bound runaway tool loops.

## Tool dispatch

`dispatchToolCalls` executes a batch of tool calls from a single LLM
response concurrently. Each goroutine emits its `tool_result` event as
soon as that tool finishes, so the UI reflects real per-tool latency
rather than the batch's slowest member.

The tool messages appended to `messages` are ordered to match the
original `tool_calls` slice — OpenAI and Anthropic require `role:tool`
messages to line up with `assistant.tool_calls[*].id` for the next
iteration. Tool call dispatch order on the wire is independent (goroutines
finish in completion order); ordering of the appended messages is what
matters for the provider contract.

`dispatchToolCalls` returns `false` if the context was canceled before
all events could be emitted — the caller propagates the cancellation.

`buildToolResultEvent` shapes a single tool outcome into the event
emitted on the SSE channel. Defined out-of-line so the goroutine body
stays short and side-effect free. `displayNameKey` is threaded through
so `chat_proxy` can stamp the `AgentTask` document with a localizable
key.

`executeOne` runs a single tool, optionally bounded by
`Options.ToolExecTimeout` (zero means the parent context governs), and
records `metrics.RecordToolDispatch` keyed by tool name + agent prefix.
Safe for concurrent calls.

`parseToolArgs` unmarshals JSON tool arguments. On failure it falls back
to a single `"raw"` field so the tool executor still receives the
original payload (rather than discarding a malformed-but-decodable LLM
output).

## HITL hand-off (interaction with resume.go)

When a manual-floor tool surfaces during `stepRun`, the goroutine
persists a `PendingToolCallBatch` and emits `EventToolApprovalRequired`
exactly once before returning. The conversation later re-enters via
`Resume` → `resumeGoroutine` → `dispatchApprovedCalls` → `stepRun` with
`Iter = batch.IterationIdx + 1`. See
[`docs/orchestrator/resume.md`](./resume.md).

## Error / failure modes

- `HITL not configured` (`EventError`) — fail-loud guard when a
  manual-floor tool is classified without a `pendingRepo`. The `New`
  constructor leaves `pendingRepo` nil by default; callers that need
  HITL must use `NewWithHITL`. Callers that don't use HITL never see
  this error (fail-loud at-use pattern).
- LLM-side errors are surfaced verbatim on `EventError`.
- Tool execution errors are NOT terminal — they become
  `EventToolResult` payloads with `tool_error` set so the LLM can react
  on the next iteration.
- Context cancellation propagates: `dispatchToolCalls` returns `false`
  and the spawned goroutine drops the remaining events on the floor.

## Cross-references

- [`docs/orchestrator/resume.md`](./resume.md) — HITL resume flow.
- [`docs/orchestrator/prompt.md`](./prompt.md) — `BuildSplit` contract.
- [`docs/orchestrator/toolregistry.md`](./toolregistry.md) — `Available*`,
  `Execute*`, `Floor`.
- `pkg/sse` — `Event`, `ApprovalCall` wire shapes.
- `pkg/a2a` — `CodedError` whose `Code` lands on `Event.Code`.
- `pkg/metrics` — `RecordToolDispatch`.
- `docs/architecture.md` — top-level system flow.
