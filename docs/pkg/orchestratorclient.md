# pkg/orchestratorclient — Typed HTTP Client

Thin typed HTTP client for the orchestrator's cluster-internal endpoints,
consumed by `services/api`. Replaces a previous scatter of inline
`*http.Client.Do` calls across `chatproxy.OrchestrationProxy`,
`chatproxy.HITLCoordinator`, `service/hitl.go::ToolsRegistryCache`,
`service/review_drafter.go`, and `wire/policy_sweep.go`. Symmetric in
shape with `pkg/tokenclient/`.

Code in `pkg/orchestratorclient/client.go` keeps only 1-line godoc and
inline WHY comments on specific lines; the contract — endpoints, body
lifecycle, retry / timeout / cancellation semantics, error envelope
mapping — lives here.

The public-API table (method → endpoint, body lifecycle) is the single
source of truth in [`pkg/orchestratorclient/AGENTS.md`](../../pkg/orchestratorclient/AGENTS.md);
this doc adds the WHY and the cross-cutting design decisions.

## Client lifecycle

`New(baseURL, httpClient)` constructs a Client:

- `nil` httpClient → `http.DefaultClient`. Production should pass a
  pre-configured `*http.Client` with shared transport / timeouts.
- Trailing slashes are stripped from `baseURL` so callers cannot
  accidentally create double-slash URLs (`http://orch//chat/...`).

`BaseURL()` and `HTTPClient()` accessors exist so callers needing the same
transport (logging wrappers, shared timeouts) can compose off the same
client value instead of constructing a parallel one.

## Body lifecycle convention

Methods split into two groups with opposite ownership of `resp.Body`:

| Method group        | `resp.Body` owner | Why                                  |
| ------------------- | ----------------- | ------------------------------------ |
| `StreamSSE`         | Inside (deferred) | Caller never sees the response       |
| `ListTools` etc.    | Inside (deferred) | Body is decoded, then closed         |

`StreamSSE` always closes the upstream body itself via `defer
resp.Body.Close()` and forwards the framed data into the caller-supplied
`http.ResponseWriter`. The caller never touches the upstream `*http.Response`.

(An older API had `StreamChat` / `StreamResume` that returned raw
`*http.Response` and required the caller to close it. That shape was folded
into the single `StreamSSE` entry point during the SSE-codec consolidation
— `StreamSSE` is the only streaming method now.)

## StreamSSE — the SSE proxy

`StreamSSE` owns four cross-cutting concerns in a single call:

1. **URL selection** based on `req.BatchID`:
   - `BatchID == ""` → `POST /chat/{ConversationID}` (fresh chat turn)
   - `BatchID != ""` → `POST /chat/{ConversationID}/resume?batch_id=X`
     (HITL resume after approval)
2. **Upstream context handling** — correlation propagation + optional
   detach for client-disconnect semantics.
3. **SSE response envelope** — written exactly once before any byte of
   the upstream body lands on the wire.
4. **Buffered drain loop** with optional per-event domain dispatch.

### Correlation propagation

The correlation ID is resolved once via `logger.CorrelationIDFromContext`.
It is reused for two things:

- Upstream `Context` propagation (when budget > 0 we build a fresh
  `Background()` context but re-attach the correlation ID via
  `logger.WithCorrelationID`).
- Upstream `X-Correlation-ID` header — set only if the caller-supplied
  `Headers` map does NOT already contain one. The chatturn paths inject
  their own merged map and that override wins; we never overwrite.

### OrchCtxBudget — detached-context semantics

`OrchCtxBudget` controls how the upstream request is contextualized:

| Value | Upstream context           | Client-disconnect behavior                  |
| ----- | -------------------------- | ------------------------------------------- |
| `0`   | Inherits the caller's ctx  | ctx cancel aborts upstream immediately      |
| `> 0` | Fresh `Background()` + budget | Writes stop on ctx cancel; upstream drains |

The detached branch is what makes the chatturn lifecycle invariant
possible: a client navigating away mid-stream MUST NOT abort the
orchestrator's LLM call, because that call has side effects (tool
dispatch, message persistence) that need to reach terminal states. So the
proxy keeps reading from the upstream until the response body ends, but
silently drops the bytes onto the floor instead of writing them to the
now-dead client socket.

Concretely: under `OrchCtxBudget > 0`, the proxy sets `clientGone :=
ctx.Done()`. Each drain iteration tries to read from `clientGone` via a
non-blocking `select`; if the channel is closed, `write = false` for the
rest of the drain. Under `OrchCtxBudget == 0`, `clientGone` is nil and the
select default always wins → the branch is a free no-op.

The fresh-background context still gets the OrchCtxBudget deadline applied
so a stuck upstream cannot leak goroutines forever, even if the client
already left.

### SSE response envelope

Headers written exactly once, before the first byte of the upstream body
lands:

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

`X-Accel-Buffering: no` disables nginx's response buffering so the FE sees
frames as they arrive instead of in 8 KiB batches.

Callers MUST NOT write to `req.Writer` before invoking `StreamSSE` —
`WriteHeader(http.StatusOK)` would race or no-op.

### Drain loop

`bufio.Scanner` with a 1 MiB buffer cap (`sseScannerBufferBytes = 1 << 20`).
This matches the prior chatproxy buffer — large `tool_result` payloads
(whole-channel review batches) must fit a single line. Smaller would split
a single frame across two scanner reads, which the line-oriented SSE format
does not support.

For each scanned line:

1. Forward the raw bytes to `req.Writer` (unless client gone — see above).
2. Flush the writer so the FE sees each frame immediately.
3. If `req.OnEvent != nil` AND the line starts with `data: `, parse the
   suffix as an SSE event and invoke `OnEvent` synchronously.

`OnEvent` is for domain dispatch: a `tool_call` event triggers AgentTask
creation, etc. The handler/hitl.go resume-proxy path passes
`OnEvent == nil` because it is pure forwarding.

Malformed frames (the `data:` parse failed) are forwarded to the client as
raw bytes but SKIPPED for `OnEvent` — we avoid handing callers a
zero-valued domain event. The upstream is responsible for emitting
well-formed JSON; a single garbled frame should not crash the loop or
trigger false-positive domain dispatches.

### Failure modes

`StreamSSE` returns non-nil on:

- `Writer` does not implement `http.Flusher` — no bytes have been written
  yet, so callers can still map to a non-SSE HTTP error response.
- `c.httpClient.Do` failed — connect failure, error wrapped with `stream
  chat:` or `stream resume:` so caller substring matchers can distinguish
  pre-connect from mid-drain failures.
- `scanner.Err()` returned non-nil on the drain — wrapped with `scanner:`.

It returns `nil` after a clean drain, including the "client went away"
case under `OrchCtxBudget > 0` (upstream was drained but the writes after
the disconnect were skipped — from the caller's perspective the request
completed normally).

## JSON RPC-style endpoints

`ListTools`, `ListToolNames`, and `DraftReply` follow the same shape:

1. Build the request with `http.NewRequestWithContext`.
2. Call `c.httpClient.Do`.
3. `defer resp.Body.Close()`.
4. Check `resp.StatusCode` against a per-endpoint allow-set.
5. Decode JSON.

### Error envelope

Every error is wrapped with the prefix `orchestratorclient: <verb>:` so
caller substring matchers can distinguish failure phases:

- `orchestratorclient: build <verb> request: …` — pre-Do failure (bad
  URL, broken header).
- `orchestratorclient: <verb>: …` — network failure during Do.
- `orchestratorclient: <verb>: unexpected status %d` — non-2xx response.
- `orchestratorclient: decode <verb>: …` — body received but unparseable.

### Status-code policy

- `ListTools` and `ListToolNames` accept ONLY `200 OK`. Any other status
  yields `unexpected status %d` and the caller treats the projection as
  unavailable.
- `DraftReply` accepts `200 ≤ status < 300` and, on non-2xx, reads up to
  512 bytes of the body for diagnostics. The bounded readback is
  important — a streaming JSON error response should not be slurped into
  memory wholesale.

### Localisation

`ListTools(ctx, acceptLanguage)` forwards the caller's `acceptLanguage`
as the request's `Accept-Language` header. The orchestrator's locale-
aware projection returns the description in the caller's preferred
language. Pass `""` to use the orchestrator's default (RU). Both
single-tag values (`"en"`) and preference lists (`"en-US,en;q=0.9"`) are
accepted — the orchestrator parses them with
`pkg/i18n.MatchAcceptLanguage`.

## Retry / timeout policy

`pkg/orchestratorclient` does NOT retry on its own. The orchestrator is in
the same cluster as the API and an internal HTTP failure is usually a real
outage that the caller should propagate immediately. Retries belong to:

- The Router's per-LLM-call sibling retry (`pkg/llm/router.go`) — different
  layer, different concern.
- The caller's framework (e.g. `services/api/internal/wire/policy_sweep.go`
  has its own scheduled retry around `ListToolNames`).

Per-call deadlines come from the caller's context. The single exception is
`StreamSSE`'s `OrchCtxBudget` — see above.

## Public DTO surface

- `ToolEntry` — mirrors
  `services/orchestrator/internal/handler/internal_tools.go`'s `AllEntries`
  output. `DisplayNameKey` is the i18n catalog key the frontend uses to
  render the settings UI's tool label in the user's locale. Optional —
  orchestrator deploys without the key send `""` and the FE falls back to
  `DisplayName`.
- `DraftReplyExample` — one (review → owner reply) pair shown to the LLM
  as few-shot context. Mirrors
  `services/orchestrator/internal/handler/draft_reply.go`.
- `DraftReplyRequest` / `DraftReplyResponse` — body of
  `POST /internal/draft-reply`.
- `StreamSSERequest` — configuration for `StreamSSE`. Fields documented
  inline in the struct godoc.

## Pointers to related docs

- [`pkg/orchestratorclient/AGENTS.md`](../../pkg/orchestratorclient/AGENTS.md)
  — public-API table + tests howto.
- [`pkg/llm.md`](llm.md) — sibling design contract for the LLM Router that
  the orchestrator itself fronts.
