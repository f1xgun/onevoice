package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

			// Execute each tool call
			for _, tc := range resp.ToolCalls {
				var args map[string]interface{}
				if tc.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
						args = map[string]interface{}{"raw": tc.Function.Arguments}
					}
				}

				displayName := o.tools.DisplayName(tc.Function.Name)

				// Emit tool call event
				select {
				case ch <- Event{Type: EventToolCall, ToolCallID: tc.ID, ToolName: tc.Function.Name, ToolDisplayName: displayName, ToolArgs: args}:
				case <-ctx.Done():
					return
				}

				// Execute the tool
				toolStart := time.Now()
				result, execErr := o.tools.Execute(ctx, tc.Function.Name, args)
				if execErr != nil {
					result = map[string]interface{}{"error": execErr.Error(), "tool_name": tc.Function.Name}
				}

				// Record tool dispatch metrics
				toolStatus := "success"
				if execErr != nil {
					toolStatus = "error"
				}
				toolAgent := tc.Function.Name
				if sep := strings.Index(tc.Function.Name, "__"); sep != -1 {
					toolAgent = tc.Function.Name[:sep]
				}
				metrics.RecordToolDispatch(tc.Function.Name, toolAgent, toolStatus, time.Since(toolStart))

				// Emit tool result event for the frontend
				toolResultEv := Event{
					Type:            EventToolResult,
					ToolCallID:      tc.ID,
					ToolName:        tc.Function.Name,
					ToolDisplayName: displayName,
					ToolResult:      result,
				}
				if execErr != nil {
					toolResultEv.ToolError = execErr.Error()
				}
				select {
				case ch <- toolResultEv:
				case <-ctx.Done():
					return
				}

				// Serialize result; fallback to error JSON if marshal fails
				resultJSON, marshalErr := json.Marshal(result)
				if marshalErr != nil {
					resultJSON = []byte(fmt.Sprintf(`{"error":"marshal failed: %s","tool_name":%q}`, marshalErr.Error(), tc.Function.Name))
				}

				// Append tool result message
				messages = append(messages, llm.Message{
					Role:       "tool",
					Content:    string(resultJSON),
					ToolCallID: tc.ID,
				})
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
