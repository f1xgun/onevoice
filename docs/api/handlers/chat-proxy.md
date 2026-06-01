# Chat-proxy handler

`services/api/internal/handler/chat_proxy.go` is a thin HTTP-facade over
`services/api/internal/service/chatturn.Turn`. The chat-turn lifecycle
(gate, enrich, stream, persist, auto-title gate, postal fanout) used to
live in this file as five collaborator types; it now lives entirely in
`internal/service/chatturn` as private methods on a single `Turn` struct.
This handler's job is parsing, auth gating, and `TurnOutcome` → HTTP
status mapping when no SSE bytes have flowed yet.

## Route

```
POST /api/v1/chat/{conversationID}
```

Rate-limited per-user via `RateLimitByUser(Chat, scope="chat")` and
gated by `RequireVerifiedEmailDay7` (day-7 soft-restrict). The rate
limit runs BEFORE verify so a throttled request short-circuits before
the DB lookup — see `docs/api/routes.md` for the middleware order.

The HITL resume path (`POST /chat/{id}/resume`) is served by the
`HITLHandler.Resume` method, not by this file. The resume header is
re-used here so the SAME chat endpoint can also act as a resume
trigger for callers that prefer header-based dispatch.

## Resume dispatch

```go
const ResumeBatchHeader = "X-Onevoice-Resume-Batch-Id"

headerBatch := r.Header.Get(ResumeBatchHeader)
if headerBatch == "" {
    headerBatch = r.URL.Query().Get("batch_id")
}
```

`X-Onevoice-Resume-Batch-Id` takes priority over `?batch_id=`. The
header lives in the `handler` package (not `chatturn`) so the router
and existing tests keep using `handler.ResumeBatchHeader`.

When `headerBatch != ""`:

- Body decode is SKIPPED. Explicit-resume calls reuse the already-
  persisted user message and send an empty body. Forcing a decode here
  would 400 every valid resume request.
- `Turn.Run` sees a non-empty `ResumeBatchID` and routes through its
  resume branch (re-emit-approval, batch lookup, dispatch).

When `headerBatch == ""`:

- Body decode runs unconditionally.
- `body.Message` may still be empty — the empty check happens INSIDE
  `Turn.Run`'s Fresh branch and surfaces as `OutcomeMissingMessage`.
  Decoding here even on empty bodies lets the gate route an empty-
  message request to inline-error / re-emit-approval BEFORE the
  message-required check fires (one of `Turn.Run`'s recoverable
  outcomes).

## ACL gate

```go
bc, ok := authz.BusinessContextFromCtx(r.Context())   // 500 backstop
if !authz.Can(r.Context(), authz.PermContentCreate)   // 403 forbidden
```

`PermContentCreate` matches HITLHandler.Resume's gate — resume
re-drives the same turn and may emit further content, so the two
endpoints share the create permission.

The cross-tenant defense for the conversation itself lives inside
`Turn.Run` (`ConversationRepository.GetByID` + ownership check). This
handler does not duplicate it.

## Per-user SSE concurrency cap

```go
if h.sseCounter != nil {
    tier := h.defaultTier
    if tier == "" {
        tier = "free"
    }
    release, acqErr := h.sseCounter.Acquire(r.Context(), bc.UserID, tier)
    if acqErr != nil {
        h.writeConcurrencyError(w, acqErr)
        return
    }
    defer release()
}
```

`Acquire` runs BEFORE any branch writes the SSE `Content-Type` header
so a rejection surfaces as a JSON HTTP 429 — NEVER a half-stream.
Release closures are idempotent. `SetSSECounter` is the optional wiring
hook from `wire.Handlers`; a nil counter or a Counter built with
`max <= 0` disables the gate.

The `defaultTier` field labels the SSE concurrency block metric when a
request has no per-user tier override. Empty falls back to `"free"` at
acquire time so the metric label is never blank.

`writeConcurrencyError` maps the counter sentinels:

| Sentinel                                            | HTTP | Body                                                              |
|-----------------------------------------------------|------|-------------------------------------------------------------------|
| `ssecounter.ErrConcurrencyExceeded`                 | 429  | `{"code":"sse_concurrency_exceeded","retry_after_s":1}` + `Retry-After: 1` |
| `ratelimit.ErrUnavailable` / `ratelimit.ErrExceeded`| 503  | `{"code":"rate_limit_unavailable"}`                              |
| anything else                                       | 503  | `{"code":"rate_limit_unavailable"}` (defensive fallback)         |

`retryAfterSeconds = 1` is honest under typical short-burst load — the
counter releases as soon as ANY in-flight stream from the same user
completes, so 1s is plenty for the next attempt to succeed.

## Conversation persistence + tool-call accumulation

The handler does NOT persist messages. `Turn.Run` accumulates
`tool_call` and `tool_result` SSE events during streaming and saves
them as `domain.ToolCall[]` / `domain.ToolResult[]` on the Mongo
Message document. `GET /conversations/:id/messages` returns them; the
frontend's `useChat.ts` maps them on load → re-entry into the
tool-approval panel works.

This file's only message-repo touchpoint is `loadHistory`, retained
because one legacy test (`TestChatProxy_LoadHistory_SkipsEmptyAssistant`)
constructs the handler with a struct literal and reads it directly so
the test does not depend on a fully wired `*chatturn.Turn`.
`loadHistory` skips empty assistant rows that have no tool calls —
without that filter the LLM would see literally-empty assistant turns
that the orchestrator's prior interruption left behind.

## TurnOutcome → HTTP status mapping

```go
outcome, err := h.turn.Run(r.Context(), w, req, nil)
switch outcome {
case OutcomeMissingMessage:        // 400 message is required
case OutcomeBusinessNotFound:      // 404 business not found
case OutcomeOrchestratorUnavailable: // 502 orchestrator unavailable
case OutcomeError:                 // bytes may already be on the wire; log only
default:                           // OutcomeDone / OutcomePauseHITL / OutcomeRejoinedResume /
                                   // OutcomeReemittedApproval / OutcomeInlineError —
                                   // SSE bytes already committed
}
```

The mapping rule: a `TurnOutcome` translates to an HTTP status code
ONLY when no SSE bytes have been written yet. Once any SSE byte hits
the wire (gate non-Fresh branches, or the fresh stream successfully
forwarding orchestrator output), the response body is committed and
HTTP-status mapping is moot — those outcomes fall through silently,
and `OutcomeError` only logs.

`OutcomeError` is the explicit recognition that the same `err != nil`
can mean "pre-stream wiring failure" (where we could have written a
4xx/5xx) OR "mid-stream orchestrator drop" (where the committed 200
must stand). The handler logs at ERROR with `error` so on-call has the
signal, but does not attempt to overwrite the committed response.

## Construction

`NewChatProxyHandler` keeps the legacy 12-arg signature so `wire` and
existing tests continue to compile unchanged. Internally it constructs
the `chatturn.Turn` that owns the lifecycle:

- A nil `orchClient` is replaced with a no-op client built from an
  empty URL — preserves the `chat_proxy_test.go` pattern where tests
  pass nil to skip the orchestrator handshake. The SSE path then
  returns orchestrator-unavailable cleanly.
- A nil `titler` short-circuits to `chatturn.Deps.Titler = nil` so the
  auto-title gate in `chatturn` returns early without doing a needless
  conversation re-read.
- `projectService`, `conversationRepo`, `pendingRepo` are required —
  nil here panics on construction (wire bug, not a runtime
  recoverable).

`titlerAdapter` is a small struct that satisfies `chatturn.Titler`
around the concrete `*service.Titler`. The wrapper exists so
`chatturn` doesn't have to import the wider `service.Titler` surface
(method set tightly scoped to `GenerateAndSave`).

## Test-only entry points

`fireAutoTitleIfPending` and `fireAutoTitleIfPendingResume` are
test-only wrappers that adapt the legacy `persistCtx func() (ctx,
cancel)` closure pattern to `chatturn.Turn`'s ctx-based public API.
The closure pattern was unique to the legacy handler shape; preserved
here so the existing `chat_proxy_test.go` suites pass unchanged. The
wrappers MUST NOT be called from production code paths — `Turn.Run`
already invokes the underlying gate.

## SSE event types proxied through

The orchestrator emits these event types; the chat-proxy forwards
them verbatim and lets `chatturn.Turn` accumulate them for persistence:

| Event         | Payload fields                                        |
|---------------|-------------------------------------------------------|
| `text`        | `text`                                                |
| `tool_call`   | `tool_name`, `tool_args`                              |
| `tool_result` | `tool_name`, `tool_result` (any), `tool_error` (str)  |
| `done`        | `usage` (token counts)                                |
| `error`       | `error` (str)                                         |

Event ordering is preserved 1:1 from the orchestrator's emit order —
`Turn.Run` does NOT reorder, deduplicate, or coalesce events. The
frontend depends on the orchestrator's order to render tool-call
panels in the same sequence the LLM emitted them.
