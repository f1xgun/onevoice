package chatturn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Deps wires the repository / service / transport dependencies a Turn needs.
// Mirrors the dependency set of services/api/internal/handler.ChatProxyHandler;
// the deepening refactor moves the lifecycle into chatturn while keeping the
// dep surface byte-identical.
//
// All fields are required EXCEPT Titler and Audit — a nil Titler is the
// graceful-disable path (auto-titling off in dev / tests without OpenAI
// credentials), and a nil Audit disables the RPA mutation audit trail.
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
	Titler  Titler       // nil → auto-title disabled
	Audit   audit.Logger // nil → RPA mutation audit disabled

	// PlanResolver resolves the per-business rate-limit tier forwarded to the
	// orchestrator. Optional: a nil resolver preserves the legacy empty-tier
	// behavior. Wired in production via Turn.SetPlanResolver.
	PlanResolver PlanResolver

	// HistoryLimit caps how many prior messages are loaded into the LLM
	// context per turn. A non-positive value falls back to
	// defaultHistoryLimit so a struct-literal construction (tests) stays safe.
	HistoryLimit int
}

// defaultHistoryLimit is the fallback chat-history fetch limit when Deps
// supplies a non-positive HistoryLimit. Mirrors the API config default; the
// operator-facing knob lives in services/api/internal/config (MESSAGE_HISTORY_LIMIT).
const defaultHistoryLimit = 100

// Turn is the chat-turn lifecycle: one HTTP request, one Run call, one
// terminal TurnOutcome. The struct is stateless across calls — Run owns the
// per-request state on its stack. Safe for concurrent calls from a single
// *Turn instance.
type Turn struct {
	deps Deps
}

// SetPlanResolver wires the per-business plan resolver post-construction. The
// wiring layer calls this once at boot so the constructor signature (and its
// many test call sites) stay unchanged. Safe to call before the Turn serves
// traffic; not safe to call concurrently with in-flight Run calls.
func (t *Turn) SetPlanResolver(r PlanResolver) {
	t.deps.PlanResolver = r
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
	if deps.HistoryLimit <= 0 {
		deps.HistoryLimit = defaultHistoryLimit
	}
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
	if !t.ownsConversation(ctx, req.ConversationID, req.UserID.String(), req.BusinessID.String()) {
		return OutcomeConversationNotFound, nil
	}

	action, activeMsg, batch, batchID := t.gateOnRequest(ctx, req.ConversationID, req.ResumeBatchID)
	switch action {
	case gateRejoinResume:
		return t.streamResume(ctx, w, req.ConversationID, activeMsg, batchID)
	case gateReemitApproval:
		t.reemitApprovalEvent(w, batch)
		return OutcomeReemittedApproval, nil
	case gateRejectInProgress:
		// A fresh turn is already streaming (RECENT in_progress placeholder).
		// Reject this concurrent request rather than running a second parallel
		// fresh turn — do NOT fall through into the stream body.
		return OutcomeTurnInProgress, nil
	case gateHealStranded:
		t.finalizeStranded(ctx, activeMsg)
	case gateFresh:
	}

	if req.Message == "" {
		return OutcomeMissingMessage, nil
	}

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

	streamStartID := streamStartMessageID()
	streamStartAt := time.Now()
	t.reserveAssistantPlaceholder(ctx, req.ConversationID, streamStartID, streamStartAt)
	state := newStreamState()
	taskOpsCtx, cancelTaskOps := context.WithTimeout(context.Background(), streamBudget)
	if corrID := logger.CorrelationIDFromContext(ctx); corrID != "" {
		taskOpsCtx = logger.WithCorrelationID(taskOpsCtx, corrID)
	}
	defer cancelTaskOps()

	body, _ := json.Marshal(t.buildOrchestratorRequest(ctx, req, enriched))
	streamErr := t.streamOrchestrator(ctx, taskOpsCtx, w, req.ConversationID, body, nil, enriched.business.ID.String(), state, emit)
	if streamErr != nil && state.pauseEvent == nil && state.streamErrContent == "" &&
		!errors.Is(streamErr, context.Canceled) && strings.Contains(streamErr.Error(), "stream chat") {
		return OutcomeOrchestratorUnavailable, streamErr
	}

	t.persistAfterStream(ctx, req, enriched, streamStartID, streamStartAt, state)
	if t.deps.Posts != nil || t.deps.Reviews != nil || t.deps.Audit != nil {
		sideCtx, cancel := t.persistContext(ctx)
		defer cancel()
		t.recordPostsAndReviews(sideCtx, enriched.business.ID.String(), streamStartID, state.toolCalls, state.toolResults)
		t.auditRPAMutations(sideCtx, enriched.business.ID.String(), req.UserID.String(), state.toolCalls, state.toolResults)
		t.auditPlatformMutations(sideCtx, enriched.business.ID.String(), req.UserID.String(), state.toolCalls, state.toolResults)
	}

	if state.pauseEvent != nil {
		return OutcomePauseHITL, nil
	}
	if state.streamErrContent != "" {
		return OutcomeError, nil
	}
	return OutcomeDone, nil
}

// reserveAssistantPlaceholder writes the in_progress assistant Message before
// the stream opens so a concurrent fresh POST /chat/{id} for the same
// conversation gates on the in-flight turn (via FindByConversationActive)
// instead of running a second parallel fresh turn — two parallel fresh turns
// would each build a history snapshot missing the other, double-fire the
// titler, and tie on created_at. The reservation reuses streamStartID so the
// post-stream finalize (persistAfterStream) Updates the same row; a crash
// before finalize leaves it in_progress and is reclaimed by gateHealStranded on
// the next request. Best-effort: a failed reservation is logged, not fatal —
// the turn still proceeds (degrading to the pre-reservation behavior).
func (t *Turn) reserveAssistantPlaceholder(parentCtx context.Context, conversationID, messageID string, createdAt time.Time) {
	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	placeholder := &domain.Message{
		ID:             messageID,
		ConversationID: conversationID,
		Role:           domain.MessageRoleAssistant,
		Status:         domain.MessageStatusInProgress,
		CreatedAt:      createdAt,
	}
	if err := t.persistAssistantPlaceholder(saveCtx, placeholder); err != nil {
		slog.WarnContext(saveCtx, "chatturn: failed to reserve in_progress assistant placeholder",
			"error", err, "conversation_id", conversationID, "message_id", messageID)
	}
}

// ownsConversation is the cross-tenant access gate for the chat turn. The router
// only authorizes the caller against the {bizID} URL segment; the conversation
// ID is otherwise trusted, so without this check a member of one organization
// could load, append to, and stream another tenant's conversation by guessing
// its ID. The ownership predicate mirrors ConversationService exactly (compare
// the persisted user_id / business_id against the caller).
//
// Returns true (proceed) when the conversation does NOT exist yet — the first
// turn on a brand-new conversation ID legitimately precedes its row, so a
// not-found must NOT be rejected. A lookup error other than not-found is also
// treated as proceed-with-warn so a transient datastore blip degrades to the
// pre-existing behavior rather than a spurious 404. Returns false only when the
// conversation is FOUND and the owner mismatches.
func (t *Turn) ownsConversation(ctx context.Context, conversationID, userID, businessID string) bool {
	conv, err := t.deps.Conversations.GetByID(ctx, conversationID)
	switch {
	case errors.Is(err, domain.ErrConversationNotFound):
		return true
	case err != nil:
		slog.WarnContext(ctx, "chatturn: ownership gate: conversation lookup failed, proceeding",
			"conversation_id", conversationID, "error", err)
		return true
	}
	if conv.UserID != userID || conv.BusinessID != businessID {
		slog.WarnContext(ctx, "chatturn: ownership gate: conversation owner mismatch, rejecting as not-found",
			"conversation_id", conversationID)
		return false
	}
	return true
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
	streamStartAt time.Time,
	state *streamState,
) {
	if state.pauseEvent != nil {
		saveCtx, cancel := t.persistContext(parentCtx)
		defer cancel()
		// Approval-required calls are listed in the pause event. Depending on
		// the model/provider they may ALSO have been streamed as tool_call
		// frames (→ state.toolCalls) or not at all. Persist each call once:
		// the batch calls in their pending form, plus any auto-executed calls
		// that are NOT part of the batch. Building only from state.toolCalls
		// (the old behavior) dropped the calls entirely when no tool_call
		// frame preceded the pause, so a reload showed an empty bubble.
		pendingIDs := make(map[string]struct{}, len(state.pauseEvent.Calls))
		for _, call := range state.pauseEvent.Calls {
			pendingIDs[call.CallID] = struct{}{}
		}
		toolCalls := make([]domain.ToolCall, 0, len(state.toolCalls)+len(state.pauseEvent.Calls))
		for _, tc := range state.toolCalls {
			if _, batched := pendingIDs[tc.ID]; !batched {
				toolCalls = append(toolCalls, tc)
			}
		}
		for _, call := range state.pauseEvent.Calls {
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:         call.CallID,
				Name:       call.ToolName,
				Arguments:  call.Args,
				ApprovalID: fmt.Sprintf("%s-%s", state.pauseEvent.BatchID, call.CallID),
				Status:     domain.ToolCallStatusPending,
			})
		}
		assistantMsg := &domain.Message{
			ID:             streamStartID,
			ConversationID: req.ConversationID,
			Role:           domain.MessageRoleAssistant,
			Content:        state.assistantText.String(),
			ToolCalls:      toolCalls,
			ToolResults:    state.toolResults,
			Status:         domain.MessageStatusPendingApproval,
			CreatedAt:      streamStartAt,
		}
		if err := t.persistAssistantPause(saveCtx, assistantMsg); err != nil {
			slog.WarnContext(saveCtx, "chatturn: failed to persist assistant pending_approval message",
				"error", err, "conversation_id", req.ConversationID, "batch_id", state.pauseEvent.BatchID)
		}
		return
	}

	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	content := state.assistantText.String()
	if content == "" && state.streamErrContent != "" {
		content = i18n.Tr(saveCtx, "api.chat.stream_error_wrapper", state.streamErrContent)
	}
	assistantMsg := &domain.Message{
		ID:             streamStartID,
		ConversationID: req.ConversationID,
		Role:           domain.MessageRoleAssistant,
		Content:        content,
		ToolCalls:      state.toolCalls,
		ToolResults:    state.toolResults,
		Status:         domain.MessageStatusComplete,
		CreatedAt:      streamStartAt,
	}
	if err := t.persistAssistantComplete(saveCtx, assistantMsg); err != nil {
		slog.ErrorContext(saveCtx, "chatturn: failed to save assistant message", "error", err)
	}
	if state.streamErrContent == "" && (state.assistantText.Len() > 0 || len(state.toolCalls) > 0) {
		t.fireAutoTitleIfPending(parentCtx, req.ConversationID, enriched.business.ID.String(), req.Message, state.assistantText.String())
	}
}
