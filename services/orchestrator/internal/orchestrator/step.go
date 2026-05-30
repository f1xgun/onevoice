package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitl"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
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
	// the batch was persisted via pendingRepo.Persist and the
	// tool_approval_required SSE event emitted. Goroutine MUST exit.
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
	// Messages is the conversation history forwarded to the LLM. As of Plan
	// 24-02 it MUST NOT carry a leading role:"system" entry — system content
	// lives on SystemPlatform / SystemBusiness and is wired into
	// llm.ChatRequest.SystemBlocks by stepRun. Legacy Resume snapshots (pre
	// Plan 24-02) may still contain a leading system message; Resume detects
	// that shape and falls back to the legacy scrub path (see resume.go).
	Messages []llm.Message

	// SystemPlatform is Block 1 of the two-block system prompt — the
	// platform-wide prefix that Anthropic caches via cache_control:ephemeral.
	// Byte-stable per locale (asserted by TestSystemPromptHash_Stability).
	SystemPlatform string

	// SystemBusiness is Block 2 of the two-block system prompt — the per-
	// business prefix (name, integrations, current time, optional project
	// block). NEVER carries cache_control: every business has distinct bytes
	// here and stamping it would defeat cross-business cache reuse.
	SystemBusiness string

	// AvailableTools is the whitelist-filtered tool set for this turn.
	AvailableTools []llm.ToolDefinition

	// BusinessApprovals is the businesses.settings.tool_approvals snapshot.
	// Nil maps are tolerated by hitl.Resolve.
	BusinessApprovals map[string]domain.ToolFloor

	// ProjectApprovalOverrides is the projects.approval_overrides snapshot.
	// Nil maps are tolerated by hitl.Resolve.
	ProjectApprovalOverrides map[string]domain.ToolFloor

	// ConversationID / BusinessID / UserID / MessageID are the identity
	// fields persisted on PendingToolCallBatch so that the resolve
	// handler can enforce business-scoped access control.
	ConversationID string
	BusinessID     string

	// ProjectID is nullable — empty when a conversation has no project
	// ("Без проекта"). Threaded into batch.ProjectID so the
	// TOCTOU re-check can load the project's approval_overrides at
	// resolve time.
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
// OutcomePaused so the caller's goroutine exits cleanly.
//
// The StepOutcome return is currently unused by Run (it just calls close(ch)) but
// IS consumed by Resume's dispatchApprovedCalls path — suppressing unparam
// because the return value is load-bearing downstream.
//
//nolint:unparam // StepOutcome consumed by Resume — see resume.go.
func (o *Orchestrator) stepRun(ctx context.Context, state *RunState, out chan<- Event) (StepOutcome, string, error) {
	for state.Iter < o.options.MaxIterations {
		// 1. Call the LLM. Plan 24-02: SystemBlocks is the canonical channel
		// for system content. Block 1 (platform) carries CacheBoundary=true so
		// Anthropic stamps cache_control on it; Block 2 (per-business) does
		// not — keeps the cache prefix byte-stable across businesses.
		llmReq := llm.ChatRequest{
			UserID:   state.UserUUID,
			Model:    state.Model,
			Messages: state.Messages,
			Tools:    state.AvailableTools,
			Tier:     state.Tier,
		}
		if state.SystemPlatform != "" || state.SystemBusiness != "" {
			llmReq.SystemBlocks = []llm.SystemBlock{
				{Text: state.SystemPlatform, CacheBoundary: true},
				{Text: state.SystemBusiness},
			}
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

		// 4. Classify every LLM-proposed tool call through hitl.Bucket,
		//    bucketing into auto / manual / forbidden. hitl.Bucket folds the
		//    registry-floor lookup + Resolve into one pure call so this loop
		//    body and the resolve-path TOCTOU re-check cannot diverge.
		autoCalls, manualCalls, forbiddenCalls := hitl.Bucket(
			o.tools.Floor,
			state.BusinessApprovals,
			state.ProjectApprovalOverrides,
			resp.ToolCalls,
		)

		// 5. Forbidden calls → synthesize rejection message, emit
		//    tool_rejected event, DO NOT dispatch. The LLM sees the
		//    outcome on the next iteration.
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

		// 6. Auto calls — dispatch in parallel via dispatchToolCalls (ported from
		// main's c662290) so independent platform broadcasts complete
		// concurrently. dispatchToolCalls appends tool-role messages in the
		// original tool_calls order regardless of completion order, preserving
		// the LLM's assistant.tool_calls[i].id ↔ tool[i] correspondence.
		if len(autoCalls) > 0 {
			if !o.dispatchToolCalls(ctx, out, autoCalls, &state.Messages) {
				return OutcomeError, "", ctx.Err()
			}
		}

		// 7. Manual calls — persist, emit pause event, return.
		//    Order invariant: Persist succeeds BEFORE emitting the pause
		//    event. Persist's internal preparing → pending two-step is the
		//    crash-recovery seam for the orphan-reconcile sweep; callers see
		//    a single transition from "no batch" to "pending".
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

			if err := o.pendingRepo.Persist(ctx, batch); err != nil {
				select {
				case out <- Event{Type: EventError, Content: fmt.Sprintf("failed to persist approval batch: %v", err)}:
				case <-ctx.Done():
				}
				return OutcomeError, "", err
			}

			// Single tool_approval_required event per turn covering every
			// manual call in this iteration (one card per batch).
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

// modelMessagesSnapshotV2 is the Plan 24-02 versioned envelope that wraps the
// pre-Phase-24 raw []llm.Message blob. Versioning lets Resume distinguish
// post-24-02 batches (where Messages is system-free and SystemPlatform/
// SystemBusiness travel alongside) from legacy batches (where Messages still
// has a leading role:"system" entry and the envelope fields are absent).
//
// Marshaling: post-24-02 batches always emit V=2. Legacy batches written
// pre-24-02 unmarshal as raw []llm.Message — Resume detects the JSON shape and
// falls through to the legacy scrub path.
type modelMessagesSnapshotV2 struct {
	V              int           `json:"v"` // 2 — Plan 24-02 envelope
	Messages       []llm.Message `json:"messages"`
	SystemPlatform string        `json:"system_platform,omitempty"`
	SystemBusiness string        `json:"system_business,omitempty"`
}

// buildPendingBatch assembles the PendingToolCallBatch that will be persisted
// at pause time. ProjectID is threaded through from RunState so the
// TOCTOU re-check can load the project's approval_overrides at resolve time.
// ModelMessages carries a versioned snapshot of the conversation history +
// SystemBlocks so Resume can rebuild RunState after a process restart.
func buildPendingBatch(batchID string, state *RunState, manualCalls []llm.ToolCall) *domain.PendingToolCallBatch {
	snapshot := modelMessagesSnapshotV2{
		V:              2,
		Messages:       state.Messages,
		SystemPlatform: state.SystemPlatform,
		SystemBusiness: state.SystemBusiness,
	}
	msgSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		// Snapshot marshal failure is silently tolerated here — Resume will
		// fail cleanly with EventError "corrupt snapshot" if this ever
		// happens. llm.Message + strings are plain JSON so this is only
		// theoretical.
		slog.Warn("stepRun: failed to marshal messages snapshot", "error", err, "batch_id", batchID)
		msgSnapshot = []byte(`{"v":2,"messages":[]}`)
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
			// Persist the pause-time floor on every
			// PendingCall so the resolve-time TOCTOU re-check consults the
			// same registry the orchestrator used at pause. Only manual-
			// floor calls reach the manualCalls bucket (bucketing in
			// stepRun guarantees this), so the constant is correct without
			// an extra registry lookup. The orchestrator-side recheck in
			// resume.go remains the load-bearing TOCTOU primitive.
			FloorAtPause: domain.ToolFloorManual,
			// Verdict/EditedArgs/Dispatched left zero — populated by
			// the resolve handler.
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
		// Status / CreatedAt / UpdatedAt / ExpiresAt set by the repo —
		// pendingRepo.Persist writes status=pending with
		// expires_at=now+24h after promotion completes.
	}
}

// summarizeManualCalls projects the LLM's raw tool_call list into the shape
// emitted on the tool_approval_required SSE event. EditableFields comes from
// the tool registry; Floor is always ToolFloorManual because
// these are the calls that triggered the pause.
func summarizeManualCalls(reg *toolregistry.Registry, calls []llm.ToolCall) []sse.ApprovalCall {
	out := make([]sse.ApprovalCall, 0, len(calls))
	for _, tc := range calls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{"raw": tc.Function.Arguments}
			}
		}
		out = append(out, sse.ApprovalCall{
			CallID:         tc.ID,
			ToolName:       tc.Function.Name,
			Args:           args,
			EditableFields: reg.EditableFields(tc.Function.Name),
			Floor:          domain.ToolFloorManual,
		})
	}
	return out
}
