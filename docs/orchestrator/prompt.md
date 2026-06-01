# Prompt Builder

`services/orchestrator/internal/prompt` assembles the system prompt that
fronts every LLM turn. It emits a two-block system message — Block 1
(platform-wide, byte-stable per locale) and Block 2 (per-business, varies
per request) — plus the conversation history. Block 1 is what Anthropic's
cross-business prompt cache keys against; keeping it byte-stable per locale
is the load-bearing invariant of this file.

## Public API

- `type BusinessContext` — all data needed to build the system prompt
  (Name, Category, Address, Phone, Website, Description, Tone,
  ActiveIntegrations, Now, Locale).
- `type ProjectContext` — optional project prompt layer (ID, Name,
  SystemPrompt, WhitelistMode, AllowedTools). Appended after the business
  rules block when a chat lives inside a project.
- `Build(ctx, proj, history) []llm.Message` — back-compat wrapper that
  returns a single concatenated `system` message followed by `history`.
  Retained for callers (titler, draft_reply) that still want the legacy
  shape.
- `BuildSplit(ctx, proj, history) (platform, business string, msgs []llm.Message)` —
  primary entry point. Returns the two system blocks separately so
  `stepRun` can stamp `cache_control` on Block 1 via
  `llm.SystemBlock.CacheBoundary`. New callers should prefer this.

## Locale handling

- `BusinessContext.Locale` may be any `language.Tag`, including the zero Tag.
- `Build` / `BuildSplit` normalize it exactly once via
  `i18n.NormalizeToSupported`; internal helpers trust their tag and never
  re-normalize. The "collapse arbitrary tag → Russian|English" rule lives
  in `pkg/i18n` alongside `MatchAcceptLanguage`, not scattered through
  `prompt/`.
- Anything that is not English produces the Russian template — preserving
  byte-for-byte pre-i18n output for legacy callers that leave `Locale`
  unset.
- `BusinessContext.Locale` is populated by
  `services/orchestrator/internal/handler/chat.go` from the per-request
  body's `locale` field, falling back to `i18n.LocaleFromContext`
  (LocaleResolver middleware).

The locale steers BOTH the section labels (`## Бизнес` vs `## Business`)
AND the trailing language directive (`Общайся на русском языке` vs
`Respond in English`). The directive is load-bearing — it is the single
string that flips the LLM's output language.

## Block layout

Block 1 (platform — locale-fixed, byte-stable):

- Preamble (`Ты — AI-ассистент...` / `You are an AI assistant...`)
- `## Правила` / `## Rules`
- `## Принципы работы с инструментами` / `## Tool-use principles`
- `## Стиль ответов` / `## Response style`
- `## Восстановление после ошибок` / `## Error recovery`
- `## Безопасность и приватность` / `## Safety and privacy`
- `## Тон по умолчанию` / `## Default tone`
- Trailing language directive

Block 2 (business — varies per request):

- `## Бизнес:` / `## Business:` header + business details (Name, Category,
  Address, Phone, Website, Description)
- `Тон общения:` / `Tone:`
- `Текущая дата и время:` / `Current date and time:`
- `## Активные интеграции` / `## Active integrations` list
- Optional `## Проект: ...` / `## Project: ...` block when `proj != nil`

## Caching invariants

- Anthropic stamps `cache_control` on Block 1 ONLY — see the
  `CacheBoundary` flag on `llm.SystemBlock` and `stampSystemCacheControl`
  in `pkg/llm/providers/anthropic.go`.
- Block 1 byte-stability across `BusinessContext` variations is guarded by
  `TestSystemPromptHash_Stability` (`builder_stability_test.go`).
- Block 1 size invariant: each platform block is padded to comfortably
  exceed Anthropic Sonnet 4.6's 1024-token cache minimum (~1100 tokens per
  locale). `TestSystemPromptHash_Stability_LockedHash` pins the per-locale
  sha256, so any rewording requires hash rotation.

## Block-order rationale

The legacy single-block builder emitted preamble → Business → Tone → Now →
Integrations → Rules. Splitting it reorders to preamble → Rules →
Business → Tone → Now → Integrations because the cache prefix MUST be
platform-only. Rules-first ordering is in fact preferred: platform rules
lead the prompt and carry the `cache_control` marker.

## Project layering

`appendProjectBlock` glues the project prompt layer onto the
business-only system text. The header is always emitted (even when
`SystemPrompt` is empty) so the LLM knows which project is in scope; this
makes the transition visible during move-chat.

Layering order: business context → project system prompt → conversation
history. The project block is appended AFTER the business rules because
LLM attention gives the last-emitted block precedence, which matches the
UX intent: project-level instructions override general business rules for
chats inside a project.

When `WhitelistMode` is explicit or none, a follow-up
`### Ограничения инструментов` / `### Tool restrictions` section is
emitted so the LLM knows to refuse / explain unavailable-platform
requests instead of silently substituting the closest allowed tool. The
two language variants intentionally carry the same semantic load
(refuse-with-explanation + anti-substitution instruction) so LLM
behavior is identical across locales. Tool names in the allowlist are
emitted verbatim in both locales because they are stable identifiers
the LLM matches against its tools schema, not user-facing copy.

`appendProjectBlock` trusts its `tag` argument: callers must have
normalized to one of `i18n.Supported` (Russian or English) — the
`Build` / `BuildSplit` entry points do.

### Defensive whitelist behavior

`WhitelistModeExplicit` with an empty `AllowedTools` slice should not
happen — the service layer rejects this via `ErrProjectWhitelistEmpty`.
The prompt builder defensively emits the same content as
`WhitelistModeNone` (all tools disabled) if it does.

## Cross-references

- `pkg/i18n` — `NormalizeToSupported`, `LocaleFromContext`, supported tags.
- `pkg/llm` — `SystemBlock`, `ChatRequest.SystemBlocks`, `CacheBoundary`.
- `pkg/llm/providers/anthropic.go` — `stampSystemCacheControl`.
- `services/orchestrator/internal/orchestrator/orchestrator.go` —
  `Run` consumer of `BuildSplit`.
- `services/orchestrator/internal/handler/chat.go` — `BusinessContext.Locale`
  populator.
- `docs/architecture.md` — top-level system flow.
