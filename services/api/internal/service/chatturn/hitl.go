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

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// gateAction enumerates the outcomes of gateOnRequest.
// See docs/services/chatturn-hitl.md.
type gateAction int

const (
	// gateFresh — no active message, no resume header → start a new LLM turn.
	gateFresh gateAction = iota
	// gateRejoinResume — explicit resume header or implicit rejoin → call streamResume.
	gateRejoinResume
	// gateReemitApproval — pending batch + UI lost the approval card → reemitApprovalEvent.
	gateReemitApproval
	// gateHealStranded — active message whose approval batch already resolved (or
	// vanished) but whose resume never wrote back, OR a STALE in_progress fresh-turn
	// placeholder left by a crashed/dropped turn (older than streamBudget). Finalize
	// the stranded message, then proceed as a fresh turn instead of dead-ending every
	// later request with turn_already_in_progress. See docs/services/chatturn-hitl.md.
	gateHealStranded
	// gateRejectInProgress — a fresh (non-HITL) turn is still streaming: its
	// in_progress assistant placeholder is RECENT (younger than streamBudget) and
	// has no pending/resolving approval batch behind it. A second concurrent
	// POST /chat/{id} for the same conversation must be REJECTED (no second parallel
	// fresh turn) so the two turns cannot each build a history snapshot missing the
	// other, double-fire the titler, and tie on created_at. Distinct from
	// gateHealStranded: only a STALE placeholder (crashed turn) is healed-and-run;
	// a RECENT one is in flight and the conversation is busy.
	gateRejectInProgress
)

// sseEventError is the SSE event-type string for the inline-error path.
const sseEventError = "error"
const batchStateResolving = "resolving"

// gateOnRequest selects an action from the conversation's active message + pending batches.
// See docs/services/chatturn-hitl.md.
func (t *Turn) gateOnRequest(ctx context.Context, conversationID, headerBatchID string) (gateAction, *domain.Message, *domain.PendingToolCallBatch, string) {
	activeMsg, activeErr := t.deps.Messages.FindByConversationActive(ctx, conversationID)
	if activeErr != nil && !errors.Is(activeErr, domain.ErrMessageNotFound) {
		slog.WarnContext(ctx, "chatturn: FindByConversationActive failed, falling through",
			"error", activeErr, "conversation_id", conversationID)
		activeMsg = nil
	}

	if activeMsg != nil && headerBatchID == "" {
		batches, berr := t.deps.Pending.ListPendingByConversation(ctx, conversationID)
		if berr != nil {
			slog.WarnContext(ctx, "chatturn: ListPendingByConversation failed",
				"error", berr, "conversation_id", conversationID)
		}
		var resolving, pending *domain.PendingToolCallBatch
		for _, b := range batches {
			switch b.Status {
			case batchStateResolving, "resuming":
				// "resuming" is an in-flight resolve/resume just like "resolving":
				// a POST /chat/{id}/resume already claimed the batch and is
				// streaming the post-approval continuation (up to streamBudget).
				// Classifying it as in-flight routes a concurrent fresh turn to the
				// resume path, where AtomicTransitionResolvingToResuming rejects it
				// with OutcomeResumeInProgress. Without this arm a "resuming" batch
				// matches neither case, the pending_approval message is misread as
				// stranded, and the fresh turn force-completes it and runs a second
				// billed orchestrator stream. A genuinely-orphaned "resuming" batch
				// is still reclaimed by ReconcileOrphanResolving + the compensating
				// reset, exactly as an orphaned "resolving" batch is.
				if resolving == nil {
					resolving = b
				}
			case "pending":
				if pending == nil {
					pending = b
				}
			}
		}

		switch {
		case resolving != nil:
			return gateRejoinResume, activeMsg, resolving, resolving.ID
		case pending != nil:
			return gateReemitApproval, activeMsg, pending, ""
		case t.isRecentInProgress(activeMsg):
			// A fresh turn's in_progress placeholder that is still within the
			// stream budget: the first turn is in flight. Reject the second
			// concurrent fresh turn rather than running it in parallel.
			return gateRejectInProgress, activeMsg, nil, ""
		default:
			// A stale in_progress placeholder (crashed/dropped turn) or a
			// resolved-HITL message that never wrote back: heal and proceed so a
			// crashed turn never wedges the conversation forever.
			return gateHealStranded, activeMsg, nil, ""
		}
	}

	if activeMsg != nil && headerBatchID != "" {
		return gateRejoinResume, activeMsg, nil, headerBatchID
	}
	return gateFresh, nil, nil, ""
}

// isRecentInProgress reports whether msg is a fresh-turn in_progress placeholder
// still within the stream budget — i.e. a turn that is genuinely in flight, not a
// crashed one. Only in_progress messages qualify: a stranded pending_approval
// (resolved-HITL) message is always healed regardless of age, so the HITL
// self-heal path is unchanged. The reservation stamps CreatedAt at stream-open;
// a placeholder older than streamBudget belongs to a turn that can no longer be
// streaming (the orchestrator stream is itself bounded by streamBudget), so it is
// treated as stranded and healed. A zero/absent timestamp is treated as NOT
// recent so a malformed row self-heals rather than wedging the conversation.
func (t *Turn) isRecentInProgress(msg *domain.Message) bool {
	if msg == nil || msg.Status != domain.MessageStatusInProgress {
		return false
	}
	if msg.CreatedAt.IsZero() {
		return false
	}
	return time.Since(msg.CreatedAt) < streamBudget
}

// finalizeStranded marks an orphaned active message complete so a stranded
// conversation self-heals instead of dead-ending every later turn with
// turn_already_in_progress. Pending tool calls flip to approved — the resume
// executed them; only the write-back was lost. Best-effort: a failed Update is
// logged, not fatal (the fresh turn still proceeds). See docs/services/chatturn-hitl.md.
func (t *Turn) finalizeStranded(parentCtx context.Context, msg *domain.Message) {
	if msg == nil {
		return
	}
	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()

	healed := *msg
	healed.Status = domain.MessageStatusComplete
	healed.ToolCalls = make([]domain.ToolCall, len(msg.ToolCalls))
	copy(healed.ToolCalls, msg.ToolCalls)
	for i := range healed.ToolCalls {
		if healed.ToolCalls[i].Status == domain.ToolCallStatusPending {
			healed.ToolCalls[i].Status = domain.ToolCallStatusApproved
		}
	}
	if err := t.deps.Messages.Update(saveCtx, &healed); err != nil {
		slog.WarnContext(saveCtx, "chatturn: failed to finalize stranded message",
			"error", err, "message_id", msg.ID)
	}
	t.bumpLastMessageAtNow(saveCtx, healed.ConversationID)
}

// reemitApprovalEvent writes a tool_approval_required SSE event from the persisted batch (no orch hop).
// See docs/services/chatturn-hitl.md.
func (t *Turn) reemitApprovalEvent(w http.ResponseWriter, batch *domain.PendingToolCallBatch) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	calls := make([]sse.ApprovalCall, 0, len(batch.Calls))
	for _, call := range batch.Calls {
		calls = append(calls, sse.ApprovalCall{
			CallID:         call.CallID,
			ToolName:       call.ToolName,
			Args:           call.Arguments,
			EditableFields: []string{},
			Floor:          domain.ToolFloorManual,
		})
	}
	data, _ := sse.Marshal(sse.Event{
		Type:    "tool_approval_required",
		BatchID: batch.ID,
		Calls:   calls,
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// reasonNoActiveApproval is the uniform inline-error payload for every resume
// path that cannot proceed (no batch, no active message, or a cross-tenant
// ownership mismatch). Keeping it uniform avoids leaking why the resume failed.
const reasonNoActiveApproval = "no_active_approval_for_conversation"

// sseInlineError writes a single error SSE event and closes the stream.
func (t *Turn) sseInlineError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	data, _ := sse.Marshal(sse.Event{Type: sseEventError, Content: reasonNoActiveApproval})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// streamResume is the rejoin path: a new chat request arrives while a batch is
// still resolving, so we re-attach to the orchestrator resume stream and
// finalize the assistant Message. It re-supplies the active-platform /
// whitelist context (but not fresh approvals — the orchestrator falls back to
// base floors) so the post-approval LLM continuation keeps offering the
// business's platform tools instead of dropping every {platform}__action call.
// See docs/services/chatturn-hitl.md.
func (t *Turn) streamResume(
	ctx context.Context,
	w http.ResponseWriter,
	conversationID string,
	activeMsg *domain.Message,
	batchID string,
) (TurnOutcome, error) {
	batch, err := t.deps.Pending.GetByBatchID(ctx, batchID)
	if err != nil || batch == nil || batch.ConversationID != conversationID {
		t.sseInlineError(w)
		return OutcomeInlineError, nil
	}
	body := t.resumeBody(ctx, batch.BusinessID, batch.ProjectID)
	return t.runResumeStream(ctx, w, conversationID, activeMsg, batch.BusinessID, batch.UserID, batchID, body)
}

// resumeBody assembles the rejoin resume request body. It mirrors the
// approve-path body in the HITL handler: active_integrations from the
// business's active platforms and whitelist_mode / allowed_tools from the
// conversation's project, so the orchestrator rebuilds AvailableTools with the
// platform tools the post-approval continuation may emit. Any lookup failure
// degrades that field to empty rather than failing the rejoin. Returns nil when
// businessID is unparseable, preserving the prior nil-body behavior.
func (t *Turn) resumeBody(ctx context.Context, businessID, projectID string) []byte {
	bizUUID, err := uuid.Parse(businessID)
	if err != nil {
		return nil
	}

	active := make([]string, 0)
	integrations, listErr := t.deps.Integrations.ListByBusinessID(ctx, bizUUID)
	if listErr != nil {
		slog.WarnContext(ctx, "chatturn: resume: failed to list integrations for active-platform body", "error", listErr)
	} else {
		seen := make(map[string]bool, len(integrations))
		for _, integ := range integrations {
			if integ.Status == domain.IntegrationStatusActive && !seen[integ.Platform] {
				active = append(active, integ.Platform)
				seen[integ.Platform] = true
			}
		}
	}

	var whitelistMode string
	var allowedTools []string
	if projectID != "" {
		if projUUID, perr := uuid.Parse(projectID); perr == nil {
			if proj, perr := t.deps.Projects.GetByID(ctx, bizUUID, projUUID); perr == nil && proj != nil {
				whitelistMode = string(proj.WhitelistMode)
				allowedTools = proj.AllowedTools
			}
		}
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"active_integrations": active,
		"whitelist_mode":      whitelistMode,
		"allowed_tools":       allowedTools,
	})
	return raw
}

// ResumeApproved is the primary approve→resume path (POST /chat/{id}/resume).
// It loads the active assistant message and finalizes it after the orchestrator
// executes the approved tools. The bare SSE proxy this replaces streamed the
// result to the browser but never wrote it back, leaving the message stranded
// at pending_approval (no result) and bricking the conversation with
// turn_already_in_progress on the next message. body carries
// business_approvals + project_approval_overrides for the orchestrator's
// resume-time policy re-check. See docs/services/chatturn-hitl.md.
func (t *Turn) ResumeApproved(
	ctx context.Context,
	w http.ResponseWriter,
	conversationID string,
	batchID string,
	body []byte,
) (TurnOutcome, error) {
	batch, err := t.deps.Pending.GetByBatchID(ctx, batchID)
	if err != nil || batch == nil || batch.ConversationID != conversationID {
		t.sseInlineError(w)
		return OutcomeInlineError, nil
	}
	if !t.ownsConversation(ctx, conversationID, batch.UserID, batch.BusinessID) {
		t.sseInlineError(w)
		return OutcomeConversationNotFound, nil
	}
	activeMsg, ferr := t.deps.Messages.FindByConversationActive(ctx, conversationID)
	if ferr != nil || activeMsg == nil {
		slog.WarnContext(ctx, "chatturn: ResumeApproved: no active message to finalize",
			"error", ferr, "conversation_id", conversationID, "batch_id", batchID)
		t.sseInlineError(w)
		return OutcomeInlineError, nil
	}
	return t.runResumeStream(ctx, w, conversationID, activeMsg, batch.BusinessID, batch.UserID, batchID, body)
}

// runResumeStream is the shared resume core for both the rejoin (streamResume)
// and approve (ResumeApproved) entry points: it proxies the orchestrator
// resume SSE while accumulating tool results onto activeMsg via OnEvent, then
// persists the finalized message on the terminal event (or via the post-stream
// fallback). See docs/services/chatturn-hitl.md.
// resumeToolArgs returns the arguments of the tool_call matching toolCallID
// during a resume stream, so onToolResult can scope a token_expired flip to the
// failing integration's external_id. The approved (paused) tool's call lives in
// the persisted message's ToolCalls, indexed by callIdx; tools emitted fresh on
// resume live in recCalls. Returns nil when neither carries the id, in which
// case the flip falls back to platform-wide scoping.
func resumeToolArgs(toolCallID string, callIdx map[string]int, persisted, fresh []domain.ToolCall) map[string]interface{} {
	if toolCallID == "" {
		return nil
	}
	if idx, ok := callIdx[toolCallID]; ok && idx >= 0 && idx < len(persisted) {
		return persisted[idx].Arguments
	}
	for i := range fresh {
		if fresh[i].ID == toolCallID {
			return fresh[i].Arguments
		}
	}
	return nil
}

// resumeStreamHeaders forwards the request locale to the orchestrator's resume
// endpoint as an Accept-Language header. The resume body has no locale field
// (unlike the fresh-chat body), so without this the orchestrator's
// LocaleMiddleware resolves an empty header to the default tag (RU) and rebuilds
// the post-approval tool definitions in RU even for an EN tenant. ctx carries
// the tag set by the API's LocaleMiddleware.
func resumeStreamHeaders(ctx context.Context) map[string]string {
	return map[string]string{"Accept-Language": i18n.LocaleFromContext(ctx).String()}
}

// resumeApprovalID reconstructs the HITL dedupe key the orchestrator ran an
// approved resume dispatch under — "<batch_id>-<call_id>", the same value it
// forms in orchestrator/resume.go and the agent keys its (business_id,
// approval_id) dedupe on. Persisting it onto the produced task / review lets a
// later retry re-send the identical key so an already-landed call is deduped
// instead of double-posted. Empty when the call carried no id (nothing to key).
func resumeApprovalID(batchID, callID string) string {
	if callID == "" {
		return ""
	}
	return fmt.Sprintf("%s-%s", batchID, callID)
}

func (t *Turn) runResumeStream(
	ctx context.Context,
	w http.ResponseWriter,
	conversationID string,
	activeMsg *domain.Message,
	businessID string,
	actorUserID string,
	batchID string,
	body []byte,
) (TurnOutcome, error) {
	if _, claimErr := t.deps.Pending.AtomicTransitionResolvingToResuming(ctx, batchID); claimErr != nil {
		if errors.Is(claimErr, domain.ErrBatchNotResolving) {
			return OutcomeResumeInProgress, nil
		}
		if errors.Is(claimErr, domain.ErrBatchNotFound) {
			t.sseInlineError(w)
			return OutcomeInlineError, nil
		}
		slog.ErrorContext(ctx, "chatturn: resume: failed to claim batch for resume",
			"error", claimErr, "batch_id", batchID, "conversation_id", conversationID)
		t.sseInlineError(w)
		return OutcomeInlineError, nil
	}

	msg := *activeMsg
	var postText strings.Builder
	postText.WriteString(msg.Content)

	callIdx := make(map[string]int, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		callIdx[tc.ID] = i
	}
	resultIdx := make(map[string]int, len(msg.ToolResults))
	for i, tr := range msg.ToolResults {
		resultIdx[tr.ToolCallID] = i
	}
	recResultIdx := make(map[string]int)
	freshCallIDs := make(map[string]struct{})

	taskOpsCtx, cancelTaskOps := context.WithTimeout(context.Background(), streamBudget)
	if corrID := logger.CorrelationIDFromContext(ctx); corrID != "" {
		taskOpsCtx = logger.WithCorrelationID(taskOpsCtx, corrID)
	}
	defer cancelTaskOps()

	idMap := make(map[string]string)
	var recCalls []domain.ToolCall
	var recResults []domain.ToolResult

	var terminated, fireAutoTitle bool
	var rePause *sse.Event

	streamErr := t.deps.Orch.StreamSSE(ctx, orchestratorclient.StreamSSERequest{
		ConversationID: conversationID,
		BatchID:        batchID,
		Body:           body,
		Headers:        resumeStreamHeaders(ctx),
		Writer:         w,
		OrchCtxBudget:  streamBudget,
		OnEvent: func(ev sse.Event) {
			if terminated {
				return
			}
			switch ev.Type {
			case "text":
				postText.WriteString(ev.Content)
			case "tool_call":
				approvalID := resumeApprovalID(batchID, ev.ToolCallID)
				recCalls = append(recCalls, domain.ToolCall{
					ID:         ev.ToolCallID,
					Name:       ev.ToolName,
					Arguments:  ev.ToolArgs,
					ApprovalID: approvalID,
				})
				if ev.ToolCallID != "" {
					freshCallIDs[ev.ToolCallID] = struct{}{}
					if _, known := callIdx[ev.ToolCallID]; !known {
						callIdx[ev.ToolCallID] = len(msg.ToolCalls)
						msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
							ID:         ev.ToolCallID,
							Name:       ev.ToolName,
							Arguments:  ev.ToolArgs,
							ApprovalID: approvalID,
							Status:     domain.ToolCallStatusApproved,
						})
					}
				}
				t.onToolCall(taskOpsCtx, businessID, ev.ToolCallID, ev.ToolName, ev.ToolDisplayName, ev.ToolDisplayNameKey, ev.ToolArgs, approvalID, idMap)
			case "tool_approval_required":
				evCopy := ev
				rePause = &evCopy
			case "tool_result":
				var content map[string]interface{}
				if m, ok := ev.ToolResult.(map[string]interface{}); ok {
					content = m
				} else {
					content = map[string]interface{}{"raw": ev.ToolResult}
				}
				persistedIdx, hasPersisted := callIdx[ev.ToolCallID]
				_, hasFresh := freshCallIDs[ev.ToolCallID]
				if ev.ToolCallID == "" || (!hasPersisted && !hasFresh) {
					slog.WarnContext(taskOpsCtx, "chatturn: resume: dropping tool_result with no matching tool_call",
						"tool_call_id", ev.ToolCallID, "conversation_id", conversationID)
					break
				}
				tr := domain.ToolResult{
					ToolCallID: ev.ToolCallID,
					Content:    content,
					IsError:    ev.ToolError != "",
					Code:       ev.Code,
				}
				if existing, dup := resultIdx[ev.ToolCallID]; dup {
					msg.ToolResults[existing] = tr
				} else {
					resultIdx[ev.ToolCallID] = len(msg.ToolResults)
					msg.ToolResults = append(msg.ToolResults, tr)
				}
				if existing, dup := recResultIdx[ev.ToolCallID]; dup {
					recResults[existing] = tr
				} else {
					recResultIdx[ev.ToolCallID] = len(recResults)
					recResults = append(recResults, tr)
				}
				if hasPersisted {
					if ev.ToolError != "" {
						msg.ToolCalls[persistedIdx].Status = domain.ToolCallStatusRejected
					} else {
						msg.ToolCalls[persistedIdx].Status = domain.ToolCallStatusApproved
					}
				}
				toolArgs := resumeToolArgs(ev.ToolCallID, callIdx, msg.ToolCalls, recCalls)
				t.onToolResult(taskOpsCtx, businessID, ev.ToolCallID, content, toolArgs, ev.ToolError, ev.Code, idMap)
			case "tool_rejected":
				if idx, ok := callIdx[ev.ToolCallID]; ok {
					msg.ToolCalls[idx].Status = domain.ToolCallStatusRejected
				}
			case sseEventError:
				msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
				t.persistResumeDone(ctx, &msg, businessID, actorUserID, recCalls, recResults)
				terminated = true
			case "done":
				msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
				t.persistResumeDone(ctx, &msg, businessID, actorUserID, recCalls, recResults)
				fireAutoTitle = true
				terminated = true
			}
		},
	})

	if streamErr != nil && !terminated && strings.Contains(streamErr.Error(), "stream resume:") {
		slog.ErrorContext(ctx, "chatturn: orchestrator resume request failed", "error", streamErr)
		t.compensateResuming(ctx, batchID)
		return OutcomeOrchestratorUnavailable, fmt.Errorf("chatturn: orchestrator resume: %w", streamErr)
	}
	if streamErr != nil {
		slog.WarnContext(ctx, "chatturn: resume stream ended with error",
			"error", streamErr, "conversation_id", conversationID)
	}

	if terminated {
		if fireAutoTitle {
			t.fireAutoTitleIfPendingResume(ctx, conversationID, &msg)
		}
		return OutcomeRejoinedResume, nil
	}

	if rePause != nil {
		return t.persistResumeRePause(ctx, &msg, businessID, actorUserID, postText.String(), rePause, recCalls, recResults), nil
	}

	msg.Content = postText.String()
	if msg.Status == domain.MessageStatusPendingApproval || msg.Status == domain.MessageStatusInProgress {
		msg.Status = domain.MessageStatusComplete
	}
	saveCtx, cancel := t.persistContext(ctx)
	defer cancel()
	if err := t.deps.Messages.Update(saveCtx, &msg); err != nil {
		metrics.ResumePersistFailure("partial")
		slog.ErrorContext(saveCtx, "chatturn: resume: failed to persist partial message (self-heal will retry on next request)",
			"error", err, "message_id", msg.ID, "conversation_id", conversationID, "status", msg.Status)
	}
	t.bumpLastMessageAtNow(saveCtx, conversationID)
	if t.deps.Posts != nil || t.deps.Reviews != nil {
		t.recordPostsAndReviews(saveCtx, businessID, msg.ID, recCalls, recResults)
	}
	t.auditRPAMutations(saveCtx, businessID, actorUserID, recCalls, recResults)
	t.auditPlatformMutations(saveCtx, businessID, actorUserID, recCalls, recResults)
	return OutcomeRejoinedResume, nil
}

// resumeCompensationTimeout bounds the compensating reset issued when the
// resume stream fails to open after the resolving→resuming claim. It runs on a
// fresh context so a canceled request ctx cannot skip the compensation.
const resumeCompensationTimeout = 5 * time.Second

// compensateResuming rolls a batch back resuming→resolving after the resume
// stream failed to open (orchestrator unavailable at resume time). Without it
// the batch is permanently stranded at "resuming": the approved tool never
// dispatched, a retried /resume can never re-win the resolving→resuming claim,
// and the approved publish/reply is silently dropped. It runs detached from the
// request context — the failure may itself be a request-context deadline — and
// is best-effort: a residual orphan is healed at startup by
// ReconcileOrphanResolving.
func (t *Turn) compensateResuming(parentCtx context.Context, batchID string) {
	ctx, cancel := context.WithTimeout(context.Background(), resumeCompensationTimeout)
	defer cancel()
	if corrID := logger.CorrelationIDFromContext(parentCtx); corrID != "" {
		ctx = logger.WithCorrelationID(ctx, corrID)
	}
	if err := t.deps.Pending.AtomicTransitionResumingToResolving(ctx, batchID); err != nil {
		slog.ErrorContext(ctx, "chatturn: compensating reset of resuming batch failed",
			"batch_id", batchID, "error", err)
	}
}

// persistResumeRePause keeps the assistant message ACTIVE when the resume loop
// paused again on the next manual-floor tool in a sequential fan-out (model
// emits one tool call per turn, so Yandex→Telegram→VK each pause separately).
// The orchestrator already persisted the next batch; we mirror persistAfterStream
// by appending its calls as pending and writing Status=PendingApproval. Marking
// the message Complete here (the pre-fix fallback did) stranded the chain at the
// SECOND tool: the next approve's FindByConversationActive found nothing and
// failed with no_active_approval_for_conversation. See docs/services/chatturn-hitl.md.
//
// The tool that executed in THIS resume stream (the one whose result drove the
// loop to the next pause) lives in recCalls/recResults, so its Post / RPA-audit
// records are written here — exactly as persistResumeDone does on a terminal
// event. Without this, every approved post except the final one (which ends on
// 'done') is silently dropped from the feed and its RPA mutation goes unaudited.
// Records are scoped to recResults, which carry only THIS stream's executed
// tools, so each tool is recorded exactly once (on whichever stream it
// completes in); the just-re-paused tool has no result yet and is not recorded.
func (t *Turn) persistResumeRePause(parentCtx context.Context, msg *domain.Message, businessID, actorUserID, content string, pause *sse.Event, toolCalls []domain.ToolCall, toolResults []domain.ToolResult) TurnOutcome {
	msg.Content = content
	existing := make(map[string]struct{}, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		existing[tc.ID] = struct{}{}
	}
	for _, call := range pause.Calls {
		if _, dup := existing[call.CallID]; dup {
			continue
		}
		msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
			ID:         call.CallID,
			Name:       call.ToolName,
			Arguments:  call.Args,
			ApprovalID: fmt.Sprintf("%s-%s", pause.BatchID, call.CallID),
			Status:     domain.ToolCallStatusPending,
		})
	}
	msg.Status = domain.MessageStatusPendingApproval

	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	if err := t.deps.Messages.Update(saveCtx, msg); err != nil {
		metrics.ResumePersistFailure("repause")
		slog.ErrorContext(saveCtx, "chatturn: resume: failed to persist re-paused message (message stays active; next approve/heal will recover)",
			"error", err, "message_id", msg.ID, "conversation_id", msg.ConversationID)
	}
	t.bumpLastMessageAtNow(saveCtx, msg.ConversationID)
	if t.deps.Posts != nil || t.deps.Reviews != nil {
		t.recordPostsAndReviews(saveCtx, businessID, msg.ID, toolCalls, toolResults)
	}
	t.auditRPAMutations(saveCtx, businessID, actorUserID, toolCalls, toolResults)
	t.auditPlatformMutations(saveCtx, businessID, actorUserID, toolCalls, toolResults)
	return OutcomePauseHITL
}

// persistResumeDone writes the assistant message at resume terminal events and
// records the posts/reviews produced by the approved tools — the resume path is
// where manual-floor publishing tools actually execute, so this is the only
// place those records can be created. Uses persistContext (NOT request ctx —
// that ctx is canceled when the SSE stream closes).
func (t *Turn) persistResumeDone(parentCtx context.Context, msg *domain.Message, businessID, actorUserID string, toolCalls []domain.ToolCall, toolResults []domain.ToolResult) {
	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	if err := t.deps.Messages.Update(saveCtx, msg); err != nil {
		metrics.ResumePersistFailure("done")
		slog.ErrorContext(saveCtx, "chatturn: resume: failed to persist completed message (self-heal will retry on next request)",
			"error", err, "message_id", msg.ID, "conversation_id", msg.ConversationID, "status", msg.Status)
	}
	t.bumpLastMessageAtNow(saveCtx, msg.ConversationID)
	if t.deps.Posts != nil || t.deps.Reviews != nil {
		t.recordPostsAndReviews(saveCtx, businessID, msg.ID, toolCalls, toolResults)
	}
	t.auditRPAMutations(saveCtx, businessID, actorUserID, toolCalls, toolResults)
	t.auditPlatformMutations(saveCtx, businessID, actorUserID, toolCalls, toolResults)
}
