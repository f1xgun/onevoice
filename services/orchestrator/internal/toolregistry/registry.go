// Package toolregistry is the orchestrator's in-process catalog of LLM-callable
// tools. See docs/orchestrator/toolregistry.md.
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

// ApprovalExecutor is Executor plus a variant that propagates an approvalID
// into the dispatch payload. See docs/orchestrator/toolregistry.md.
type ApprovalExecutor interface {
	Executor
	ExecuteWithApproval(ctx context.Context, args map[string]interface{}, approvalID string) (interface{}, error)
}

// ExecutorFunc is a function that implements Executor.
type ExecutorFunc func(ctx context.Context, args map[string]interface{}) (interface{}, error)

// Execute implements Executor.
func (f ExecutorFunc) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return f(ctx, args)
}

type entry struct {
	def                     llm.ToolDefinition
	displayName             string
	userDescription         string // human-readable; LLM-facing description stays in def.Function.Description
	displayNameKey          string
	descriptionEn           string
	parameterDescriptionsEn map[string]string
	executor                Executor
	floor                   domain.ToolFloor
	editableFields          []string
}

// Registry holds tool definitions and their executors.
// See docs/orchestrator/toolregistry.md.
type Registry struct {
	tools map[string]entry
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]entry)}
}

// ToolSpec is the declarative description of a tool at registration time.
// See docs/orchestrator/toolregistry.md for Floor / EditableFields policy.
type ToolSpec struct {
	Def                     llm.ToolDefinition
	DisplayName             string
	DisplayNameKey          string
	UserDescription         string
	DescriptionEn           string
	ParameterDescriptionsEn map[string]string
	Floor                   domain.ToolFloor
	EditableFields          []string
}

// Register stores spec under spec.Def.Function.Name and binds exec as the
// executor. See docs/orchestrator/toolregistry.md.
func (r *Registry) Register(spec ToolSpec, exec Executor) {
	var paramsEn map[string]string
	if len(spec.ParameterDescriptionsEn) > 0 {
		paramsEn = make(map[string]string, len(spec.ParameterDescriptionsEn))
		for k, v := range spec.ParameterDescriptionsEn {
			paramsEn[k] = v
		}
	}
	r.tools[spec.Def.Function.Name] = entry{
		def:                     spec.Def,
		displayName:             spec.DisplayName,
		userDescription:         spec.UserDescription,
		displayNameKey:          spec.DisplayNameKey,
		descriptionEn:           spec.DescriptionEn,
		parameterDescriptionsEn: paramsEn,
		executor:                exec,
		floor:                   spec.Floor,
		editableFields:          append([]string(nil), spec.EditableFields...),
	}
}

// DisplayName returns the human-readable label registered for the named tool,
// or "" when unknown.
func (r *Registry) DisplayName(name string) string {
	e, ok := r.tools[name]
	if !ok {
		return ""
	}
	return e.displayName
}

// DisplayNameKey returns the i18n catalog key for the given tool, or "" if
// unknown / unset.
func (r *Registry) DisplayNameKey(name string) string {
	e, ok := r.tools[name]
	if !ok {
		return ""
	}
	return e.displayNameKey
}

// Available returns tool definitions available for the given active integrations.
// Returns RU descriptions; use AvailableForWhitelist for the locale-aware variant.
// See docs/orchestrator/toolregistry.md.
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

// localizeDef returns e.def with Description/parameter descriptions swapped to
// the per-locale text. Non-English locales return e.def by value (byte-identical).
func (r *Registry) localizeDef(e entry, tag language.Tag) llm.ToolDefinition {
	if tag != language.English {
		return e.def
	}
	if e.descriptionEn == "" && len(e.parameterDescriptionsEn) == 0 {
		return e.def
	}

	out := e.def
	out.Function = e.def.Function
	if e.descriptionEn != "" {
		out.Function.Description = e.descriptionEn
	}
	if len(e.parameterDescriptionsEn) > 0 {
		out.Function.Parameters = localizeParameters(e.def.Function.Parameters, e.parameterDescriptionsEn)
	}
	return out
}

// localizeParameters returns a deep-copied parameters schema with
// properties.<name>.description swapped to the EN values. Source map is never mutated.
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

	outParams := make(map[string]interface{}, len(params))
	for k, v := range params {
		outParams[k] = v
	}
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

// AvailableForWhitelist applies a typed WhitelistMode filter on top of
// Available. See docs/orchestrator/toolregistry.md for whitelist semantics
// (auto-floor exemption, unknown-tool safe-default, mode fallthroughs).
//
// ctx must be the request-scoped context from orchestrator.Run — used for
// slog attribution and cancellation hygiene. Never fabricate a root ctx.
func (r *Registry) AvailableForWhitelist(
	ctx context.Context,
	activeIntegrations []string,
	mode domain.WhitelistMode,
	allowed []string,
) []llm.ToolDefinition {
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

// availableLocalized is the locale-aware twin of Available. Internal: callers
// should use AvailableForWhitelist.
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

// ExecuteWithApproval runs the registered executor with approvalID propagated
// when the executor implements ApprovalExecutor; otherwise falls back to
// Execute. approvalID is always "<batch_id>-<call_id>".
// See docs/orchestrator/toolregistry.md.
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
	return e.executor.Execute(ctx, args)
}

// Floor returns the registered ToolFloor for toolName or ToolFloorForbidden
// when unknown (safe default — matches the startup validation sweep).
func (r *Registry) Floor(toolName string) domain.ToolFloor {
	if e, ok := r.tools[toolName]; ok {
		return e.floor
	}
	return domain.ToolFloorForbidden
}

// EditableFields returns the registered edit allowlist for toolName as a
// defensive copy, or nil when the tool is unknown.
func (r *Registry) EditableFields(toolName string) []string {
	if e, ok := r.tools[toolName]; ok {
		return append([]string(nil), e.editableFields...)
	}
	return nil
}

// Has reports whether toolName is currently registered.
func (r *Registry) Has(toolName string) bool {
	_, ok := r.tools[toolName]
	return ok
}

// AllFloors returns a snapshot of every registered tool's floor.
func (r *Registry) AllFloors() map[string]domain.ToolFloor {
	out := make(map[string]domain.ToolFloor, len(r.tools))
	for name, e := range r.tools {
		out[name] = e.floor
	}
	return out
}

// RegistryEntry is the projection exposed by GET /api/v1/tools — aliased to
// domain.ToolEntry so the API and orchestrator share one canonical type.
// See pkg/domain/tool_entry.go.
type RegistryEntry = domain.ToolEntry

// AllEntries returns a per-tool projection (RU descriptions).
// Callers needing locale-aware descriptions should use AllEntriesForLocale.
func (r *Registry) AllEntries() []RegistryEntry {
	return r.AllEntriesForLocale(i18n.DefaultTag)
}

// AllEntriesForLocale returns AllEntries with each tool's Description resolved
// to tag. Tools without descriptionEn fall back to def.Function.Description.
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

// toolPlatform extracts the prefix of a "{platform}__{action}" tool name, or
// "" for bare internal tools and names starting with "__".
func toolPlatform(toolName string) string {
	idx := strings.Index(toolName, "__")
	if idx <= 0 {
		return ""
	}
	return toolName[:idx]
}
