package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// EventType identifies the kind of event emitted by the agent loop.
type EventType string

const (
	EventText       EventType = "text"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventError      EventType = "error"
	EventDone       EventType = "done"
)

// Event is emitted on the output channel during agent execution.
type Event struct {
	Type            EventType
	Content         string
	ToolCallID      string
	ToolName        string
	ToolDisplayName string
	ToolArgs        map[string]interface{}
	ToolResult      interface{}
	ToolError       string
}

// LLMClient abstracts the Router for testability.
type LLMClient interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// RunRequest holds everything needed to start an agent run.
type RunRequest struct {
	UserID             uuid.UUID
	Model              string
	BusinessContext    prompt.BusinessContext
	Messages           []llm.Message // conversation history (excluding system)
	ActiveIntegrations []string
	Tier               string
}

// Options configures the Orchestrator.
type Options struct {
	MaxIterations int
	// ToolExecTimeout bounds how long a single tool call may block.
	// Zero means no per-tool timeout — the parent context governs.
	ToolExecTimeout time.Duration
}

// Orchestrator runs the LLM agent loop.
type Orchestrator struct {
	llm     LLMClient
	tools   *tools.Registry
	options Options
}

// New creates an Orchestrator with default options (MaxIterations=10).
func New(llmClient LLMClient, toolRegistry *tools.Registry) *Orchestrator {
	return NewWithOptions(llmClient, toolRegistry, Options{MaxIterations: 10})
}

// NewWithOptions creates an Orchestrator with custom options.
func NewWithOptions(llmClient LLMClient, toolRegistry *tools.Registry, opts Options) *Orchestrator {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 10
	}
	return &Orchestrator{llm: llmClient, tools: toolRegistry, options: opts}
}

// Run starts the agent loop and returns a channel of events.
// The channel is closed when the loop finishes or the context is canceled.
func (o *Orchestrator) Run(ctx context.Context, req RunRequest) (<-chan Event, error) {
	ch := make(chan Event, 32)

	go func() {
		defer close(ch)

		availableTools := o.tools.Available(req.ActiveIntegrations)
		messages := prompt.Build(req.BusinessContext, req.Messages)

		for iter := 0; iter < o.options.MaxIterations; iter++ {
			llmReq := llm.ChatRequest{
				UserID:   req.UserID,
				Model:    req.Model,
				Messages: messages,
				Tools:    availableTools,
				Tier:     req.Tier,
			}

			resp, err := o.llm.Chat(ctx, llmReq)
			if err != nil {
				select {
				case ch <- Event{Type: EventError, Content: err.Error()}:
				case <-ctx.Done():
				}
				return
			}

			// No tool calls or stop reason — emit text and finish
			if len(resp.ToolCalls) == 0 || resp.FinishReason == "stop" {
				if resp.Content != "" {
					select {
					case ch <- Event{Type: EventText, Content: resp.Content}:
					case <-ctx.Done():
						return
					}
				}
				select {
				case ch <- Event{Type: EventDone}:
				case <-ctx.Done():
				}
				return
			}

			// Append assistant message with tool calls
			messages = append(messages, llm.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			if !o.dispatchToolCalls(ctx, ch, resp.ToolCalls, &messages) {
				return
			}
		}

		// Exhausted max iterations
		select {
		case ch <- Event{Type: EventError, Content: fmt.Sprintf("max iterations (%d) reached", o.options.MaxIterations)}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// toolOutcome captures the result of a single tool invocation in a parallel batch.
type toolOutcome struct {
	tc      llm.ToolCall
	args    map[string]interface{}
	result  interface{}
	execErr error
}

// dispatchToolCalls executes a batch of tool calls from a single LLM response
// concurrently. Each goroutine emits its tool_result event as soon as that
// tool finishes, so the UI reflects real per-tool latency rather than the
// batch's slowest member. The tool messages appended to `messages` are
// ordered to match the original tool_calls slice — OpenAI and Anthropic
// require role:tool messages to line up with assistant.tool_calls[*].id for
// the next iteration.
//
// Returns false if the context was canceled before all events could be emitted.
func (o *Orchestrator) dispatchToolCalls(
	ctx context.Context,
	ch chan<- Event,
	toolCalls []llm.ToolCall,
	messages *[]llm.Message,
) bool {
	outcomes := make([]toolOutcome, len(toolCalls))
	for i, tc := range toolCalls {
		args := parseToolArgs(tc.Function.Arguments)
		outcomes[i] = toolOutcome{tc: tc, args: args}

		select {
		case ch <- Event{
			Type:            EventToolCall,
			ToolCallID:      tc.ID,
			ToolName:        tc.Function.Name,
			ToolDisplayName: o.tools.DisplayName(tc.Function.Name),
			ToolArgs:        args,
		}:
		case <-ctx.Done():
			return false
		}
	}

	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := outcomes[i].tc.Function.Name
			result, execErr := o.executeOne(ctx, name, outcomes[i].args)
			outcomes[i].result = result
			outcomes[i].execErr = execErr

			ev := buildToolResultEvent(outcomes[i].tc, o.tools.DisplayName(name), result, execErr)
			select {
			case ch <- ev:
			case <-ctx.Done():
			}
		}(i)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return false
	}

	for _, out := range outcomes {
		result := out.result
		if out.execErr != nil {
			result = map[string]interface{}{"error": out.execErr.Error(), "tool_name": out.tc.Function.Name}
		}
		resultJSON, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			resultJSON = []byte(fmt.Sprintf(`{"error":"marshal failed: %s","tool_name":%q}`, marshalErr.Error(), out.tc.Function.Name))
		}
		*messages = append(*messages, llm.Message{
			Role:       "tool",
			Content:    string(resultJSON),
			ToolCallID: out.tc.ID,
		})
	}
	return true
}

// buildToolResultEvent wraps a tool outcome into the event emitted on the SSE
// channel. Shaping it here keeps the goroutine body short and side-effect free.
func buildToolResultEvent(tc llm.ToolCall, displayName string, result interface{}, execErr error) Event {
	payload := result
	if execErr != nil {
		payload = map[string]interface{}{"error": execErr.Error(), "tool_name": tc.Function.Name}
	}
	ev := Event{
		Type:            EventToolResult,
		ToolCallID:      tc.ID,
		ToolName:        tc.Function.Name,
		ToolDisplayName: displayName,
		ToolResult:      payload,
	}
	if execErr != nil {
		ev.ToolError = execErr.Error()
	}
	return ev
}

// executeOne runs a single tool, optionally bounded by ToolExecTimeout, and
// records metrics. Safe for concurrent calls.
func (o *Orchestrator) executeOne(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	callCtx := ctx
	var cancel context.CancelFunc
	if o.options.ToolExecTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, o.options.ToolExecTimeout)
		defer cancel()
	}

	start := time.Now()
	result, err := o.tools.Execute(callCtx, name, args)

	status := "success"
	if err != nil {
		status = "error"
	}
	agent := name
	if sep := strings.Index(name, "__"); sep != -1 {
		agent = name[:sep]
	}
	metrics.RecordToolDispatch(name, agent, status, time.Since(start))

	return result, err
}

// parseToolArgs unmarshals JSON tool arguments. On failure it falls back to a
// single "raw" field so the tool executor still receives the original payload.
func parseToolArgs(raw string) map[string]interface{} {
	if raw == "" {
		return map[string]interface{}{}
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return map[string]interface{}{"raw": raw}
	}
	return args
}
