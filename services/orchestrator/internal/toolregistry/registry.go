package toolregistry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// Executor handles the execution of a tool call.
type Executor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// ApprovalExecutor extends Executor with a variant that accepts an approvalID
// propagated into the dispatch payload (a2a.ToolRequest.ApprovalID
// field). Implemented by natsexec.NATSExecutor; internal-only executors that
// never pass through HITL approval do not need to implement it — Registry
// falls back to plain Execute (discarding approvalID, which is correct for
// tools that have no agent-side Redis dedupe).
type ApprovalExecutor interface {
	Executor
	ExecuteWithApproval(ctx context.Context, args map[string]interface{}, approvalID string) (interface{}, error)
}

// ExecutorFunc is a function that implements Executor.
type ExecutorFunc func(ctx context.Context, args map[string]interface{}) (interface{}, error)

func (f ExecutorFunc) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return f(ctx, args)
}

type entry struct {
	def             llm.ToolDefinition
	displayName     string
	userDescription string // human-readable description surfaced in settings UI (LLM-facing description stays in def.Function.Description).
	// displayNameKey is the i18n catalog key the FE uses to render the
	// agent_tasks task title (Phase C3/D3). Empty means "no key" — the FE
	// falls back to the legacy displayName literal. Wired through
	// SetDisplayNameKey from the toolSpec definitions in internal/wire.
	displayNameKey string
	// descriptionEn is the English translation of def.Function.Description.
	// def.Function.Description stays RU (the source-of-truth literal); the
	// EN variant is swapped in at AvailableForLocale / AllEntriesForLocale
	// time when the request locale resolves to English. Empty means "no
	// translation registered" — the RU description is used in both locales
	// (safe fallback; matches pkg/i18n catalog lookup semantics).
	descriptionEn string
	// parameterDescriptionsEn maps each JSON-schema parameter name to its
	// English description. The RU descriptions stay inline in
	// def.Function.Parameters as the source of truth; the EN variant is
	// swapped in by localizeDef when the request locale is English. Nil /
	// empty means "no parameter translations" — the schema is returned
	// verbatim with RU descriptions, preserving byte-compat for the legacy
	// (RU-default) callers.
	parameterDescriptionsEn map[string]string
	executor                Executor
	floor                   domain.ToolFloor
	editableFields          []string
}

// Registry holds tool definitions and their executors.
type Registry struct {
	tools map[string]entry
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]entry)}
}

// Register adds a tool definition with its executor (may be nil for stub tools),
// the human-readable displayName surfaced on the Tasks page, the ToolFloor
// baseline, and the per-tool EditableFields allowlist for HITL
// edit-args validation.
//
// The caller MUST pass all five arguments explicitly — every registration site
// in services/orchestrator/cmd/main.go must deliberately choose a floor and an
// edit allowlist. There is no default so that a newly-added tool can never
// silently inherit an unsafe policy. EditableFields is copied defensively so
// subsequent caller-side mutations cannot change registered behavior.
//
// Convention: EditableFields is always lowercase_with_underscore
// matching the tool's JSON arguments schema keys. The comparison performed by
// ValidateEditArgs is case-sensitive.
func (r *Registry) Register(
	def llm.ToolDefinition,
	displayName string,
	exec Executor,
	floor domain.ToolFloor,
	editableFields []string,
) {
	r.tools[def.Function.Name] = entry{
		def:            def,
		displayName:    displayName,
		executor:       exec,
		floor:          floor,
		editableFields: append([]string(nil), editableFields...),
	}
}

// DisplayName returns the human-readable label registered for the named tool.
// Returns an empty string for unknown tools.
func (r *Registry) DisplayName(name string) string {
	e, ok := r.tools[name]
	if !ok {
		return ""
	}
	return e.displayName
}

// SetUserDescription attaches a short user-facing description to a
// previously-registered tool. This is what renders in /settings/tools and the
// project approval overrides; the LLM-facing description (kept on
// def.Function.Description) may reference other tool names and disambiguation
// rules that would be confusing to surface in the UI. No-op if the tool is
// not registered.
func (r *Registry) SetUserDescription(name, text string) {
	e, ok := r.tools[name]
	if !ok {
		return
	}
	e.userDescription = text
	r.tools[name] = e
}

// SetDisplayNameKey attaches the i18n catalog key the frontend uses to render
// localized task titles. No-op if the tool is not registered. Wired from the
// toolSpec.displayNameKey field in internal/wire. Phase C3 introduced the
// field; Phase D3 wires it through to the registry, SSE events, and the
// AgentTask repo writes so the FE renders t(displayNameKey) || displayName.
func (r *Registry) SetDisplayNameKey(name, key string) {
	e, ok := r.tools[name]
	if !ok {
		return
	}
	e.displayNameKey = key
	r.tools[name] = e
}

// DisplayNameKey returns the i18n catalog key for the given tool, or "" if
// unknown / unset. Used by the orchestrator's tool-dispatch loop to populate
// the ToolCall SSE event so chat_proxy can stamp it onto AgentTask writes.
func (r *Registry) DisplayNameKey(name string) string {
	e, ok := r.tools[name]
	if !ok {
		return ""
	}
	return e.displayNameKey
}

// SetDescriptionEn attaches the English translation of the tool's LLM-facing
// description. No-op if the tool is not registered. Used by Phase D3 to make
// the LLM tools schema locale-aware — the orchestrator's chat handler and
// the public /internal/tools endpoint both pick RU vs EN per request.
func (r *Registry) SetDescriptionEn(name, text string) {
	e, ok := r.tools[name]
	if !ok {
		return
	}
	e.descriptionEn = text
	r.tools[name] = e
}

// SetParameterDescriptionsEn attaches a map of parameter-name -> English
// description for the named tool. The map is copied defensively so subsequent
// caller-side mutations cannot change registered behavior. No-op if the tool
// is not registered. A nil or empty map is treated as "no translations" —
// localizeDef leaves the schema verbatim, preserving byte-compat with legacy
// (RU-default) callers.
//
// Closes the Phase D3 deferred TODO that left parameter descriptions RU-only;
// see services/orchestrator/internal/wire/tools.go for the populated maps and
// .planning/i18n-readiness/PLAN.md (D3 + AD-3) for the design rationale.
func (r *Registry) SetParameterDescriptionsEn(name string, m map[string]string) {
	e, ok := r.tools[name]
	if !ok {
		return
	}
	if len(m) == 0 {
		e.parameterDescriptionsEn = nil
	} else {
		cp := make(map[string]string, len(m))
		for k, v := range m {
			cp[k] = v
		}
		e.parameterDescriptionsEn = cp
	}
	r.tools[name] = e
}

// Available returns tool definitions available for the given active integrations.
// Tools named "{platform}__{action}" are included only if platform is active.
// Tools without "__" are always included (internal tools).
//
// Descriptions are returned in the registry's source-of-truth language (RU).
// Use AvailableForWhitelist (ctx-aware) for the locale-resolved variant.
func (r *Registry) Available(activeIntegrations []string) []llm.ToolDefinition {
	active := make(map[string]bool, len(activeIntegrations))
	for _, p := range activeIntegrations {
		active[p] = true
	}

	result := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, e := range r.tools {
		name := e.def.Function.Name
		idx := strings.Index(name, "__")
		if idx == -1 {
			// Internal tool — always available
			result = append(result, e.def)
			continue
		}
		platform := name[:idx]
		if active[platform] {
			result = append(result, e.def)
		}
	}
	return result
}

// localizeDef returns a llm.ToolDefinition with the top-level Description and
// nested parameter `properties.<name>.description` fields swapped to the
// per-locale text. The function Name, the JSON-schema shape, parameter types,
// `required` lists, and any non-`description` keys (`enum`, `default`, etc.)
// remain verbatim.
//
// Non-English locales (and the zero Tag) short-circuit and return the
// registered def by value — preserving byte-identical output for the
// RU-default callers that existed before Phase D3 / its follow-up. This
// matters for the snapshot-style tests downstream that assert the exact
// schema sent to the LLM in the legacy path.
//
// For English locales, the original def is NOT mutated: the Function struct
// is copied by value, the Parameters map is rebuilt via a deep walk that only
// allocates new sub-maps along the property path being swapped, and parameters
// without a registered EN description keep their RU description (graceful
// degradation — never serve an empty description to the LLM).
func (r *Registry) localizeDef(e entry, tag language.Tag) llm.ToolDefinition {
	if tag != language.English {
		return e.def
	}
	// Nothing to localize for this entry — fast path matches legacy bytes.
	if e.descriptionEn == "" && len(e.parameterDescriptionsEn) == 0 {
		return e.def
	}

	out := e.def
	out.Function = e.def.Function // copy Function struct by value
	if e.descriptionEn != "" {
		out.Function.Description = e.descriptionEn
	}
	if len(e.parameterDescriptionsEn) > 0 {
		out.Function.Parameters = localizeParameters(e.def.Function.Parameters, e.parameterDescriptionsEn)
	}
	return out
}

// localizeParameters returns a deep-copied JSON-schema parameters map with
// `properties.<name>.description` fields swapped to the EN values from
// translations. Properties not listed in translations keep their original
// (RU) descriptions, and any non-`description` keys under each property are
// preserved by reference (they are immutable JSON values that we never mutate).
//
// The top-level map and the `properties` sub-map are always reallocated when
// translations is non-empty so callers can compare maps by identity without
// observing source mutation. Individual property entries are reallocated only
// when their description is actually being swapped — this minimizes allocation
// pressure on a hot path (this runs once per tool per chat request).
//
// If params is nil or has no `properties` key (zero-parameter tool), the
// original map is returned by reference — there's nothing to localize and
// reallocating an empty parent would just churn the GC.
func localizeParameters(params map[string]interface{}, translations map[string]string) map[string]interface{} {
	if params == nil {
		return nil
	}
	propsRaw, ok := params["properties"]
	if !ok {
		return params
	}
	props, ok := propsRaw.(map[string]interface{})
	if !ok || len(props) == 0 {
		return params
	}

	// Shallow-clone the top-level params map (keeps `type`, `required` etc. by reference).
	outParams := make(map[string]interface{}, len(params))
	for k, v := range params {
		outParams[k] = v
	}
	// Build a new `properties` map; per-property entries are cloned only when
	// their description is actually being swapped.
	outProps := make(map[string]interface{}, len(props))
	for name, raw := range props {
		propMap, ok := raw.(map[string]interface{})
		if !ok {
			outProps[name] = raw
			continue
		}
		enDesc, hasEN := translations[name]
		if !hasEN || enDesc == "" {
			outProps[name] = raw
			continue
		}
		// Clone this property so the source map is not mutated.
		clone := make(map[string]interface{}, len(propMap))
		for k, v := range propMap {
			clone[k] = v
		}
		clone["description"] = enDesc
		outProps[name] = clone
	}
	outParams["properties"] = outProps
	return outParams
}

// AvailableForWhitelist applies a typed WhitelistMode filter on top
// of Available. Unknown tool names in `allowed` are logged (slog WARN) and
// silently dropped — the safe-default behavior (whitelist drift: a renamed
// or missing tool is treated as denied rather than surfaced as an error).
//
// ctx is the request-scoped context threaded from orchestrator.Run. It is
// used for slog attribution (correlation_id, business_id) and cancellation
// hygiene. Callers MUST NOT fabricate a root context here — there is always
// a request-scoped ctx available at the call site in Run.
//
// v1.3 scope note: inherit == all. Replaced later with a business-level
// tool_approvals map that will serve as the actual "inherited" baseline;
// until then, the baseline is "every registered tool for the active
// integrations".
//
// Auto-floor read tools are always included in WhitelistModeExplicit. Tools
// registered with ToolFloorAuto are read-only / safe queries by definition
// (cmd/main.go's policy guideline: "no external side effects") — denying
// the LLM a way to read public state forces it to hallucinate around the
// missing context (e.g. clicking "Проверить отзывы" with only a write tool
// in the whitelist made the LLM publish posts ABOUT checking reviews
// instead of fetching them). Whitelist intent is to gate write actions;
// HITL still gates execution of every Manual-floor tool, so this exemption
// does not weaken the security posture. WhitelistModeNone remains absolute
// — if the operator picks "no tools", we honor it.
func (r *Registry) AvailableForWhitelist(
	ctx context.Context,
	activeIntegrations []string,
	mode domain.WhitelistMode,
	allowed []string,
) []llm.ToolDefinition {
	// Resolve locale from ctx once; localizeDef per-entry swaps the RU
	// Description for the EN one when the request is English (Phase D3).
	// LocaleFromContext never returns the zero Tag — defaults to i18n.DefaultTag
	// (RU) — so the localization branch is a clean no-op for the legacy path.
	tag := i18n.LocaleFromContext(ctx)
	base := r.availableLocalized(activeIntegrations, tag)
	switch mode {
	case "", domain.WhitelistModeInherit, domain.WhitelistModeAll:
		return base
	case domain.WhitelistModeNone:
		return []llm.ToolDefinition{}
	case domain.WhitelistModeExplicit:
		known := make(map[string]bool, len(r.tools))
		for name := range r.tools {
			known[name] = true
		}
		allowSet := make(map[string]bool, len(allowed))
		for _, name := range allowed {
			if !known[name] {
				slog.WarnContext(ctx, "project whitelist contains unknown tool",
					"tool", name,
				)
				continue
			}
			allowSet[name] = true
		}
		result := make([]llm.ToolDefinition, 0, len(base))
		for _, def := range base {
			name := def.Function.Name
			if allowSet[name] || r.tools[name].floor == domain.ToolFloorAuto {
				result = append(result, def)
			}
		}
		return result
	default:
		slog.WarnContext(ctx, "unknown whitelist mode, falling back to inherit",
			"mode", string(mode),
		)
		return base
	}
}

// availableLocalized is the locale-aware twin of Available. Returns the same
// set of tools, with Description swapped to the requested locale for entries
// that registered a translation. Internal: callers should use
// AvailableForWhitelist (the public surface that also enforces whitelist
// rules).
func (r *Registry) availableLocalized(activeIntegrations []string, tag language.Tag) []llm.ToolDefinition {
	active := make(map[string]bool, len(activeIntegrations))
	for _, p := range activeIntegrations {
		active[p] = true
	}
	result := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, e := range r.tools {
		name := e.def.Function.Name
		idx := strings.Index(name, "__")
		if idx == -1 {
			result = append(result, r.localizeDef(e, tag))
			continue
		}
		if active[name[:idx]] {
			result = append(result, r.localizeDef(e, tag))
		}
	}
	return result
}

// Execute runs the registered executor for the named tool.
// Returns an error if the tool is unknown or the executor is nil.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	e, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %q", name)
	}
	if e.executor == nil {
		return nil, fmt.Errorf("tool %q has no executor (NATS unavailable)", name)
	}
	return e.executor.Execute(ctx, args)
}

// ExecuteWithApproval runs the registered executor with the given approvalID
// propagated into the dispatch payload when the executor implements
// ApprovalExecutor (production NATS executors do). Executors that do not
// implement ApprovalExecutor fall back to plain Execute — safe for internal
// tools that have no agent-side dedupe requirement.
//
// Called by the Resume path after parsing a resolved batch:
// approvalID is always "<batch_id>-<call_id>", so each approved call
// within a batch has a unique dedupe key for the agent's Redis SetNX
// (pkg/hitldedupe).
func (r *Registry) ExecuteWithApproval(ctx context.Context, name string, args map[string]interface{}, approvalID string) (interface{}, error) {
	e, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %q", name)
	}
	if e.executor == nil {
		return nil, fmt.Errorf("tool %q has no executor (NATS unavailable)", name)
	}
	if ae, ok := e.executor.(ApprovalExecutor); ok {
		return ae.ExecuteWithApproval(ctx, args, approvalID)
	}
	// Fallback: executor doesn't carry approval metadata — safe for
	// internal/stub tools with no agent-side dedupe.
	return e.executor.Execute(ctx, args)
}

// Floor returns the registered ToolFloor for toolName or ToolFloorForbidden
// if the tool is unknown (safe default — the runtime policy resolver treats
// unknown tools as "not permitted", matching the startup validation sweep
// that logs tool_approval_whitelist_unknown for entries referencing missing
// tools).
func (r *Registry) Floor(toolName string) domain.ToolFloor {
	if e, ok := r.tools[toolName]; ok {
		return e.floor
	}
	return domain.ToolFloorForbidden
}

// EditableFields returns the registered edit allowlist for toolName, or nil
// if the tool is unknown. The returned slice is a defensive copy — mutating
// it does not alter registry state. The list is always lowercase_with_underscore
// matching the tool's JSON args schema.
func (r *Registry) EditableFields(toolName string) []string {
	if e, ok := r.tools[toolName]; ok {
		return append([]string(nil), e.editableFields...)
	}
	return nil
}

// Has reports whether toolName is currently registered. Used by the
// startup validation sweep to detect whitelist entries that reference a tool
// which has been renamed or removed between deploys.
func (r *Registry) Has(toolName string) bool {
	_, ok := r.tools[toolName]
	return ok
}

// AllFloors returns a snapshot of every registered tool's floor. Used by
// startup validation and by GET /api/v1/tools to populate the settings UI's
// per-tool toggles.
func (r *Registry) AllFloors() map[string]domain.ToolFloor {
	out := make(map[string]domain.ToolFloor, len(r.tools))
	for name, e := range r.tools {
		out[name] = e.floor
	}
	return out
}

// RegistryEntry is the projection exposed by GET /api/v1/tools.
// Kept in the tools package so the API handler can import a typed shape.
type RegistryEntry struct {
	Name            string           `json:"name"`
	DisplayName     string           `json:"displayName"`              // human-readable label (e.g., "Отправить пост") shown in settings UI; may be empty — frontend falls back to Name.
	DisplayNameKey  string           `json:"displayNameKey,omitempty"` // i18n catalog key for the FE; Phase D3. Empty → FE falls back to DisplayName.
	Platform        string           `json:"platform"`                 // e.g., "telegram" — derived from {platform}__{action}
	Floor           domain.ToolFloor `json:"floor"`
	EditableFields  []string         `json:"editableFields"`
	Description     string           `json:"description"`     // LLM-facing description — includes tool-name references and disambiguation rules. Locale-resolved when fetched via AllEntriesForLocale.
	UserDescription string           `json:"userDescription"` // end-user-facing description shown in settings UI; never references other tool names.
}

// AllEntries returns a snapshot of (name, displayName, platform, floor, editable, description)
// for every registered tool. Description is returned in the registry's
// source-of-truth language (RU). Feeds GET /api/v1/tools as well as the
// cluster-internal /internal/tools/names endpoint used by the startup
// validation sweep.
//
// Callers that need locale-aware descriptions (the live /internal/tools
// endpoint reached from the API on every cache miss) should use
// AllEntriesForLocale.
func (r *Registry) AllEntries() []RegistryEntry {
	return r.AllEntriesForLocale(i18n.DefaultTag)
}

// AllEntriesForLocale returns the same projection as AllEntries but with the
// per-tool Description resolved to the requested locale (Phase D3). Tools
// without a descriptionEn fall back to def.Function.Description (RU) — the
// frontend keeps showing the RU description rather than nothing.
//
// DisplayName + UserDescription are NOT swapped here — the frontend already
// renders them via its own i18n catalog (using the displayNameKey field and
// `tools.{platform}.{action}.description` keys respectively) and round-tripping
// translations through the backend would duplicate the source of truth.
func (r *Registry) AllEntriesForLocale(tag language.Tag) []RegistryEntry {
	out := make([]RegistryEntry, 0, len(r.tools))
	for _, e := range r.tools {
		platform := toolPlatform(e.def.Function.Name)
		desc := e.def.Function.Description
		if tag == language.English && e.descriptionEn != "" {
			desc = e.descriptionEn
		}
		out = append(out, RegistryEntry{
			Name:            e.def.Function.Name,
			DisplayName:     e.displayName,
			DisplayNameKey:  e.displayNameKey,
			Platform:        platform,
			Floor:           e.floor,
			EditableFields:  append([]string(nil), e.editableFields...),
			Description:     desc,
			UserDescription: e.userDescription,
		})
	}
	return out
}

// toolPlatform extracts the prefix of a "{platform}__{action}" tool name.
// Returns "" for bare (internal) tools without the "__" separator and for
// names that start with "__" (edge case: leading separator means no platform
// prefix).
func toolPlatform(toolName string) string {
	idx := strings.Index(toolName, "__")
	if idx <= 0 {
		return ""
	}
	return toolName[:idx]
}
