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
	// gateInlineError — orphan in_progress message with no active batch → sseInlineError.
	gateInlineError
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

	// Implicit-resume branch: no header but active message → tri-case over active batches.
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
			// Reuse activeMsg.ID so tool_result events extend the same Message row.
			return gateRejoinResume, activeMsg, resolving, resolving.ID
		case pending != nil:
			return gateReemitApproval, activeMsg, pending, ""
		default:
			return gateInlineError, activeMsg, nil, ""
		}
	}

	if activeMsg != nil && headerBatchID != "" {
		return gateRejoinResume, activeMsg, nil, headerBatchID
	}
	return gateFresh, nil, nil, ""
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

// streamResume proxies the orchestrator resume SSE and finalizes the assistant Message.
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
		t.sseInlineError(w, "no_active_approval_for_conversation")
		return OutcomeInlineError, nil
	}

	// Local copy so we flush full final state in one Update; preserve Content from pause-time.
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
		Body:           nil,
		Writer:         w,
		OrchCtxBudget:  streamBudget,
		OnEvent: func(ev sse.Event) {
			if terminated {
				// Defensive: ignore frames after terminal done/error → persist decision stays idempotent.
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
				// MUST flip off pending_approval/in_progress here, else the conversation is bricked.
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

	// Connect failure: no bytes written to w yet → handler may emit a 502 JSON body.
	if streamErr != nil && !terminated && strings.Contains(streamErr.Error(), "stream resume:") {
		slog.ErrorContext(ctx, "chatturn: orchestrator resume request failed", "error", streamErr)
		return OutcomeOrchestratorUnavailable, fmt.Errorf("chatturn: orchestrator resume: %w", streamErr)
	}
	// Mid-drain failure: response is committed; log and fall through to partial-persist.
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

	// Non-terminal exit (transient drop, unhandled event): force Complete to avoid bricking the conversation.
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
