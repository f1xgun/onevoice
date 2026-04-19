package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/hitl"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// StepOutcome identifies the terminal state of a stepRun invocation. Callers
// (Run for fresh turns, Resume for post-approval continuation) branch on this
// value to decide whether to exit the goroutine (OutcomePaused/OutcomeDone)
// or surface an error (OutcomeError/OutcomeMaxIterations).
type StepOutcome int

const (
	// OutcomeDone — LLM returned a terminal response with no tool calls.
	OutcomeDone StepOutcome = iota
	// OutcomePaused — at least one manual-floor tool call was classified;
	// the batch was persisted (InsertPreparing → PromoteToPending) and
	// the tool_approval_required SSE event emitted. Goroutine MUST exit.
	OutcomePaused
	// OutcomeError — unrecoverable error; an EventError has already been emitted.
	OutcomeError
	// OutcomeMaxIterations — safety cap hit; an EventError has been emitted.
	OutcomeMaxIterations
)

// RunState holds the mutable loop state across iterations. Serialized to
// PendingToolCallBatch.ModelMessages at pause time and reconstructed at
// resume time by Resume.
type RunState struct {
	// Messages is the full LLM conversation including the built system prompt.
	Messages []llm.Message

	// AvailableTools is the whitelist-filtered tool set for this turn.
	AvailableTools []llm.ToolDefinition

	// BusinessApprovals is the businesses.settings.tool_approvals snapshot
	// (POLICY-02). Nil maps are tolerated by hitl.Resolve.
	BusinessApprovals map[string]domain.ToolFloor

	// ProjectApprovalOverrides is the projects.approval_overrides snapshot
	// (POLICY-03). Nil maps are tolerated by hitl.Resolve.
	ProjectApprovalOverrides map[string]domain.ToolFloor

	// ConversationID / BusinessID / UserID / MessageID are the identity
	// fields persisted on PendingToolCallBatch so that the resolve
	// handler (Plan 16-07) can enforce business-scoped access control.
	ConversationID string
	BusinessID     string

	// ProjectID is nullable — empty when a conversation has no project
	// ("Без проекта"). Threaded into batch.ProjectID so Plan 16-07's
	// TOCTOU re-check can load the project's approval_overrides at
	// resolve time (POLICY-03 + HITL-06).
	ProjectID string
	UserID    string
	MessageID string

	// Model / Tier mirror the incoming ChatRequest fields so that
	// subsequent iterations (including post-resume) route to the same
	// provider with the same tier.
	Model string
	Tier  string

	// UserID / UUID (from RunRequest.UserID) is retained here as a
	// sibling of UserID (string) — LLMClient.Chat takes the uuid.UUID
	// form in ChatRequest. We keep both; legacy callers pass a uuid.
	UserUUID uuid.UUID

	// Iter is the 0-based iteration counter. Pause persists this value
	// so resume can continue at Iter+1.
	Iter int
}

// stepRun is the single shared loop body used by both Run (fresh turns) and
// Resume (post-approval continuation). It MUST NOT block waiting for
// approval — when a manual-floor tool is classified, it persists the
// batch, emits the tool_approval_required event, and returns
// OutcomePaused so the caller's goroutine exits cleanly (HITL-03).
//
// Signature is anti-footgun #3 — see 16-OVERVIEW.md. Any deviation blocks
// the phase: the wave-2 grep gate confirms the literal substring.
func (o *Orchestrator) stepRun(ctx context.Context, state *RunState, out chan<- Event) (StepOutcome, string, error) {
	for state.Iter < o.options.MaxIterations {
		// 1. Call the LLM
		llmReq := llm.ChatRequest{
			UserID:   state.UserUUID,
			Model:    state.Model,
			Messages: state.Messages,
			Tools:    state.AvailableTools,
			Tier:     state.Tier,
		}
		resp, err := o.llm.Chat(ctx, llmReq)
		if err != nil {
			select {
			case out <- Event{Type: EventError, Content: err.Error()}:
			case <-ctx.Done():
			}
			return OutcomeError, "", err
		}

		// 2. No tool calls → terminal (done)
		if len(resp.ToolCalls) == 0 || resp.FinishReason == "stop" {
			if resp.Content != "" {
				select {
				case out <- Event{Type: EventText, Content: resp.Content}:
				case <-ctx.Done():
					return OutcomeDone, "", nil
				}
			}
			select {
			case out <- Event{Type: EventDone}:
			case <-ctx.Done():
			}
			return OutcomeDone, "", nil
		}

		// 3. Append assistant message with tool calls (tool results follow per-call).
		state.Messages = append(state.Messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// 4. Classify every LLM-proposed tool call through hitl.Resolve,
		//    bucketing into auto / manual / forbidden. This is the single
		//    point where policy resolution happens at pause time
		//    (Plan 16-07 re-runs the same function at resolve time for
		//    TOCTOU safety — HITL-06).
		var autoCalls []llm.ToolCall
		var manualCalls []llm.ToolCall
		var forbiddenCalls []llm.ToolCall

		for _, tc := range resp.ToolCalls {
			floor := o.tools.Floor(tc.Function.Name)
			effective := hitl.Resolve(floor, state.BusinessApprovals, state.ProjectApprovalOverrides, tc.Function.Name)
			switch effective {
			case domain.ToolFloorAuto:
				autoCalls = append(autoCalls, tc)
			case domain.ToolFloorManual:
				manualCalls = append(manualCalls, tc)
			case domain.ToolFloorForbidden:
				forbiddenCalls = append(forbiddenCalls, tc)
			default:
				// Unknown tool → Registry.Floor returns Forbidden by default;
				// unknown Resolve result falls through here defensively.
				forbiddenCalls = append(forbiddenCalls, tc)
			}
		}

		// 5. Forbidden calls → synthesize rejection message, emit
		//    tool_rejected event, DO NOT dispatch. The LLM sees the
		//    outcome on the next iteration (HITL-09).
		for _, tc := range forbiddenCalls {
			rejectionMsg := `{"rejected":true,"reason":"policy_forbidden"}`
			state.Messages = append(state.Messages, llm.Message{
				Role:       "tool",
				Content:    rejectionMsg,
				ToolCallID: tc.ID,
			})
			select {
			case out <- Event{
				Type:       EventToolRejected,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Content:    "policy_forbidden",
			}:
			case <-ctx.Done():
				return OutcomeError, "", ctx.Err()
			}
		}

		// 6. Auto calls — dispatch inline, preserving existing behavior.
		if err := o.executeAutoCalls(ctx, state, autoCalls, out); err != nil {
			return OutcomeError, "", err
		}

		// 7. Manual calls — two-phase persist, emit pause event, return.
		//    Order invariant (Pitfall 1/3): persist succeeds BEFORE
		//    emitting the pause event — on crash the orphan-reconcile
		//    sweep (Plan 16-02) cleans stuck preparing rows.
		if len(manualCalls) > 0 {
			if o.pendingRepo == nil {
				err := fmt.Errorf("HITL not configured: manual-floor tool classified but pendingRepo is nil")
				select {
				case out <- Event{Type: EventError, Content: err.Error()}:
				case <-ctx.Done():
				}
				return OutcomeError, "", err
			}

			batchID := uuid.NewString()
			batch := buildPendingBatch(batchID, state, manualCalls)

			if err := o.pendingRepo.InsertPreparing(ctx, batch); err != nil {
				select {
				case out <- Event{Type: EventError, Content: fmt.Sprintf("failed to persist approval batch: %v", err)}:
				case <-ctx.Done():
				}
				return OutcomeError, "", err
			}
			if err := o.pendingRepo.PromoteToPending(ctx, batchID); err != nil {
				select {
				case out <- Event{Type: EventError, Content: fmt.Sprintf("failed to promote approval batch: %v", err)}:
				case <-ctx.Done():
				}
				return OutcomeError, "", err
			}

			// Single tool_approval_required event per turn covering every
			// manual call in this iteration (HITL-02: one card per batch).
			select {
			case out <- Event{
				Type:    EventToolApprovalRequired,
				BatchID: batchID,
				Calls:   summarizeManualCalls(o.tools, manualCalls),
			}:
			case <-ctx.Done():
				return OutcomePaused, batchID, ctx.Err()
			}
			return OutcomePaused, batchID, nil
		}

		// 8. Continue loop (only auto calls, or only forbidden + auto).
		state.Iter++
	}

	// Max iterations exhausted
	select {
	case out <- Event{Type: EventError, Content: fmt.Sprintf("max iterations (%d) reached", o.options.MaxIterations)}:
	case <-ctx.Done():
	}
	return OutcomeMaxIterations, "", nil
}

// executeAutoCalls dispatches auto-floor tool calls inline, preserving the
// exact legacy behavior of the pre-Phase-16 Run body: decode arguments, emit
// tool_call, call the registered Executor (NATSExecutor in production),
// record metrics, emit tool_result, append the serialized result as a
// tool-role message to state.Messages.
func (o *Orchestrator) executeAutoCalls(ctx context.Context, state *RunState, calls []llm.ToolCall, out chan<- Event) error {
	for _, tc := range calls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{"raw": tc.Function.Arguments}
			}
		}

		// Emit tool_call event
		select {
		case out <- Event{Type: EventToolCall, ToolCallID: tc.ID, ToolName: tc.Function.Name, ToolArgs: args}:
		case <-ctx.Done():
			return ctx.Err()
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

		// Emit tool_result event
		toolResultEv := Event{
			Type:       EventToolResult,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			ToolResult: result,
		}
		if execErr != nil {
			toolResultEv.ToolError = execErr.Error()
		}
		select {
		case out <- toolResultEv:
		case <-ctx.Done():
			return ctx.Err()
		}

		// Append tool result message
		resultJSON, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			resultJSON = []byte(fmt.Sprintf(`{"error":"marshal failed: %s","tool_name":%q}`, marshalErr.Error(), tc.Function.Name))
		}
		state.Messages = append(state.Messages, llm.Message{
			Role:       "tool",
			Content:    string(resultJSON),
			ToolCallID: tc.ID,
		})
	}
	return nil
}

// buildPendingBatch assembles the PendingToolCallBatch that will be persisted
// at pause time. ProjectID is threaded through from RunState so Plan 16-07's
// TOCTOU re-check can load the project's approval_overrides at resolve time
// (POLICY-03 + HITL-06). ModelMessages is the full state.Messages snapshot
// as JSON so Resume can rebuild RunState after a process restart.
func buildPendingBatch(batchID string, state *RunState, manualCalls []llm.ToolCall) *domain.PendingToolCallBatch {
	msgSnapshot, err := json.Marshal(state.Messages)
	if err != nil {
		// Snapshot marshal failure is silently tolerated here — Resume will
		// fail cleanly with EventError "corrupt snapshot" if this ever
		// happens. llm.Message is plain JSON so this is only theoretical.
		slog.Warn("stepRun: failed to marshal messages snapshot", "error", err, "batch_id", batchID)
		msgSnapshot = []byte("[]")
	}
	calls := make([]domain.PendingCall, 0, len(manualCalls))
	for _, tc := range manualCalls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{"raw": tc.Function.Arguments}
			}
		}
		calls = append(calls, domain.PendingCall{
			CallID:    tc.ID,
			ToolName:  tc.Function.Name,
			Arguments: args,
			// Verdict/EditedArgs/Dispatched left zero — populated by
			// Plan 16-07's resolve handler.
		})
	}
	return &domain.PendingToolCallBatch{
		ID:             batchID,
		ConversationID: state.ConversationID,
		BusinessID:     state.BusinessID,
		ProjectID:      state.ProjectID,
		UserID:         state.UserID,
		MessageID:      state.MessageID,
		Calls:          calls,
		ModelMessages:  msgSnapshot,
		IterationIdx:   state.Iter,
		// Status / CreatedAt / UpdatedAt / ExpiresAt set by the repo
		// (InsertPreparing sets status=preparing; PromoteToPending
		// sets status=pending + expires_at=now+24h per Plan 16-02).
	}
}

// summarizeManualCalls projects the LLM's raw tool_call list into the shape
// emitted on the tool_approval_required SSE event. EditableFields comes from
// the tool registry (Plan 16-03); Floor is always ToolFloorManual because
// these are the calls that triggered the pause.
func summarizeManualCalls(reg *tools.Registry, calls []llm.ToolCall) []ApprovalCallSummary {
	out := make([]ApprovalCallSummary, 0, len(calls))
	for _, tc := range calls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{"raw": tc.Function.Arguments}
			}
		}
		out = append(out, ApprovalCallSummary{
			CallID:         tc.ID,
			ToolName:       tc.Function.Name,
			Args:           args,
			EditableFields: reg.EditableFields(tc.Function.Name),
			Floor:          domain.ToolFloorManual,
		})
	}
	return out
}
