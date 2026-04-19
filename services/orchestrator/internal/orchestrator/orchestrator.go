package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
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

	// EventToolApprovalRequired is emitted once per paused LLM turn, carrying
	// the batch_id + summarized calls that need human approval (HITL-02).
	// Emitted AFTER the PendingToolCallBatch is persisted (InsertPreparing →
	// PromoteToPending succeed); never emitted on a partial-persist crash.
	// The goroutine exits immediately after (HITL-03).
	EventToolApprovalRequired EventType = "tool_approval_required"

	// EventToolRejected is emitted for each tool call that the policy
	// resolver marks ToolFloorForbidden at pause time (synthetic
	// rejection, HITL-09) OR that the resolve-time TOCTOU re-check marks
	// policy_revoked (HITL-06). Content carries the reason.
	EventToolRejected EventType = "tool_rejected"
)

// Event is emitted on the output channel during agent execution.
//
// The Type/Content/ToolName/ToolArgs/ToolResult/ToolError fields are the
// legacy shape (pre-Phase-16). BatchID + Calls are Phase 16 additions for
// HITL — both are omitempty in JSON so legacy events remain byte-identical
// on the wire. ToolCallID is also added for Phase 16 so chat_proxy can
// persist tool_call events with the LLM's real call ID on the assistant
// Message.ToolCalls (HITL-13).
type Event struct {
	Type            EventType
	Content         string
	ToolCallID      string
	ToolName        string
	ToolDisplayName string
	ToolArgs        map[string]interface{}
	ToolResult      interface{}
	ToolError       string
	// BatchID is set on EventToolApprovalRequired events. Carries the
	// PendingToolCallBatch._id so the frontend can POST to the resolve
	// endpoint with the same identifier at approval time.
	BatchID string
	// Calls is set on EventToolApprovalRequired events with one entry per
	// manual-floor tool call bundled into the batch.
	Calls []ApprovalCallSummary
}

// ApprovalCallSummary is the per-call projection the frontend receives on a
// tool_approval_required event. EditableFields drives the UI's per-field
// read-only enforcement (HITL-L4 / HITL-07); Floor is always ToolFloorManual
// for batched calls (forbidden calls never appear in a batch — they get
// synthetic rejections instead).
type ApprovalCallSummary struct {
	CallID         string                 `json:"call_id"`
	ToolName       string                 `json:"tool_name"`
	Args           map[string]interface{} `json:"args"`
	EditableFields []string               `json:"editable_fields"`
	Floor          domain.ToolFloor       `json:"floor"`
}

// LLMClient abstracts the Router for testability.
type LLMClient interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// RunRequest holds everything needed to start an agent run.
//
// Phase 16 additions (all optional to preserve backward-compat with the
// existing test suite that predates HITL):
//   - ConversationID / BusinessID / ProjectID / UserID / MessageID: identity
//     fields persisted on the PendingToolCallBatch at pause time so Plan
//     16-07's resolve handler can enforce business-scoped access control
//     and run the TOCTOU re-check against projects.approval_overrides.
//   - BusinessApprovals / ProjectApprovalOverrides: the POLICY-02/03 maps
//     that hitl.Resolve consults to classify each LLM-proposed tool call
//     at pause time. chat_proxy.go (Plan 16-06) forwards these from the
//     business/project documents it loads to enrich the chat request.
type RunRequest struct {
	UserID          uuid.UUID
	Model           string
	BusinessContext prompt.BusinessContext
	// ProjectContext is the optional project prompt layer (Phase 15).
	// nil means "Без проекта" — legacy pre-Phase-15 behavior.
	ProjectContext *prompt.ProjectContext
	// WhitelistMode is the project's typed tool-whitelist mode (Phase 15).
	// "" = inherit (v1.3 baseline per 15-CONTEXT.md D-18).
	WhitelistMode domain.WhitelistMode
	// AllowedTools is consulted only when WhitelistMode == explicit.
	AllowedTools       []string
	Messages           []llm.Message // conversation history (excluding system)
	ActiveIntegrations []string
	Tier               string

	// Phase 16 HITL identity fields — threaded into RunState → batch.* at
	// pause time. Empty strings are tolerated: the repo stores them
	// verbatim and the resolve handler will 403/404 on missing context
	// when it needs business-scoped auth (Plan 16-07).
	ConversationID   string
	BusinessID       string
	ProjectID        string
	UserIDString     string
	MessageID        string

	// Phase 16 HITL policy inputs — consulted by hitl.Resolve at pause
	// time to classify each LLM-proposed tool call. nil maps are tolerated
	// (treated as empty maps — the registry floor wins).
	BusinessApprovals        map[string]domain.ToolFloor
	ProjectApprovalOverrides map[string]domain.ToolFloor
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
	llm         LLMClient
	tools       *tools.Registry
	options     Options
	pendingRepo domain.PendingToolCallRepository
}

// New creates an Orchestrator with default options (MaxIterations=10) and a
// nil pendingRepo. Callers that need HITL must use NewWithOptions or
// NewWithHITL; a nil pendingRepo causes stepRun to emit EventError
// "HITL not configured" when a manual-floor tool is classified (fail-loud
// at-use pattern — callers that don't use HITL never see the error).
func New(llmClient LLMClient, toolRegistry *tools.Registry) *Orchestrator {
	return NewWithOptions(llmClient, toolRegistry, Options{MaxIterations: 10})
}

// NewWithOptions creates an Orchestrator with custom options. pendingRepo is
// nil by default; use NewWithHITL to inject one. Backward-compatible with
// every pre-Phase-16 caller that used NewWithOptions(llm, reg, opts).
func NewWithOptions(llmClient LLMClient, toolRegistry *tools.Registry, opts Options) *Orchestrator {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 10
	}
	return &Orchestrator{llm: llmClient, tools: toolRegistry, options: opts}
}

// NewWithHITL constructs an Orchestrator with HITL wired in — pendingRepo
// receives the InsertPreparing + PromoteToPending + MarkDispatched +
// MarkResolved calls from stepRun / Resume. Use this constructor in
// cmd/main.go once Plan 16-02's repository is threaded through.
func NewWithHITL(
	llmClient LLMClient,
	toolRegistry *tools.Registry,
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

// Run starts a fresh agent turn and returns a channel of events. The
// channel is closed when stepRun returns (done / paused / error).
//
// Run is a thin wrapper over stepRun: it builds a fresh RunState from
// RunRequest and spawns the goroutine. Resume (in resume.go) is the
// companion wrapper that rebuilds RunState from a persisted batch
// snapshot; both call into stepRun. This is the Run→Resume→stepRun
// shape mandated by HITL-03 — no blocked goroutines on approval channels.
func (o *Orchestrator) Run(ctx context.Context, req RunRequest) (<-chan Event, error) {
	ch := make(chan Event, 32)

	state := &RunState{
		Messages:                 prompt.Build(req.BusinessContext, req.ProjectContext, req.Messages),
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
