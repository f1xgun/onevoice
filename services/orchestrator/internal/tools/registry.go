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

// ExecutorFunc is a function that implements Executor.
type ExecutorFunc func(ctx context.Context, args map[string]interface{}) (interface{}, error)

func (f ExecutorFunc) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return f(ctx, args)
}

type entry struct {
	def      llm.ToolDefinition
	executor Executor
}

// Registry holds tool definitions and their executors.
type Registry struct {
	tools map[string]entry
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]entry)}
}

// Register adds a tool definition with its executor (may be nil for stub tools).
func (r *Registry) Register(def llm.ToolDefinition, exec Executor) {
	r.tools[def.Function.Name] = entry{def: def, executor: exec}
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
