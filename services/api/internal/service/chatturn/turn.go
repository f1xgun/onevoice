package chatturn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Deps wires the eleven repository / service / transport dependencies a Turn
// needs. Mirrors the dependency set of services/api/internal/handler.ChatProxyHandler;
// the deepening refactor moves the lifecycle into chatturn while keeping the
// dep surface byte-identical.
//
// All fields are required EXCEPT Titler — a nil Titler is the graceful-
// disable path (auto-titling off in dev / tests without OpenAI credentials).
type Deps struct {
	Business     BusinessReader
	Integrations IntegrationLister
	Projects     ProjectReader

	Conversations domain.ConversationRepository
	Messages      domain.MessageRepository
	Pending       domain.PendingToolCallRepository
	Posts         domain.PostRepository
	Reviews       domain.ReviewRepository
	AgentTasks    domain.AgentTaskRepository

	TaskHub *taskhub.Hub
	Orch    *orchestratorclient.Client
	Titler  Titler // nil → auto-title disabled
}

// Turn is the chat-turn lifecycle: one HTTP request, one Run call, one
// terminal TurnOutcome. The struct is stateless across calls — Run owns the
// per-request state on its stack. Safe for concurrent calls from a single
// *Turn instance.
type Turn struct {
	deps Deps
}

// New constructs a Turn from a wired Deps. Required dependencies that are
// nil produce a panic at construction time, mirroring the existing
// ChatProxyHandler invariants (CONVENTIONS.md §"Wiring panics — fail fast at
// boot, not on the first request").
func New(deps Deps) *Turn {
	if deps.Business == nil {
		panic("chatturn.New: Business cannot be nil")
	}
	if deps.Integrations == nil {
		panic("chatturn.New: Integrations cannot be nil")
	}
	if deps.Projects == nil {
		panic("chatturn.New: Projects cannot be nil")
	}
	if deps.Conversations == nil {
		panic("chatturn.New: Conversations cannot be nil")
	}
	if deps.Messages == nil {
		panic("chatturn.New: Messages cannot be nil")
	}
	if deps.Pending == nil {
		panic("chatturn.New: Pending cannot be nil")
	}
	if deps.Orch == nil {
		panic("chatturn.New: Orch cannot be nil")
	}
	// Posts, Reviews, AgentTasks, TaskHub, Titler can all be nil — they
	// gate optional behaviors (postal fanout / auto-title) downstream.
	return &Turn{deps: deps}
}

// Run drives one chat-turn lifecycle:
//
//  1. Gate — fresh / rejoin-resume / re-emit-approval / inline-error.
//  2. Enrich — load business, integrations, project, history; persist user msg.
//  3. Stream — open orchestrator stream, dispatch SSE events through emit.
//  4. Post-stream — persist assistant message, fire auto-title gate, postal fanout.
//
// The emit callback (optional, may be nil) is invoked synchronously for
// each parsed SSE event after streamOrchestrator has already written the
// raw bytes to w; callers use it for observability hooks or test
// assertions. The http.ResponseWriter is threaded through because the HITL
// gate's rejoin-resume / re-emit-approval / inline-error branches write
// directly to the wire (legacy behavior preserved verbatim).
//
// Returns the terminal outcome. The handler maps it to an HTTP status code
// only when nothing has been written to the wire yet (e.g., the gate-error
// 500 or the OrchestratorUnavailable 502 paths); once SSE bytes start
// flowing, the body is committed and status-code mapping is moot.
func (t *Turn) Run(
	ctx context.Context,
	w http.ResponseWriter,
	req TurnRequest,
	emit func(sse.Event),
) (TurnOutcome, error) {
	// Step 1 — gate.
	action, activeMsg, batch, batchID, gateErr := t.gateOnRequest(ctx, req.ConversationID, req.ResumeBatchID)
	if gateErr != nil {
		slog.ErrorContext(ctx, "chatturn: gate failed", "error", gateErr)
		return OutcomeError, fmt.Errorf("chatturn: gate: %w", gateErr)
	}
	switch action {
	case gateRejoinResume:
		return t.streamResume(ctx, w, req.ConversationID, activeMsg, batchID)
	case gateReemitApproval:
		t.reemitApprovalEvent(w, batch)
		return OutcomeReemittedApproval, nil
	case gateInlineError:
		t.sseInlineError(w, "turn_already_in_progress")
		return OutcomeInlineError, nil
	case gateFresh:
		// fall through to fresh-turn path
	}

	// Step 2 — enrich + persist user message.
	enriched, err := t.enrich(ctx, req)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return OutcomeBusinessNotFound, err
		}
		slog.ErrorContext(ctx, "chatturn: enrich failed", "error", err)
		return OutcomeError, fmt.Errorf("chatturn: enrich: %w", err)
	}
	if perr := t.persistUserMessage(ctx, enriched.userMessage); perr != nil {
		slog.ErrorContext(ctx, "chatturn: failed to persist user message", "error", perr)
	}

	// Step 3 — open orchestrator stream.
	streamStartID := streamStartMessageID()
	state := newStreamState()
	taskOpsCtx, cancelTaskOps := context.WithTimeout(context.Background(), streamBudget)
	if corrID := logger.CorrelationIDFromContext(ctx); corrID != "" {
		taskOpsCtx = logger.WithCorrelationID(taskOpsCtx, corrID)
	}
	defer cancelTaskOps()

	body, _ := json.Marshal(t.buildOrchestratorRequest(req, enriched))
	streamErr := t.streamOrchestrator(ctx, taskOpsCtx, w, req.ConversationID, body, nil, enriched.business.ID.String(), state, emit)
	if streamErr != nil && state.pauseEvent == nil && state.streamErrContent == "" &&
		!errors.Is(streamErr, context.Canceled) && strings.Contains(streamErr.Error(), "stream chat") {
		// Surface as OrchestratorUnavailable so the handler can map to 502.
		// This replaces the legacy strings.Contains check on the raw error
		// string with an explicit Turn-level signal.
		return OutcomeOrchestratorUnavailable, streamErr
	}

	// Step 4 — post-stream persistence + fanout.
	t.persistAfterStream(ctx, req, enriched, streamStartID, state)
	if t.deps.Posts != nil || t.deps.Reviews != nil {
		sideCtx, cancel := t.persistContext(ctx)
		defer cancel()
		t.recordPostsAndReviews(sideCtx, enriched.business.ID.String(), state.toolCalls, state.toolResults)
	}

	if state.pauseEvent != nil {
		return OutcomePauseHITL, nil
	}
	if state.streamErrContent != "" {
		return OutcomeError, nil
	}
	return OutcomeDone, nil
}

// persistAfterStream writes the assistant Message after the SSE loop drains.
// Two branches:
//
//   - pauseEvent != nil — write Status=PendingApproval with per-call
//     ApprovalID=<batch>-<call> + Status=Pending stamped on every ToolCall.
//   - done / error — write Status=Complete. If content is empty AND a
//     stream-error frame was received, wrap it through the i18n catalog at
//     write-time so chat history renders in the writer's language forever.
//
// Auto-title fires on done only (skipped on error so a failed turn doesn't
// produce a misleading title).
func (t *Turn) persistAfterStream(
	parentCtx context.Context,
	req TurnRequest,
	enriched *enrichmentResult,
	streamStartID string,
	state *streamState,
) {
	if state.pauseEvent != nil {
		saveCtx, cancel := t.persistContext(parentCtx)
		defer cancel()
		pendingToolCalls := make([]domain.ToolCall, 0, len(state.toolCalls))
		for _, tc := range state.toolCalls {
			pendingToolCalls = append(pendingToolCalls, domain.ToolCall{
				ID:         tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
				ApprovalID: fmt.Sprintf("%s-%s", state.pauseEvent.BatchID, tc.ID),
				Status:     domain.ToolCallStatusPending,
			})
		}
		assistantMsg := &domain.Message{
			ID:             streamStartID,
			ConversationID: req.ConversationID,
			Role:           roleAssistant,
			Content:        state.assistantText.String(),
			ToolCalls:      pendingToolCalls,
			Status:         domain.MessageStatusPendingApproval,
		}
		if err := t.persistAssistantPause(saveCtx, assistantMsg); err != nil {
			slog.WarnContext(saveCtx, "chatturn: failed to persist assistant pending_approval message",
				"error", err, "conversation_id", req.ConversationID, "batch_id", state.pauseEvent.BatchID)
		}
		return
	}

	// Done / error persistence.
	if state.assistantText.Len() == 0 && len(state.toolCalls) == 0 && state.streamErrContent == "" {
		return
	}
	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	content := state.assistantText.String()
	if content == "" && state.streamErrContent != "" {
		// Localize at write-time using the locale on saveCtx (propagated by
		// persistContext from the request edge). Stored in MongoDB so chat
		// history renders in the writer's language forever — we don't
		// retroactively re-translate.
		content = i18n.Tr(saveCtx, "api.chat.stream_error_wrapper", state.streamErrContent)
	}
	assistantMsg := &domain.Message{
		ID:             streamStartID,
		ConversationID: req.ConversationID,
		Role:           roleAssistant,
		Content:        content,
		ToolCalls:      state.toolCalls,
		ToolResults:    state.toolResults,
		Status:         domain.MessageStatusComplete,
	}
	if err := t.persistAssistantComplete(saveCtx, assistantMsg); err != nil {
		slog.ErrorContext(saveCtx, "chatturn: failed to save assistant message", "error", err)
	}
	// Skip auto-title on errors.
	if state.streamErrContent == "" {
		t.fireAutoTitleIfPending(parentCtx, req.ConversationID, enriched.business.ID.String(), req.Message, state.assistantText.String())
	}
}
