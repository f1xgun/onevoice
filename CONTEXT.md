# OneVoice — Domain Vocabulary

Cross-cutting domain terms used in code and in architectural discussion. New terms
should be added here when introduced into the codebase. Module-specific terminology
lives in per-module `AGENTS.md` files.

## Glossary

### Chat turn

One round-trip in the conversational AI flow: a user message in, an assistant
reply (or HITL pause) out. A turn has a single lifecycle:

1. **Gate** — decide whether this request is a fresh turn, a HITL resume, an
   approval re-emit, or a duplicate-in-flight inline error.
2. **Enrich** — load business / project / integrations / history; persist the
   user message.
3. **Stream** — open the orchestrator's SSE stream, dispatch text / tool_call /
   tool_result / pause / error events.
4. **Post-stream** — persist the assistant message (pause-time OR done/error),
   fire auto-title gate, fan out postal side effects (posts, reviews,
   agent-tasks).

A turn is implemented by [`services/api/internal/service/chatturn`](services/api/internal/service/chatturn/).
The HTTP handler at `services/api/internal/handler/chat_proxy.go` only parses
the request, calls `chatturn.Turn.Run`, and maps the returned `TurnOutcome` to
an HTTP status.

**Terminal outcomes**: `Done`, `Error`, `PauseHITL`, `ReemittedApproval`,
`OrchestratorUnavailable`, `BusinessNotFound`.

### HITL (Human-In-The-Loop)

Approval flow for tool calls whose `ToolFloor` is `Manual` or `Forbidden`. An
HITL turn pauses mid-flight at a `tool_approval_required` SSE event, persists
the assistant message with `Status=PendingApproval`, and waits for the user to
approve or reject each pending tool call before resuming. See
[`pkg/hitl/policy.go`](pkg/hitl/policy.go) for the pure policy resolver.

### Approval ID

A `{batch_id}-{tool_call_id}` string that uniquely identifies one tool-call
approval across the system. Used as the Redis dedupe key in
[`pkg/hitldedupe`](pkg/hitldedupe/) so that a retry within the 24-hour approval
window never double-executes.

### Postal fanout

The side effects triggered when a successful tool execution implies external
state change worth tracking — a post landing on Telegram, a review reply on
Yandex.Business, a scheduled VK publication. Lives inside `chatturn` as an
unexported sub-module (`postal.go`); it is **not** a separate service.
