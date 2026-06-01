# Orchestrator: Chat HTTP Handler

`services/orchestrator/internal/handler/chat.go` is the HTTP entry point for
the orchestrator's agent loop. `POST /chat/{conversationID}` is the only
endpoint here; it decodes the chat request body, sets up the SSE response,
hands the request off to the `Run` runner, and copies emitted
`orchestrator.Event` values onto the wire as `pkg/sse.Event` frames.

See [`docs/orchestrator/run.md`](./run.md) for the `Event` payload shape and
loop lifecycle, and [`docs/orchestrator/resume.md`](./resume.md) for the
companion HITL resume entry point.

## Public API

- `type Runner` — the single-method interface (`Run(ctx, req) → (<-chan
  Event, error)`) the handler depends on. Decouples the HTTP layer from the
  concrete `orchestrator.Orchestrator` so tests can inject a fake.
- `type ChatHandler` — request-scoped state: the runner and the default
  model name (typically `LLM_MODEL`) used when the request body omits it.
- `NewChatHandler(runner, defaultModel)` — constructor.
- `(*ChatHandler).Chat(w, r)` — the HTTP handler bound to
  `POST /chat/{conversationID}`.

## Request body

```jsonc
{
  // LLM routing
  "model": "claude-sonnet-4",        // optional; falls back to LLM_MODEL
  "tier": "free",

  // User turn + history
  "message": "...",                  // required
  "history": [{"role":"user","content":"..."}, ...],

  // Business identity + context
  "business_id": "<uuid>",
  "business_name": "...",
  "business_category": "...",
  "business_address": "...",
  "business_phone": "...",
  "business_website": "...",
  "business_description": "...",
  "business_voice_tone": ["warm", "professional"],
  "active_integrations": ["telegram", "vk"],

  // Project enrichment — all optional. Empty project_id ⇒ no project layer.
  "project_id": "<uuid>",
  "project_name": "...",
  "project_system_prompt": "...",
  "project_whitelist_mode": "inherit|allowlist|denylist|all",
  "project_allowed_tools": ["telegram__send_channel_post", ...],

  // HITL identity + policy. Forwarded by chat_proxy on every request so
  // the orchestrator's pause path persists non-empty IDs on
  // pending_tool_calls (pre-fix, these all defaulted to "" and every
  // persisted batch was unreachable by hydration).
  "user_id": "<uuid>",
  "message_id": "<uuid>",
  "business_approvals": {"<tool>": "auto|manual|forbidden"},
  "project_approval_overrides": {"<tool>": "auto|manual|forbidden"},

  // Per-chat locale forwarded from the API's chat_proxy. Body field wins
  // over Accept-Language header so an intermediate proxy can't silently
  // flip the LLM's reply language.
  "locale": "ru"
}
```

`message` is the only strictly required field. `business_id` is recommended:
empty or malformed values degrade to `uuid.Nil`, which the LLM router treats
as the skip-billing sentinel rather than writing a corrupt row.

### Request → RunRequest mapping

- The conversation ID comes from the URL path (`chi.URLParam`), not the
  body — keeps the persisted `PendingToolCallBatch.ConversationID`
  reachable from the GET `/messages` hydration filter.
- `BusinessVoiceTone` is resolved to a single comma-separated phrase via
  `joinTone(tones, locale)`; unknown / empty tokens are dropped silently.
- `ProjectWhitelistMode` is coerced to `""` (inherit ⇒ all) when not one
  of the four defined values, with a warning log — never crash on bad
  proxy input.
- `*prompt.ProjectContext` is non-nil only when `project_id != ""`; empty
  project ID is the "Без проекта" signal.
- `user_id` is parsed into `RunRequest.UserID` (uuid.UUID); parse failure
  leaves it zero with a warning log.

## SSE wire format

Response headers:

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

Each frame is a single `data:` line followed by a blank line:

```
data: <json-encoded sse.Event>\n\n
```

The handler calls `flusher.Flush()` after every frame so chunks reach the
client immediately — the SSE contract requires per-event flushing, not
per-write buffering. `X-Accel-Buffering: no` tells nginx not to coalesce.

### Event ordering

The handler is a pure relay: it pushes whatever order `orchestrator.Run`
emits onto the wire. Ordering invariants therefore live in
[`docs/orchestrator/run.md`](./run.md):

- `tool_call` precedes its matching `tool_result` / `tool_rejected` per
  call ID.
- `tool_approval_required` is emitted at most once per turn and is always
  followed by channel close.
- `done` is the terminal event on the happy path.
- Multiple parallel `tool_result` events may interleave; consumers
  correlate by `ToolCallID`.

## Error envelope

Two distinct error surfaces:

1. **HTTP-level** (request rejected before streaming starts) — written
   as JSON to the response body with the matching status code:
   - `400 {"error":"invalid request body"}` on JSON decode failure.
   - `400 {"error":"message is required"}` when `req.Message == ""`.
   - `500 streaming not supported` if the response writer is not a flusher.
2. **SSE-level** (errors raised during streaming) — `sse.Event{Type:
   "error", ...}`. Rate-limiter sentinels are translated in
   `translateRunnerError` to coded events with a localized fallback
   message; everything else falls through with `err.Error()` text so the
   existing legacy behavior is preserved:
   - `daily_spend_exceeded`
   - `rate_limit_unavailable`
   - `rate_limit_exceeded`

`ErrConversationTokenCap` is intentionally **not** translated in
`translateRunnerError`. The agent loop is the canonical emitter — `stepRun`
emits `Code="conversation_token_cap"` directly on the event channel — and
double-emission would be a bug.

## Locale dispatch

`resolveChatLocale(ctx, bodyLocale)` picks the language tag for the prompt
builder. Precedence:

1. `bodyLocale` — explicit string from the chat request body, set by the
   API's `chat_proxy` from the per-user `NEXT_LOCALE` cookie. Parsed via
   `i18n.MatchAcceptLanguage`, which handles single-tag input (`"en"`) and
   multi-tag preference lists (`"en-US,en;q=0.9"`) uniformly and returns
   `i18n.DefaultTag` on parse failure — so we never propagate a zero Tag.
2. `ctx tag` — populated by `middleware.Locale` earlier in the chain from
   the request's `Accept-Language` header.

Body-over-header is deliberate: a backend cookie value flips the
orchestrator language even when an intermediate proxy strips/rewrites
`Accept-Language`. An empty body field is the no-opinion signal that
defers to the header path. The resolved tag is stored on the request
context via `i18n.WithLocale` and on `BusinessContext.Locale` so the
prompt builder picks it up uniformly.

## Tone vocabulary

Stored tone identifiers are translated into per-locale adjectives via four
maps. The contract with the frontend (`lib/tones.ts`) is keyed off canonical
ids — keep the maps in sync.

| Map | Key | Value | Purpose |
|---|---|---|---|
| `toneIDToRu` | canonical id (`warm`, `calm`, …) | Russian adjective | RU prompt vocabulary |
| `toneIDToEn` | canonical id | English adjective | EN prompt vocabulary; same key set as `toneIDToRu` so a missing EN value is a compile-time inconsistency |
| `toneLegacyRuToRu` | legacy Russian display label | canonical Russian adjective | recognize pre-migration `voiceTone` values still in DB |
| `toneLegacyRuToID` | legacy Russian display label | canonical id | bridge legacy RU values to the EN map |

`toneLabel(token, tag)` is the unified resolver:

- EN path: direct id → `toneIDToEn`; fallback legacy RU → id → `toneIDToEn`.
- RU path: `toneIDToRu` for ids; `toneLegacyRuToRu` for legacy labels.
- Unknown tokens return `("", false)` — `joinTone` drops them.

`joinTone(tags, tag)` deduplicates and joins the resolved labels with
`", "`. Empty result signals "no opinion" so the prompt builder can use
its locale-appropriate default.

## Fan-out to orchestrator engine

The handler's request → `RunRequest` translation, then:

```go
events, err := h.runner.Run(ctx, runReq)
if err != nil { writeSSE(translateRunnerError(...)) ; return }
for event := range events {
    writeSSE(ctx, w, flusher, sseevent.FromEvent(event))
}
```

`sseevent.FromEvent` is the single mapping site between the internal
`orchestrator.Event` type and the wire `sse.Event` shape — both call sites
in this package (`Chat` and `Resume`) go through it so field-copy logic
lives in one place.

Context decoration applied before `Run`:

- `a2a.WithBusinessID(ctx, req.BusinessID)` — required by the NATS executor
  in downstream platform agents (`a2a.BusinessIDFromContext` lookup); without
  it, every tool call reaches platform agents with `business_id=""` and
  fails token resolution.
- `logger.WithCorrelationID(ctx, corrID)` — when the request carries an
  `X-Correlation-ID` header. Threads through structured logs.
- `i18n.WithLocale(ctx, locale)` — see *Locale dispatch* above.

## Friendly-message strings

`translateRunnerError` (bootstrap) and `translateChatError` in `step.go`
(in-loop) share the same two-locale switch. Strings are inlined as `const`
in each file rather than centralized in `pkg/i18n` — the catalog is too
small to justify the indirection and the `Event.Code` discriminator is the
machine contract, not the text.

| Code | RU | EN |
|---|---|---|
| `daily_spend_exceeded` | Достигнут дневной лимит расходов для этого бизнеса. Попробуйте завтра или свяжитесь с владельцем. | Daily spend limit reached for this business. Try again tomorrow or contact the owner. |
| `rate_limit_unavailable` | Сервис ограничения запросов временно недоступен. Попробуйте позже. | Rate limiter is temporarily unavailable. Please try again shortly. |
| `rate_limit_exceeded` | Слишком много запросов. Подождите минуту и повторите. | Too many requests. Wait a minute and try again. |

## Cross-references

- [`docs/orchestrator/run.md`](./run.md) — `Event` shapes, lifecycle.
- [`docs/orchestrator/resume.md`](./resume.md) — HITL resume HTTP path.
- [`docs/orchestrator/prompt.md`](./prompt.md) — `BusinessContext`,
  `ProjectContext`, `BuildSplit`.
- `pkg/sse` — `Event`, `Marshal`.
- `pkg/i18n` — `MatchAcceptLanguage`, `LocaleFromContext`, `DefaultTag`,
  `WithLocale`.
- `pkg/a2a` — `WithBusinessID`.
- `services/orchestrator/internal/sseevent` — `FromEvent` field mapper.
