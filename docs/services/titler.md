# Titler

Generates short titles for chats via the cheap `TITLER_MODEL` and writes them atomically via the conditional `UpdateTitleIfPending` repo path. All operations are best-effort fire-and-forget; failures degrade silently. The service composes `pkg/security/pii` so PII never reaches the cheap LLM endpoint and gates the post-hoc title check.

## Public API

- `NewTitler(router, repo, model)` — constructs a Titler. All three dependencies are mandatory; nil router, nil repo, or empty model is a wiring bug rejected with a panic at construction time (mirrors `hitl.go:92-123`). The `router` parameter is typed as `chatCaller`; `*llm.Router` satisfies it implicitly via its existing `Chat` method, so production callers pass the real Router and Go's structural typing handles the conversion.
- `(*Titler).GenerateAndSave(ctx, businessID, conversationID, userMsg, assistantMsg)` — runs the full auto-title pipeline.

## Tunables

- `titleMaxChars` (80) — caps the cleaned title length in RUNES (not bytes — Russian runes are multi-byte). Discretionary 60–80; the upper bound was chosen so titles can include a parenthetical clarifier where useful.
- `titleMaxOutputTokens` (30) — research recommends 20–30; chose 30 so the cheap model has a small budget for sanitize-friendly punctuation it will then strip in `sanitizeTitle`.
- `titleTemperature` (0.3) — research-recommended 0.3 (deterministic enough to avoid title drift across regenerations of similar inputs while still producing varied wording for diverse chats).
- `titleSystemPromptRu` / `titleSystemPromptEn` — cheap-model instructions. "Без кавычек и точек в конце" / "No quotes or trailing punctuation" pre-empts most of what `sanitizeTitle` would otherwise have to strip. Length-target ("3–6 слов" / "3–6 words") is kept identical across locales so output budgets behave the same way.

`titleSystemPrompt(tag)` returns the cheap-model instruction in the requested locale. EN for `language.English`, RU otherwise — matches the catalog default in `pkg/i18n.lookup`.

`userTemplate(tag)` returns the per-locale "Пользователь: …\n\nАссистент: …" / "User: …\n\nAssistant: …" framing for the cheap-LLM input. Same shape as the system prompt above — kept inline rather than catalog-based because it is a positional `fmt.Sprintf` template, not a translatable user-facing string.

## LLM-call seam (`chatCaller`)

`chatCaller` is the canonical mocking seam for the LLM-call dependency the Titler holds. It is package-private (intentionally lowercase) and exists for two reasons:

1. `*llm.Router` (concrete) satisfies it implicitly via its existing `Chat(ctx, req) (*llm.ChatResponse, error)` method — production wiring in `services/api/cmd/main.go` passes the real `Router`; Go's structural typing handles the conversion at the call site without any adapter.
2. Tests use a `fakeRouter` that records the `ChatRequest` and returns canned responses, without spinning up real LLM provider stubs.

This is the SINGLE SOURCE OF TRUTH for the LLM-call seam. Callers reference `*service.Titler` concretely; there is no parallel `titlerCaller` interface.

## Pipeline: `GenerateAndSave`

1. Pre-redact `userMsg` + `assistantMsg` via `security.RedactPII`. The cheap LLM never sees raw PII.
2. Build a `llm.ChatRequest` with `Model = t.model`, `Tier = "background"`, and NO Tools (titler MUST NOT tool-call).
3. Call `t.router.Chat`. On error → log + `recordAttempt` + return; status stays `auto_pending` so the next complete turn re-fires.
4. `sanitizeTitle` the response. If empty → log + `recordAttempt` + return.
5. `security.ContainsPIIClass` on the cleaned title. On match → write the language-neutral empty-title marker via `UpdateTitleIfPending` under the SAME atomic guard so a manual rename mid-flight still wins.
6. Otherwise call `UpdateTitleIfPending` with the cleaned title. `ErrConversationNotFound` → manual rename won the race (INFO-level).

### Context-handling contract

Caller MUST pass a long-lived `ctx`. The request `ctx` from `chat_proxy.go` is unsafe — see `chat_proxy.go`'s `persistCtx` pattern. Locale resolved from request context (set by `middleware.Locale` earlier in the chain) drives the cheap-model system prompt. `ctx` carries the request's `Accept-Language` all the way here because `chat_proxy` spawns the titler with a detached-but-correlated `ctx` that includes the i18n key.

### Logging discipline

All log lines are strictly metadata-only — never the prompt body, the assistant text, the redacted text, the LLM response, or the generated title.

### Billing attribution

`businessID` (string from the conversation document) is parsed into the typed `uuid.UUID` expected by `ChatRequest.BusinessID` so `logBilling` attributes the auto-title LLM call against the conversation's business. Malformed values degrade to `uuid.Nil` — router's nil-guard skips billing rather than write a corrupt row (fail-closed).

`UserID` is intentionally `uuid.Nil`: this is a system-level call with no rate-limit attribution.

## Idempotence: `UpdateTitleIfPending`

All title writes go through the conditional `UpdateTitleIfPending` repo path. `MatchedCount=0` → user renamed (`manual_won_race`) or doc deleted. The `ErrConversationNotFound` outcome is logged at INFO level: this is a feature, not a bug. Manual rename is sovereign.

The PII-terminal fallback path is also written under the SAME atomic guard so a concurrent manual rename still wins, even on the rejection branch.

## Post-hoc PII gate

After sanitization, `security.ContainsPIIClass` checks the cleaned title. On
match the service writes an empty title under the conditional write and logs
WARN with the `regex_class`. Provider errors and empty responses settle the same
way. The repository atomically sets `title_status: "auto"`, making the empty title
an explicit, language-neutral fallback marker. Clients localize it at display
time from `createdAt`; existing stored titles are never rewritten. See
[the conversation read contract](../api/handlers/conversation.md#fallback-title-read-contract).

## Helpers

- `sanitizeTitle(raw)` strips quotes, trailing punctuation, and surrounding whitespace from `raw`, then caps the result at `titleMaxChars` RUNES (not bytes — Russian runes are multi-byte; bounding by `len()` would cut titles mid-codepoint). The cheap-model system prompt already says "Без кавычек и точек в конце"; this helper is the post-hoc safety net for instruction-following slips.

## Cross-references

- [docs/architecture.md](../architecture.md)
- `pkg/security/pii` — pre-redaction and post-hoc PII regex.
- `pkg/llm.Router` — production `chatCaller` implementation.
- `services/api/internal/handler/chat_proxy.go` — caller; documents the detached `persistCtx` pattern used to fan out auto-title.
