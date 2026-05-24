package agentbase

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// ToolExec is the per-tool executor signature. Each agent's per-tool method
// (sendChannelPost, publishPost, getReviews, ...) is a value of this type when
// bound to its receiver via method-value expression (h.sendChannelPost).
//
// Routes carry these closures from the agent Handler into NewRouter, which
// assembles them into the dispatch chain.
type ToolExec = func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error)

// NewRouter wires a per-agent routes map into the standard tool-dispatch chain
// shared across all four platform agents (telegram, vk, yandex-business,
// google-business). Before this helper existed, each agent reimplemented two
// pieces of boilerplate:
//
//   - The dispatcher-or-not branch in Handle: when dispatcher == nil (unit
//     tests / dev-local without Redis), call routeTool directly and apply the
//     classifier; otherwise forward through dispatcher.Dispatch.
//   - The switch on req.Tool with an "unknown tool: %s" default branch.
//
// NewRouter consolidates both. The returned ToolExec is the agent's a2a.Exec
// entry point — pass it to a2a.NewAgent (or via the agent Handler's exposed
// Handle method as a thin shim, which keeps the existing test surface intact).
//
// dispatcher may be nil. In that case the returned ToolExec applies the
// fallback classifier directly. fallbackClassifier may also be nil, in which
// case errors propagate as-is.
//
// Note: when dispatcher is non-nil, NewRouter does NOT apply
// fallbackClassifier — the Dispatcher owns its own classifier (passed to
// NewDispatcher) and applies it once per Dispatch. Passing the same classifier
// to both NewDispatcher and NewRouter is intentional and harmless: the
// dispatcher path uses its own copy, the nil-dispatcher path uses
// NewRouter's. This keeps each path single-classify and lets agents wire a
// nil dispatcher in tests without rewiring the classifier.
func NewRouter(routes map[string]ToolExec, dispatcher Dispatcher, fallbackClassifier ErrorClassifier) ToolExec {
	routeTool := func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		if exec, ok := routes[req.Tool]; ok {
			return exec(ctx, req)
		}
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}

	if dispatcher == nil {
		return func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
			resp, err := routeTool(ctx, req)
			if fallbackClassifier != nil {
				err = fallbackClassifier.Classify(err)
			}
			return resp, err
		}
	}

	return func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return dispatcher.Dispatch(ctx, req, routeTool)
	}
}
