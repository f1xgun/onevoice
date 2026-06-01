# Tool Registry

`services/orchestrator/internal/toolregistry` is the orchestrator's in-process
catalog of LLM-callable tools. Per-platform tool specs are registered at
startup (see `internal/wire/tools_*.go`); the agent loop consults the registry
on every iteration to derive the tool set offered to the LLM, dispatches calls
back through it, and reads each tool's HITL floor for pause-time policy
classification.

The registry is the source of truth for tool metadata at the orchestrator
edge: name, display name, i18n catalog key, platform prefix, floor, editable
fields, and per-locale descriptions. The same projection is served to the API
via `GET /internal/tools` (consumed by `ToolsRegistryCache` in
`services/api/internal/service/hitl.go`).

## Public API

- `type Executor interface { Execute(ctx, args) (interface{}, error) }` —
  Minimum contract for a tool runtime.
- `type ApprovalExecutor interface { Executor; ExecuteWithApproval(ctx, args, approvalID) (...) }` —
  Optional extension implemented by `natsexec.NATSExecutor` so the
  `approvalID` (the `<batch_id>-<call_id>` dedupe key) reaches the agent's
  Redis SetNX guard. Internal/stub executors that have no agent-side dedupe
  do not need to implement it — `Registry` falls back to plain `Execute`.
- `type ExecutorFunc func(...) (...)` — function adapter for `Executor`.
- `type ToolSpec struct { Def, DisplayName, DisplayNameKey, UserDescription, DescriptionEn, ParameterDescriptionsEn, Floor, EditableFields }` —
  Declarative description of a tool at registration time. Pure data; the
  runtime executor is passed separately to `Register` so per-platform spec
  builders in `internal/wire/` don't have to thread the NATS connection
  through.
- `type Registry struct { ... }` — holds tool definitions and their executors.
- `NewRegistry() *Registry` — fresh empty registry.
- `Register(spec ToolSpec, exec Executor)` — store spec keyed on
  `spec.Def.Function.Name`; `exec` may be nil for stub tools (NATS
  unavailable — registry still answers metadata queries but `Execute` errors).
- `DisplayName(name) string`, `DisplayNameKey(name) string` — labels for the
  named tool; empty string when unknown / unset.
- `Available(activeIntegrations) []llm.ToolDefinition` — tool defs filtered
  to active platforms (returns RU descriptions).
- `AvailableForWhitelist(ctx, activeIntegrations, mode, allowed) []llm.ToolDefinition` —
  locale-aware tool set with `WhitelistMode` filter applied.
- `Execute(ctx, name, args) (interface{}, error)` — runs the registered
  executor.
- `ExecuteWithApproval(ctx, name, args, approvalID) (interface{}, error)` —
  same, but propagates `approvalID` to executors that implement
  `ApprovalExecutor`.
- `Floor(toolName) domain.ToolFloor` — registered floor, or `ToolFloorForbidden`
  when unknown.
- `EditableFields(toolName) []string` — defensive copy of the edit allowlist,
  or nil when unknown.
- `Has(toolName) bool` — reports whether `toolName` is currently registered.
- `AllFloors() map[string]domain.ToolFloor` — snapshot used by startup
  validation and `GET /api/v1/tools`.
- `AllEntries() []RegistryEntry` — projection (RU descriptions) for
  `GET /api/v1/tools` and `/internal/tools/names`.
- `AllEntriesForLocale(tag) []RegistryEntry` — locale-aware variant
  consumed by the live `/internal/tools` endpoint on every API-side cache
  miss.
- `type RegistryEntry = domain.ToolEntry` — alias so the orchestrator and the
  API share one canonical JSON shape across the `internal/` visibility wall.
  Field documentation lives in `pkg/domain/tool_entry.go`.

## Policy: choosing a Floor at registration

- `ToolFloorAuto` — read-only / safe queries with no external side effects.
- `ToolFloorManual` — any public mutation (post, reply, update, schedule,
  upload). `EditableFields` covers ONLY human-facing text fields
  (text/caption/description); ids, recipients, URLs, dates, categories, and
  quantities are pinned at pause time.
- `ToolFloorForbidden` — reserved for actions that must NEVER be lifted via
  settings (e.g., a hypothetical "wipe all posts"). Kept registered so the
  LLM sees it exists but `policy.Resolve` always denies.
  Destructive-but-legitimate operations (comment moderation, etc.) belong
  under Manual, not Forbidden — users with a valid use-case can opt into
  auto-approval.

When in doubt, prefer Manual + a narrow `EditableFields` list (conservative
default). `EditableFields` is always `lowercase_with_underscore` matching
the tool's JSON arguments schema keys; `ValidateEditArgs` performs a
case-sensitive comparison. There is no default Floor — every registration
site must deliberately choose so a newly-added tool cannot silently inherit
an unsafe policy.

## Whitelist semantics

`AvailableForWhitelist` applies a typed `WhitelistMode` filter on top of
`Available`:

- `""` / `WhitelistModeInherit` / `WhitelistModeAll` → permissive baseline
  (every registered tool for the active integrations). v1.3 scope note:
  inherit == all. The business-level `tool_approvals` map will become the
  actual "inherited" baseline in a later milestone; until then the baseline
  is "every registered tool for the active integrations".
- `WhitelistModeNone` → absolute no-tools. Honored even when the operator
  picks it explicitly.
- `WhitelistModeExplicit` → `allowed` allowlist plus all `ToolFloorAuto`
  tools. Auto-floor read tools are always included because read-only / safe
  queries have no external side effects, and denying the LLM a way to read
  public state forces it to hallucinate around the missing context (a real
  failure mode: clicking "Проверить отзывы" with only a write tool in the
  whitelist made the LLM publish posts ABOUT checking reviews instead of
  fetching them). The whitelist's intent is to gate write actions; HITL
  still gates execution of every Manual-floor tool, so this exemption does
  not weaken the security posture.
- unknown mode → log WARN, fall back to inherit.

Unknown tool names in `allowed` are logged (`slog WARN`) and silently
dropped — the safe-default behavior (whitelist drift: a renamed or missing
tool is treated as denied rather than surfaced as an error).

`ctx` is the request-scoped context threaded from `orchestrator.Run`. It is
used for slog attribution (correlation_id, business_id) and cancellation
hygiene. Callers MUST NOT fabricate a root context — there is always a
request-scoped `ctx` available at the call site in `Run`.

## Tool naming and platform routing

Tools are named `{platform}__{action}` (e.g. `telegram__send_channel_post`).
`Available` filters by the `{platform}` prefix; bare names (no `__`) are
treated as internal tools and are always included.

`toolPlatform` returns the prefix of `{platform}__{action}` or `""` for bare
internal tools. Names with a leading `__` (no platform prefix) return `""`
as well — edge case handled by the same `strings.Index <= 0` guard.

## Localization

`Available` returns descriptions in the registry's source-of-truth language
(Russian). The locale-aware twin `availableLocalized` (internal helper used
by `AvailableForWhitelist`) and `AllEntriesForLocale` swap each tool's
description to the requested locale when a translation is registered.

`localizeDef` copies `def` with `Function.Description` and
`Function.Parameters` swapped to the per-locale values. Non-English locales
(and the zero `language.Tag`) return `e.def` by value, preserving
byte-identical output. `localizeParameters` deep-copies the parameters
schema and rewrites `properties.<name>.description`; properties without a
translation keep their original description; the source map is never
mutated.

## Defensive-copy invariants

`Register` copies `EditableFields` and `ParameterDescriptionsEn` so caller-
side mutations after registration cannot change registered behavior.
`EditableFields(toolName)` returns a defensive copy on every call.

## Cross-references

- `pkg/domain/tool_entry.go` — `ToolEntry` field documentation.
- `pkg/llm` — `ToolDefinition` shape on the LLM wire.
- `pkg/hitl` — pause-time floor evaluation.
- `services/orchestrator/internal/wire/tools_*.go` — per-platform spec
  registration sites.
- `services/api/internal/service/hitl.go` — `ToolsRegistryCache` consumer
  of `/internal/tools`.
- `docs/orchestrator/run.md`, `docs/orchestrator/resume.md` — how
  `Floor` / `Execute` / `ExecuteWithApproval` participate in the agent loop.
- `docs/architecture.md` — top-level system flow.
