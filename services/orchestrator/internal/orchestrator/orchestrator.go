// Package orchestrator implements the LLM agent loop and HITL pause/resume
// flow. See docs/orchestrator/run.md and docs/orchestrator/resume.md.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// EventType identifies the kind of event emitted by the agent loop.
type EventType string

// Event types emitted on the agent-loop output channel.
// See docs/orchestrator/run.md.
const (
	EventText       EventType = "text"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventError      EventType = "error"
	EventDone       EventType = "done"

	// EventToolApprovalRequired is emitted once per paused LLM turn after
	// pendingRepo.Persist commits (never on partial-persist crash).
	EventToolApprovalRequired EventType = "tool_approval_required"

	// EventToolRejected is emitted per-call for synthetic rejections
	// (Forbidden at pause time, or policy_revoked at resume).
	EventToolRejected EventType = "tool_rejected"
)

// Event is emitted on the output channel during agent execution.
// See docs/orchestrator/run.md for field-by-field semantics.
type Event struct {
	Type EventType
	// Code is a machine-readable discriminator on error events. Projected
	// onto sse.Event.Code on the wire (omitempty for non-error events).
	Code               string
	Content            string
	ToolCallID         string
	ToolName           string
	ToolDisplayName    string
	ToolDisplayNameKey string
	ToolArgs           map[string]interface{}
	ToolResult         interface{}
	ToolError          string
	BatchID            string
	Calls              []sse.ApprovalCall
}

// LLMClient abstracts the Router for testability.
type LLMClient interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// RunRequest holds everything needed to start an agent run.
// See docs/orchestrator/run.md for HITL field semantics (all optional).
type RunRequest struct {
	UserID          uuid.UUID
	Model           string
	BusinessContext prompt.BusinessContext
	// ProjectContext is the optional project prompt layer. nil = no project scoping.
	ProjectContext *prompt.ProjectContext
	// WhitelistMode is the project's typed tool-whitelist mode. "" = inherit.
	WhitelistMode      domain.WhitelistMode
	AllowedTools       []string
	Messages           []llm.Message // conversation history (excluding system)
	ActiveIntegrations []string
	Tier               string

	// HITL identity fields — threaded into RunState → batch.* at pause time.
	ConversationID string
	BusinessID     string
	ProjectID      string
	UserIDString   string
	MessageID      string

	// HITL policy inputs — consulted by hitl.Resolve at pause time.
	BusinessApprovals        map[string]domain.ToolFloor
	ProjectApprovalOverrides map[string]domain.ToolFloor
}

// Options configures the Orchestrator.
type Options struct {
	MaxIterations int
	// ToolExecTimeout bounds one tool call. Zero = parent context governs.
	ToolExecTimeout time.Duration
	// ConversationInputCap stops the agent loop when accumulated input tokens
	// (LLM Usage.InputTokens summed across iterations, including tool-result
	// bytes the next iter sends back) reach this many. Zero disables.
	ConversationInputCap int
	// ConversationOutputCap is the parallel knob for accumulated output tokens.
	ConversationOutputCap int
}

// Orchestrator runs the LLM agent loop. See docs/orchestrator/run.md.
type Orchestrator struct {
	llm         LLMClient
	tools       *toolregistry.Registry
	options     Options
	pendingRepo domain.PendingToolCallRepository
}

// New creates an Orchestrator with MaxIterations=10 and no HITL.
// Manual-floor tools surfaced without pendingRepo cause stepRun to emit
// EventError "HITL not configured" (fail-loud at-use).
func New(llmClient LLMClient, toolRegistry *toolregistry.Registry) *Orchestrator {
	return NewWithOptions(llmClient, toolRegistry, Options{MaxIterations: 10})
}

// NewWithOptions creates an Orchestrator with custom options; pendingRepo nil.
// Use NewWithHITL to inject one.
func NewWithOptions(llmClient LLMClient, toolRegistry *toolregistry.Registry, opts Options) *Orchestrator {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 10
	}
	return &Orchestrator{llm: llmClient, tools: toolRegistry, options: opts}
}

// NewWithHITL constructs an Orchestrator with HITL wired in. Use this in
// cmd/main.go. See docs/orchestrator/run.md.
func NewWithHITL(
	llmClient LLMClient,
	toolRegistry *toolregistry.Registry,
	pendingRepo domain.PendingToolCallRepository,
	opts Options,
) *Orchestrator {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 10
	}
	return &Orchestrator{
		llm:         llmClient,
		tools:       toolRegistry,
		options:     opts,
		pendingRepo: pendingRepo,
	}
}

// Run starts a fresh agent turn and returns a channel of events. The channel
// is closed when stepRun returns (done / paused / error).
// See docs/orchestrator/run.md.
func (o *Orchestrator) Run(ctx context.Context, req RunRequest) (<-chan Event, error) {
	ch := make(chan Event, 32)

	// BuildSplit's Block 1 (platform) carries cache_control; Block 2 (business)
	// does not. SystemBlocks (set in stepRun) is the canonical channel —
	// Messages no longer carries a leading role:"system" entry.
	platform, business, history := prompt.BuildSplit(req.BusinessContext, req.ProjectContext, req.Messages)

	state := &RunState{
		Messages:                 history,
		SystemPlatform:           platform,
		SystemBusiness:           business,
		AvailableTools:           o.tools.AvailableForWhitelist(ctx, req.ActiveIntegrations, req.WhitelistMode, req.AllowedTools),
		BusinessApprovals:        req.BusinessApprovals,
		ProjectApprovalOverrides: req.ProjectApprovalOverrides,
		ConversationID:           req.ConversationID,
		BusinessID:               req.BusinessID,
		ProjectID:                req.ProjectID,
		UserID:                   req.UserIDString,
		MessageID:                req.MessageID,
		Model:                    req.Model,
		Tier:                     req.Tier,
		UserUUID:                 req.UserID,
		Iter:                     0,
	}

	go func() {
		defer close(ch)
		_, _, _ = o.stepRun(ctx, state, ch)
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

// dispatchToolCalls executes a batch of tool calls concurrently and emits
// tool_result events in completion order. The tool messages appended to
// `messages` MUST line up with the original tool_calls slice — OpenAI and
// Anthropic require role:tool messages to match assistant.tool_calls[*].id
// for the next iteration. Returns false on ctx cancellation.
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
			Type:               EventToolCall,
			ToolCallID:         tc.ID,
			ToolName:           tc.Function.Name,
			ToolDisplayName:    o.tools.DisplayName(tc.Function.Name),
			ToolDisplayNameKey: o.tools.DisplayNameKey(tc.Function.Name),
			ToolArgs:           args,
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

			ev := buildToolResultEvent(outcomes[i].tc, o.tools.DisplayName(name), o.tools.DisplayNameKey(name), result, execErr)
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

	// Append in original tool_calls order — provider-contract invariant.
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

// buildToolResultEvent shapes a tool outcome into an SSE event. Out-of-line so
// the goroutine body stays short and side-effect free.
func buildToolResultEvent(tc llm.ToolCall, displayName, displayNameKey string, result interface{}, execErr error) Event {
	payload := result
	if execErr != nil {
		payload = map[string]interface{}{"error": execErr.Error(), "tool_name": tc.Function.Name}
	}
	ev := Event{
		Type:               EventToolResult,
		ToolCallID:         tc.ID,
		ToolName:           tc.Function.Name,
		ToolDisplayName:    displayName,
		ToolDisplayNameKey: displayNameKey,
		ToolResult:         payload,
	}
	if execErr != nil {
		ev.ToolError = execErr.Error()
		var ce *a2a.CodedError
		if errors.As(execErr, &ce) {
			ev.Code = ce.Code
		}
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
