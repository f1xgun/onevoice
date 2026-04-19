package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// Executor handles the execution of a tool call.
type Executor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// ApprovalExecutor extends Executor with a variant that accepts an approvalID
// propagated into the dispatch payload (Plan 16-04's a2a.ToolRequest.ApprovalID
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
	def            llm.ToolDefinition
	displayName    string
	executor       Executor
	floor          domain.ToolFloor
	editableFields []string
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
// baseline (POLICY-01), and the per-tool EditableFields allowlist for HITL-07
// edit-args validation (HITL-L4 promoted into v1.3 per D-10/D-11).
//
// The caller MUST pass all five arguments explicitly — every registration site
// in services/orchestrator/cmd/main.go must deliberately choose a floor and an
// edit allowlist. There is no default so that a newly-added tool can never
// silently inherit an unsafe policy. EditableFields is copied defensively so
// subsequent caller-side mutations cannot change registered behaviour.
//
// Convention (Pitfall 8): EditableFields is always lowercase_with_underscore
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

// Available returns tool definitions available for the given active integrations.
// Tools named "{platform}__{action}" are included only if platform is active.
// Tools without "__" are always included (internal tools).
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

// AvailableForWhitelist applies Phase 15's typed WhitelistMode filter on top
// of Available. Unknown tool names in `allowed` are logged (slog WARN) and
// silently dropped — the safe-default behavior documented in
// .planning/research/PITFALLS.md §9 (whitelist drift: a renamed or missing
// tool is treated as denied rather than surfaced as an error).
//
// ctx is the request-scoped context threaded from orchestrator.Run. It is
// used for slog attribution (correlation_id, business_id) and cancellation
// hygiene. Callers MUST NOT fabricate a root context here — there is always
// a request-scoped ctx available at the call site in Run.
//
// v1.3 scope note (D-18 in 15-CONTEXT.md): inherit == all. Phase 16
// (POLICY-05) replaces this with a business-level tool_approvals map that
// will serve as the actual "inherited" baseline; until then, the baseline is
// "every registered tool for the active integrations".
func (r *Registry) AvailableForWhitelist(
	ctx context.Context,
	activeIntegrations []string,
	mode domain.WhitelistMode,
	allowed []string,
) []llm.ToolDefinition {
	base := r.Available(activeIntegrations)
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
			if allowSet[def.Function.Name] {
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
// Called by Plan 16-05's Resume path after parsing a resolved batch:
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
// if the tool is unknown (safe default per POLICY-07 — the runtime policy
// resolver treats unknown tools as "not permitted", matching the startup
// validation sweep that logs tool_approval_whitelist_unknown for entries
// referencing missing tools).
func (r *Registry) Floor(toolName string) domain.ToolFloor {
	if e, ok := r.tools[toolName]; ok {
		return e.floor
	}
	return domain.ToolFloorForbidden
}

// EditableFields returns the registered edit allowlist for toolName, or nil
// if the tool is unknown. The returned slice is a defensive copy — mutating
// it does not alter registry state. The list is always lowercase_with_underscore
// matching the tool's JSON args schema (Pitfall 8).
func (r *Registry) EditableFields(toolName string) []string {
	if e, ok := r.tools[toolName]; ok {
		return append([]string(nil), e.editableFields...)
	}
	return nil
}

// Has reports whether toolName is currently registered. Used by the POLICY-07
// startup validation sweep to detect whitelist entries that reference a tool
// which has been renamed or removed between deploys.
func (r *Registry) Has(toolName string) bool {
	_, ok := r.tools[toolName]
	return ok
}

// AllFloors returns a snapshot of every registered tool's floor. Used by
// POLICY-07 startup validation and by GET /api/v1/tools (Plan 16-07) to
// populate the settings UI's per-tool toggles.
func (r *Registry) AllFloors() map[string]domain.ToolFloor {
	out := make(map[string]domain.ToolFloor, len(r.tools))
	for name, e := range r.tools {
		out[name] = e.floor
	}
	return out
}

// RegistryEntry is the projection exposed by GET /api/v1/tools (Plan 16-07).
// Kept in the tools package so the API handler can import a typed shape.
type RegistryEntry struct {
	Name           string           `json:"name"`
	Platform       string           `json:"platform"` // e.g., "telegram" — derived from {platform}__{action}
	Floor          domain.ToolFloor `json:"floor"`
	EditableFields []string         `json:"editableFields"`
	Description    string           `json:"description"`
}

// AllEntries returns a snapshot of (name, platform, floor, editable, description)
// for every registered tool. Feeds GET /api/v1/tools in Plan 16-07 as well as
// the cluster-internal /internal/tools/names endpoint (Plan 16-03 Task 2) used
// by the POLICY-07 startup validation sweep.
func (r *Registry) AllEntries() []RegistryEntry {
	out := make([]RegistryEntry, 0, len(r.tools))
	for _, e := range r.tools {
		platform := toolPlatform(e.def.Function.Name)
		out = append(out, RegistryEntry{
			Name:           e.def.Function.Name,
			Platform:       platform,
			Floor:          e.floor,
			EditableFields: append([]string(nil), e.editableFields...),
			Description:    e.def.Function.Description,
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
