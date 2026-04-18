package tools

import (
	"context"
	"fmt"
	"strings"

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
	def         llm.ToolDefinition
	displayName string
	executor    Executor
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
// displayName is the human-readable label surfaced on the Tasks page.
func (r *Registry) Register(def llm.ToolDefinition, displayName string, exec Executor) {
	r.tools[def.Function.Name] = entry{def: def, displayName: displayName, executor: exec}
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
