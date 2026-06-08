package chatturn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// gateAction enumerates the four outcomes of gateOnRequest.
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
	// vanished) but whose resume never wrote back. Finalize the stranded message,
	// then proceed as a fresh turn instead of dead-ending every later request with
	// turn_already_in_progress. See docs/services/chatturn-hitl.md.
	gateHealStranded
)

// sseEventError is the SSE event-type string for the inline-error path.
const sseEventError = "error"

// gateOnRequest selects one of four actions from the conversation's active message + pending batches.
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
			case "resolving":
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
		default:
			return gateHealStranded, activeMsg, nil, ""
		}
	}

	if activeMsg != nil && headerBatchID != "" {
		return gateRejoinResume, activeMsg, nil, headerBatchID
	}
	return gateFresh, nil, nil, ""
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

// sseInlineError writes a single error SSE event and closes the stream.
func (t *Turn) sseInlineError(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	data, _ := sse.Marshal(sse.Event{Type: sseEventError, Content: reason})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// streamResume is the rejoin path: a new chat request arrives while a batch is
// still resolving, so we re-attach to the orchestrator resume stream and
// finalize the assistant Message. Body is nil — the rejoin does not re-supply
// approvals (the orchestrator falls back to base floors). See docs/services/chatturn-hitl.md.
func (t *Turn) streamResume(
	ctx context.Context,
	w http.ResponseWriter,
	conversationID string,
	activeMsg *domain.Message,
	batchID string,
) (TurnOutcome, error) {
	batch, err := t.deps.Pending.GetByBatchID(ctx, batchID)
	if err != nil || batch == nil || batch.ConversationID != conversationID {
		t.sseInlineError(w, "no_active_approval_for_conversation")
		return OutcomeInlineError, nil
	}
	return t.runResumeStream(ctx, w, conversationID, activeMsg, batchID, nil)
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
		t.sseInlineError(w, "no_active_approval_for_conversation")
		return OutcomeInlineError, nil
	}
	activeMsg, ferr := t.deps.Messages.FindByConversationActive(ctx, conversationID)
	if ferr != nil || activeMsg == nil {
		slog.WarnContext(ctx, "chatturn: ResumeApproved: no active message to finalize",
			"error", ferr, "conversation_id", conversationID, "batch_id", batchID)
		t.sseInlineError(w, "no_active_approval_for_conversation")
		return OutcomeInlineError, nil
	}
	return t.runResumeStream(ctx, w, conversationID, activeMsg, batchID, body)
}

// runResumeStream is the shared resume core for both the rejoin (streamResume)
// and approve (ResumeApproved) entry points: it proxies the orchestrator
// resume SSE while accumulating tool results onto activeMsg via OnEvent, then
// persists the finalized message on the terminal event (or via the post-stream
// fallback). See docs/services/chatturn-hitl.md.
func (t *Turn) runResumeStream(
	ctx context.Context,
	w http.ResponseWriter,
	conversationID string,
	activeMsg *domain.Message,
	batchID string,
	body []byte,
) (TurnOutcome, error) {
	msg := *activeMsg
	var postText strings.Builder
	postText.WriteString(msg.Content)

	callIdx := make(map[string]int, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		callIdx[tc.ID] = i
	}

	var terminated, fireAutoTitle bool

	streamErr := t.deps.Orch.StreamSSE(ctx, orchestratorclient.StreamSSERequest{
		ConversationID: conversationID,
		BatchID:        batchID,
		Body:           body,
		Writer:         w,
		OrchCtxBudget:  streamBudget,
		OnEvent: func(ev sse.Event) {
			if terminated {
				return
			}
			switch ev.Type {
			case "text":
				postText.WriteString(ev.Content)
			case "tool_result":
				var content map[string]interface{}
				if m, ok := ev.ToolResult.(map[string]interface{}); ok {
					content = m
				} else {
					content = map[string]interface{}{"raw": ev.ToolResult}
				}
				msg.ToolResults = append(msg.ToolResults, domain.ToolResult{
					ToolCallID: ev.ToolCallID,
					Content:    content,
					IsError:    ev.ToolError != "",
					Code:       ev.Code,
				})
				if idx, ok := callIdx[ev.ToolCallID]; ok {
					if ev.ToolError != "" {
						msg.ToolCalls[idx].Status = domain.ToolCallStatusRejected
					} else {
						msg.ToolCalls[idx].Status = domain.ToolCallStatusApproved
					}
				}
			case "tool_rejected":
				if idx, ok := callIdx[ev.ToolCallID]; ok {
					msg.ToolCalls[idx].Status = domain.ToolCallStatusRejected
				}
			case sseEventError:
				msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
				t.persistResumeDone(ctx, &msg)
				terminated = true
			case "done":
				msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
				t.persistResumeDone(ctx, &msg)
				fireAutoTitle = true
				terminated = true
			}
		},
	})

	if streamErr != nil && !terminated && strings.Contains(streamErr.Error(), "stream resume:") {
		slog.ErrorContext(ctx, "chatturn: orchestrator resume request failed", "error", streamErr)
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

	msg.Content = postText.String()
	if msg.Status == domain.MessageStatusPendingApproval || msg.Status == domain.MessageStatusInProgress {
		msg.Status = domain.MessageStatusComplete
	}
	saveCtx, cancel := t.persistContext(ctx)
	defer cancel()
	if err := t.deps.Messages.Update(saveCtx, &msg); err != nil {
		slog.WarnContext(saveCtx, "chatturn: resume: failed to persist partial message",
			"error", err, "message_id", msg.ID)
	}
	return OutcomeRejoinedResume, nil
}

// persistResumeDone writes the assistant message at resume terminal events.
// Uses persistContext (NOT request ctx — that ctx is canceled when the SSE stream closes).
func (t *Turn) persistResumeDone(parentCtx context.Context, msg *domain.Message) {
	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	if err := t.deps.Messages.Update(saveCtx, msg); err != nil {
		slog.WarnContext(saveCtx, "chatturn: resume: failed to persist completed message",
			"error", err, "message_id", msg.ID)
	}
}
