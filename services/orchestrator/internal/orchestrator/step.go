package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitl"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
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

// RunState is the mutable loop state. Serialized to
// PendingToolCallBatch.ModelMessages at pause time, reconstructed by Resume.
type RunState struct {
	// Messages is the conversation history forwarded to the LLM. MUST NOT
	// carry a leading role:"system" entry — system content lives on
	// SystemPlatform / SystemBusiness and is wired into SystemBlocks by
	// stepRun. Legacy Resume snapshots with a leading system message fall
	// back to the scrub path in resume.go.
	Messages []llm.Message

	// SystemPlatform is Block 1 of the two-block system prompt — the
	// platform-wide prefix Anthropic caches via cache_control:ephemeral.
	// Byte-stable per locale.
	SystemPlatform string

	// SystemBusiness is Block 2 — the per-business prefix. NEVER carries
	// cache_control; every business has distinct bytes and stamping would
	// defeat cross-business cache reuse.
	SystemBusiness string

	AvailableTools []llm.ToolDefinition

	// BusinessApprovals / ProjectApprovalOverrides are snapshots of
	// settings.tool_approvals / projects.approval_overrides. Nil maps OK.
	BusinessApprovals        map[string]domain.ToolFloor
	ProjectApprovalOverrides map[string]domain.ToolFloor

	// Identity fields persisted on PendingToolCallBatch for business-scoped
	// access control at resolve time.
	ConversationID string
	BusinessID     string

	// ProjectID is empty when conversation has no project. Threaded into
	// batch.ProjectID so TOCTOU re-check can load project overrides.
	ProjectID string
	UserID    string
	MessageID string

	// Model / Tier route subsequent iterations (including post-resume) to
	// the same provider+tier as the initial request.
	Model string
	Tier  string

	// UserUUID is the uuid.UUID form taken by LLMClient.Chat — kept
	// alongside string UserID for legacy callers.
	UserUUID uuid.UUID

	// Iter is the 0-based iteration counter; resume continues at Iter+1.
	Iter int

	// Accumulated*Tokens are running per-conversation sums persisted in the
	// pause snapshot so resume doesn't reset the budget to zero.
	AccumulatedInputTokens  int
	AccumulatedOutputTokens int
}

// ErrConversationTokenCap fires when the per-conversation token budget on
// either axis (input or output) is exhausted. Distinct from MaxIterations
// because the cap is a softer / earlier guard.
var ErrConversationTokenCap = errors.New("conversation token cap exceeded")

// Friendly user-facing text for the conversation_token_cap SSE error. The
// frontend keys off Event.Code rather than parsing this string, so the
// content is just the human-readable fallback. Two locales only — no
// pkg/i18n catalog migration.
const (
	conversationCapMessageRU = "Этот диалог достиг лимита токенов. Создайте новый чат, чтобы продолжить."
	conversationCapMessageEN = "This conversation has reached its token limit. Start a new chat to continue."
)

// friendlyConversationCapMessage returns the cap message in the locale
// resolved off the request context. The cap-hit code stays in Event.Code.
func friendlyConversationCapMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return conversationCapMessageEN
	}
	return conversationCapMessageRU
}

// Friendly text for rate-limiter sentinels that surface mid-loop. Two-locale
// switch matching the chat handler's bootstrap-error translation.
const (
	dailySpendInLoopRU        = "Достигнут дневной лимит расходов для этого бизнеса. Попробуйте завтра."
	dailySpendInLoopEN        = "Daily spend limit reached for this business. Try again tomorrow."
	rateLimitUnavailInLoopRU  = "Сервис ограничения запросов временно недоступен. Попробуйте позже."
	rateLimitUnavailInLoopEN  = "Rate limiter is temporarily unavailable. Please try again shortly."
	rateLimitExceededInLoopRU = "Слишком много запросов. Подождите минуту и повторите."
	rateLimitExceededInLoopEN = "Too many requests. Wait a minute and try again."
)

// translateChatError converts a Router-side rate-limiter sentinel into an SSE
// Event carrying the machine-readable Code. Errors without a matching
// sentinel keep their legacy free-text shape so observability is preserved.
func translateChatError(ctx context.Context, err error) Event {
	en := i18n.LocaleFromContext(ctx) == language.English
	switch {
	case errors.Is(err, llm.ErrDailySpendExceeded):
		msg := dailySpendInLoopRU
		if en {
			msg = dailySpendInLoopEN
		}
		return Event{Type: EventError, Code: "daily_spend_exceeded", Content: msg}
	case errors.Is(err, llm.ErrRateLimitUnavailable):
		msg := rateLimitUnavailInLoopRU
		if en {
			msg = rateLimitUnavailInLoopEN
		}
		return Event{Type: EventError, Code: "rate_limit_unavailable", Content: msg}
	case errors.Is(err, llm.ErrRateLimitExceeded):
		msg := rateLimitExceededInLoopRU
		if en {
			msg = rateLimitExceededInLoopEN
		}
		return Event{Type: EventError, Code: "rate_limit_exceeded", Content: msg}
	default:
		return Event{Type: EventError, Content: err.Error()}
	}
}

// stepRun is the shared loop body for Run (fresh turns) and Resume
// (post-approval continuation). MUST NOT block waiting for approval — on
// manual-floor classification it persists the batch, emits
// tool_approval_required, and returns OutcomePaused.
//
//nolint:unparam // StepOutcome consumed by Resume's dispatchApprovedCalls path.
func (o *Orchestrator) stepRun(ctx context.Context, state *RunState, out chan<- Event) (StepOutcome, string, error) {
	for state.Iter < o.options.MaxIterations {
		// Block 1 (platform) carries CacheBoundary=true so Anthropic stamps
		// cache_control on it; Block 2 (per-business) does not, keeping the
		// cache prefix byte-stable across businesses.
		// parseBizID degrades malformed BusinessID to uuid.Nil so the router's
		// nil-guard skips billing instead of writing a corrupt row.
		llmReq := llm.ChatRequest{
			UserID:         state.UserUUID,
			BusinessID:     parseBizID(state.BusinessID),
			ConversationID: state.ConversationID,
			Model:          state.Model,
			Messages:       state.Messages,
			Tools:          state.AvailableTools,
			Tier:           state.Tier,
			MaxTokens:      llm.DefaultMaxTokensFor(state.Model),
		}
		if state.SystemPlatform != "" || state.SystemBusiness != "" {
			llmReq.SystemBlocks = []llm.SystemBlock{
				{Text: state.SystemPlatform, CacheBoundary: true},
				{Text: state.SystemBusiness},
			}
		}
		resp, err := o.llm.Chat(ctx, llmReq)
		if err != nil {
			// Translate rate-limiter sentinels to coded SSE error events so
			// consumers can branch on Code without parsing free text.
			ev := translateChatError(ctx, err)
			select {
			case out <- ev:
			case <-ctx.Done():
			}
			return OutcomeError, "", err
		}

		// Accumulate BEFORE the no-tool-calls branch so the cap fires
		// uniformly on both terminal and tool-call iterations. Mid-iter
		// overshoot is caught because the comparison is against the sum.
		state.AccumulatedInputTokens += resp.Usage.InputTokens
		state.AccumulatedOutputTokens += resp.Usage.OutputTokens
		if o.options.ConversationInputCap > 0 && state.AccumulatedInputTokens >= o.options.ConversationInputCap {
			metrics.LLMConversationCapHit.WithLabelValues("input").Inc()
			select {
			case out <- Event{
				Type:    EventError,
				Code:    "conversation_token_cap",
				Content: friendlyConversationCapMessage(ctx),
			}:
			case <-ctx.Done():
			}
			return OutcomeError, "", ErrConversationTokenCap
		}
		if o.options.ConversationOutputCap > 0 && state.AccumulatedOutputTokens >= o.options.ConversationOutputCap {
			metrics.LLMConversationCapHit.WithLabelValues("output").Inc()
			select {
			case out <- Event{
				Type:    EventError,
				Code:    "conversation_token_cap",
				Content: friendlyConversationCapMessage(ctx),
			}:
			case <-ctx.Done():
			}
			return OutcomeError, "", ErrConversationTokenCap
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

		// hitl.Bucket folds registry-floor + Resolve into one pure call so
		// this loop and the resolve-path TOCTOU re-check cannot diverge.
		autoCalls, manualCalls, forbiddenCalls := hitl.Bucket(
			o.tools.Floor,
			state.BusinessApprovals,
			state.ProjectApprovalOverrides,
			resp.ToolCalls,
		)

		// Forbidden calls: synthesize rejection message + emit tool_rejected,
		// DO NOT dispatch. The LLM sees the outcome on the next iteration.
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

		// Auto calls dispatched in parallel. dispatchToolCalls appends tool-role
		// messages in the original tool_calls order regardless of completion
		// order, preserving the assistant.tool_calls[i].id ↔ tool[i] mapping.
		if len(autoCalls) > 0 {
			if !o.dispatchToolCalls(ctx, out, autoCalls, &state.Messages) {
				return OutcomeError, "", ctx.Err()
			}
		}

		// Manual calls: Persist MUST succeed before emitting the pause event.
		// Persist's internal preparing → pending two-step is the crash-recovery
		// seam for the orphan-reconcile sweep.
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

// parseBizID degrades empty/malformed BusinessID to uuid.Nil so the router's
// nil-guard skips billing instead of writing a corrupt row (fail-closed).
// uuid.MustParse would panic — unacceptable on the hot-path llm.Chat call.
func parseBizID(s string) uuid.UUID {
	if s == "" {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		slog.Warn("stepRun: malformed BusinessID, billing will be skipped",
			"business_id", s, "error", err)
		return uuid.Nil
	}
	return parsed
}

// modelMessagesSnapshotV2 versions the pause-snapshot envelope. V=2 carries
// SystemPlatform/SystemBusiness alongside system-free Messages; legacy
// batches unmarshal as raw []llm.Message and Resume falls through to the
// scrub path.
type modelMessagesSnapshotV2 struct {
	V              int           `json:"v"`
	Messages       []llm.Message `json:"messages"`
	SystemPlatform string        `json:"system_platform,omitempty"`
	SystemBusiness string        `json:"system_business,omitempty"`
	// omitempty so pre-cap snapshots stay byte-identical; legacy batches
	// hydrate at 0 (correct — pre-cap turns weren't subject to the cap).
	AccumulatedInputTokens  int `json:"accumulated_input_tokens,omitempty"`
	AccumulatedOutputTokens int `json:"accumulated_output_tokens,omitempty"`
}

// buildPendingBatch assembles the pause-time PendingToolCallBatch.
// ModelMessages carries a versioned snapshot so Resume can rebuild RunState
// after a process restart.
func buildPendingBatch(batchID string, state *RunState, manualCalls []llm.ToolCall) *domain.PendingToolCallBatch {
	snapshot := modelMessagesSnapshotV2{
		V:                       2,
		Messages:                state.Messages,
		SystemPlatform:          state.SystemPlatform,
		SystemBusiness:          state.SystemBusiness,
		AccumulatedInputTokens:  state.AccumulatedInputTokens,
		AccumulatedOutputTokens: state.AccumulatedOutputTokens,
	}
	msgSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		// Tolerated — Resume fails cleanly with EventError "corrupt snapshot"
		// if this ever happens. Only theoretical for llm.Message + strings.
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
			// Only manual-floor calls reach this branch (stepRun bucketing
			// guarantees), so the constant is correct without a registry lookup.
			FloorAtPause: domain.ToolFloorManual,
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
		// Status / CreatedAt / UpdatedAt / ExpiresAt set by pendingRepo.Persist.
	}
}

// summarizeManualCalls projects raw tool_calls into the
// tool_approval_required SSE event shape. Floor is always ToolFloorManual
// because only paused calls reach here.
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
