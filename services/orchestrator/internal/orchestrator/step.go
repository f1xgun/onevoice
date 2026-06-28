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

// StepOutcome is the terminal state returned by stepRun. See docs/orchestrator/step.md.
type StepOutcome int

const (
	// OutcomeDone — LLM returned a terminal response with no tool calls.
	OutcomeDone StepOutcome = iota
	// OutcomePaused — manual-floor tool surfaced; batch persisted and pause event emitted. Goroutine MUST exit.
	OutcomePaused
	// OutcomeError — unrecoverable error; EventError already emitted.
	OutcomeError
	// OutcomeMaxIterations — safety cap hit; EventError already emitted.
	OutcomeMaxIterations
)

// RunState is the mutable loop state; serialized to PendingToolCallBatch.ModelMessages at pause.
// See docs/orchestrator/step.md for field semantics and snapshot envelope.
type RunState struct {
	// Messages MUST NOT carry a leading role:"system" entry — system content
	// lives on SystemPlatform/SystemBusiness. Legacy Resume snapshots with a
	// leading system message fall back to the scrub path in resume.go.
	Messages []llm.Message

	// SystemPlatform is Block 1 (cached via cache_control:ephemeral, byte-stable per locale).
	SystemPlatform string
	// SystemBusiness is Block 2 (per-business, NEVER carries cache_control — would defeat cross-business reuse).
	SystemBusiness string

	AvailableTools []llm.ToolDefinition

	BusinessApprovals        map[string]domain.ToolFloor
	ProjectApprovalOverrides map[string]domain.ToolFloor

	ConversationID string
	BusinessID     string
	ProjectID      string
	UserID         string
	MessageID      string

	Model string
	Tier  string

	// UserUUID is the uuid.UUID form taken by LLMClient.Chat (alongside string UserID).
	UserUUID uuid.UUID

	// Iter is the 0-based iteration counter; resume continues at Iter+1.
	Iter int

	// Accumulated*Tokens are running per-conversation sums persisted in the
	// snapshot so resume doesn't reset the budget to zero.
	AccumulatedInputTokens  int
	AccumulatedOutputTokens int
}

// ErrConversationTokenCap fires when the per-conversation input or output token budget is exhausted.
var ErrConversationTokenCap = errors.New("conversation token cap exceeded")

// Two-locale fallback strings for the conversation_token_cap SSE error.
// The frontend keys off Event.Code; content is just the human-readable fallback.
const (
	conversationCapMessageRU = "Этот диалог достиг лимита токенов. Создайте новый чат, чтобы продолжить."
	conversationCapMessageEN = "This conversation has reached its token limit. Start a new chat to continue."
)

// friendlyConversationCapMessage returns the cap message in the locale from ctx.
func friendlyConversationCapMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return conversationCapMessageEN
	}
	return conversationCapMessageRU
}

// Two-locale fallback strings for the max_iterations SSE error. The numeric
// iteration count is intentionally kept OUT of the user-facing text — it lives
// in the slog line and metrics instead.
const (
	maxIterationsMessageRU = "Не получилось завершить запрос — действие оказалось слишком сложным. Попробуйте сформулировать его проще или разбить на части."
	maxIterationsMessageEN = "Could not complete the request — the task was too complex. Try rephrasing it or breaking it into smaller steps."
)

// Two-locale fallback strings for the generic internal_error SSE error. The
// raw failure detail stays in logs; the user only ever sees this calm message.
const (
	internalErrorMessageRU = "Произошла внутренняя ошибка. Попробуйте ещё раз чуть позже."
	internalErrorMessageEN = "Something went wrong on our side. Please try again in a moment."
)

// Two-locale fallback strings for the approval_expired SSE error emitted when
// a resume targets a batch that already expired.
const (
	approvalExpiredMessageRU = "Время на подтверждение действия истекло. Отправьте новое сообщение, чтобы продолжить."
	approvalExpiredMessageEN = "The approval window for this action has expired. Send a new message to continue."
)

// friendlyMaxIterationsMessage returns the max_iterations message in the locale from ctx.
func friendlyMaxIterationsMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return maxIterationsMessageEN
	}
	return maxIterationsMessageRU
}

// friendlyInternalErrorMessage returns the generic internal-error message in the locale from ctx.
func friendlyInternalErrorMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return internalErrorMessageEN
	}
	return internalErrorMessageRU
}

// friendlyApprovalExpiredMessage returns the approval-expired message in the locale from ctx.
func friendlyApprovalExpiredMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return approvalExpiredMessageEN
	}
	return approvalExpiredMessageRU
}

// In-loop rate-limiter sentinel translations; matches the chat handler's bootstrap-error catalog.
const (
	dailySpendInLoopRU        = "Достигнут дневной лимит расходов для этого бизнеса. Попробуйте завтра."
	dailySpendInLoopEN        = "Daily spend limit reached for this business. Try again tomorrow."
	rateLimitUnavailInLoopRU  = "Сервис ограничения запросов временно недоступен. Попробуйте позже."
	rateLimitUnavailInLoopEN  = "Rate limiter is temporarily unavailable. Please try again shortly."
	rateLimitExceededInLoopRU = "Слишком много запросов. Подождите минуту и повторите."
	rateLimitExceededInLoopEN = "Too many requests. Wait a minute and try again."
)

// translateChatError maps Router rate-limiter sentinels to coded SSE Events.
// Non-sentinel errors collapse to a generic internal_error code with a
// localized message; the raw detail is logged, never shown to the user.
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
		slog.ErrorContext(ctx, "stepRun: LLM call failed", "error", err)
		return Event{Type: EventError, Code: "internal_error", Content: friendlyInternalErrorMessage(ctx)}
	}
}

// stepRun is the shared loop body for Run and Resume. MUST NOT block waiting
// for approval — on manual-floor classification it persists the batch, emits
// tool_approval_required, and returns OutcomePaused.
// See docs/orchestrator/step.md.
//
//nolint:unparam // StepOutcome consumed by Resume's dispatchApprovedCalls path.
func (o *Orchestrator) stepRun(ctx context.Context, state *RunState, out chan<- Event) (StepOutcome, string, error) {
	for state.Iter < o.options.MaxIterations {
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
		o.applyOutboundRedaction(&llmReq)
		resp, err := o.llm.Chat(ctx, llmReq)
		if err != nil {
			ev := translateChatError(ctx, err)
			select {
			case out <- ev:
			case <-ctx.Done():
			}
			return OutcomeError, "", err
		}

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

		if len(resp.ToolCalls) == 0 {
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

		if resp.Content != "" {
			select {
			case out <- Event{Type: EventText, Content: resp.Content}:
			case <-ctx.Done():
				return OutcomeError, "", ctx.Err()
			}
		}

		state.Messages = append(state.Messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		autoCalls, manualCalls, forbiddenCalls := hitl.Bucket(
			o.tools.Floor,
			state.BusinessApprovals,
			state.ProjectApprovalOverrides,
			resp.ToolCalls,
		)

		offered := offeredToolSet(state.AvailableTools)
		autoCalls, forbiddenCalls = enforceOfferedAuto(autoCalls, forbiddenCalls, offered)

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

		if len(autoCalls) > 0 {
			if !o.dispatchToolCalls(ctx, out, autoCalls, &state.Messages) {
				return OutcomeError, "", ctx.Err()
			}
		}

		if len(manualCalls) > 0 {
			if o.pendingRepo == nil {
				err := fmt.Errorf("HITL not configured: manual-floor tool classified but pendingRepo is nil")
				slog.ErrorContext(ctx, "stepRun: manual-floor tool surfaced without HITL repo", "error", err)
				select {
				case out <- Event{Type: EventError, Code: "internal_error", Content: friendlyInternalErrorMessage(ctx)}:
				case <-ctx.Done():
				}
				return OutcomeError, "", err
			}

			batchID := uuid.NewString()
			batch := buildPendingBatch(batchID, state, manualCalls)

			if err := o.pendingRepo.Persist(ctx, batch); err != nil {
				slog.ErrorContext(ctx, "stepRun: failed to persist approval batch", "error", err, "batch_id", batchID)
				select {
				case out <- Event{Type: EventError, Code: "internal_error", Content: friendlyInternalErrorMessage(ctx)}:
				case <-ctx.Done():
				}
				return OutcomeError, "", err
			}

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

		state.Iter++
	}

	slog.WarnContext(ctx, "stepRun: max iterations reached",
		"max_iterations", o.options.MaxIterations,
		"conversation_id", state.ConversationID,
	)
	select {
	case out <- Event{Type: EventError, Code: "max_iterations", Content: friendlyMaxIterationsMessage(ctx)}:
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

// modelMessagesSnapshotV2 versions the pause-snapshot envelope. See docs/orchestrator/step.md.
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

// buildPendingBatch assembles the pause-time PendingToolCallBatch with a versioned snapshot.
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
			CallID:       tc.ID,
			ToolName:     tc.Function.Name,
			Arguments:    args,
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
	}
}

// offeredToolSet collapses the tool definitions actually offered to the model
// this turn into a name set. hitl.Bucket classifies purely off the registry
// Floor, which only knows whether a name is registered — it cannot tell that a
// registered Auto-floor tool was withheld by the project whitelist (e.g.
// WhitelistMode=none yields an empty offered list, or an explicit list omits
// it). The set lets the dispatch path re-assert the whitelist boundary.
func offeredToolSet(defs []llm.ToolDefinition) map[string]bool {
	set := make(map[string]bool, len(defs))
	for _, d := range defs {
		set[d.Function.Name] = true
	}
	return set
}

// enforceOfferedAuto moves any auto-bucketed call whose name was not offered to
// the model this turn into the forbidden bucket. This closes the gap where a
// model recalls (or is injected with) a registered Auto tool that the project
// whitelist withheld: hitl.Bucket would dispatch it because Floor returns
// Forbidden only for UNREGISTERED names. Manual-floor calls already pause and
// unregistered calls are already forbidden, so only the offered-Auto gap needs
// closing here.
func enforceOfferedAuto(autoCalls, forbiddenCalls []llm.ToolCall, offered map[string]bool) (allowed, forbidden []llm.ToolCall) {
	allowed = autoCalls[:0:0]
	forbidden = forbiddenCalls
	for _, tc := range autoCalls {
		if offered[tc.Function.Name] {
			allowed = append(allowed, tc)
			continue
		}
		forbidden = append(forbidden, tc)
	}
	return allowed, forbidden
}

// summarizeManualCalls projects raw tool_calls into tool_approval_required wire shape.
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
