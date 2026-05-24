package chatturn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// gateAction enumerates the four outcomes of gateOnRequest. The tri-case
// implicit-resume + explicit-resume contract is locked — the four constants
// must NOT collapse to two.
type gateAction int

const (
	// gateFresh — no active message, no resume header → start a new LLM turn.
	gateFresh gateAction = iota
	// gateRejoinResume — explicit resume header set, OR implicit rejoin
	// (active msg + resolving batch) → call streamResume.
	gateRejoinResume
	// gateReemitApproval — pending batch but UI lost the approval card → call
	// reemitApprovalEvent (no orchestrator roundtrip).
	gateReemitApproval
	// gateInlineError — orphan in_progress message with no active batch →
	// call sseInlineError.
	gateInlineError
)

// sseEventError is the SSE event-type string used by both the orchestrator
// and the inline-error path.
const sseEventError = "error"

// gateOnRequest inspects the conversation's active message + pending batches
// and returns one of four actions. headerBatchID is the resolved batch id
// from X-Onevoice-Resume-Batch-Id (or ?batch_id query) — empty means the
// client did not request explicit resume.
//
// Returns:
//   - action — what Run should do next
//   - activeMsg — the current pending_approval/in_progress Message, or nil
//   - batch — the pending batch for reemitApprovalEvent (nil otherwise)
//   - batchID — resolved batch ID for the resume call (empty for fresh)
//   - err — only when ListPendingByConversation hard-fails; soft-errors
//     fall through (legacy chat_proxy.go behavior preserved)
func (t *Turn) gateOnRequest(ctx context.Context, conversationID, headerBatchID string) (gateAction, *domain.Message, *domain.PendingToolCallBatch, string, error) {
	activeMsg, activeErr := t.deps.Messages.FindByConversationActive(ctx, conversationID)
	if activeErr != nil && !errors.Is(activeErr, domain.ErrMessageNotFound) {
		slog.WarnContext(ctx, "chatturn: FindByConversationActive failed, falling through",
			"error", activeErr, "conversation_id", conversationID)
		activeMsg = nil
	}

	// Implicit-resume branch: no header, but an active message exists. Look up
	// the conversation's active batches and apply the tri-case.
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
			// orchestrator is mid-dispatch; rejoin keyed on this batch.ID;
			// reuse activeMsg.ID so tool_result events extend the same Message.
			return gateRejoinResume, activeMsg, resolving, resolving.ID, nil
		case pending != nil:
			// approval card dropped off the client but the batch is still
			// pending → re-emit and close.
			return gateReemitApproval, activeMsg, pending, "", nil
		default:
			// orphan in_progress Message with no active batch.
			return gateInlineError, activeMsg, nil, "", nil
		}
	}

	// Explicit-resume branch: header AND active message → forward to resume.
	if activeMsg != nil && headerBatchID != "" {
		return gateRejoinResume, activeMsg, nil, headerBatchID, nil
	}
	return gateFresh, nil, nil, "", nil
}

// reemitApprovalEvent writes a tool_approval_required SSE event built from
// the persisted PendingToolCallBatch. Used by the implicit-resume gate
// when the client reopens the chat mid-approval (network flap, page reload)
// and the batch is still in status="pending". No orchestrator roundtrip.
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

// sseInlineError writes a single {"type":"error","content":reason} SSE event
// and closes the stream. Used by the gate's orphan-in-progress case and by
// the resume guard when the batch is missing or mis-scoped.
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

// streamResume proxies to the orchestrator's resume endpoint and folds
// tool_result events into the existing assistant Message. On done,
// transitions Message.Status from pending_approval/in_progress to complete.
//
// Returns OutcomeOrchestratorUnavailable when the resume request fails
// before any SSE byte is written — the handler maps this to 502 Bad Gateway.
// All other paths write SSE bytes and return OutcomeRejoinedResume.
func (t *Turn) streamResume(
	ctx context.Context,
	w http.ResponseWriter,
	conversationID string,
	activeMsg *domain.Message,
	batchID string,
) (TurnOutcome, error) {
	// Validate the batch exists for this conversation before proxying.
	batch, err := t.deps.Pending.GetByBatchID(ctx, batchID)
	if err != nil || batch == nil || batch.ConversationID != conversationID {
		t.sseInlineError(w, "no_active_approval_for_conversation")
		return OutcomeInlineError, nil
	}

	// Detach the orchestrator request from the client's ctx so a client-side
	// reconnect cannot cancel the in-flight resume mid-LLM-call.
	corrID := logger.CorrelationIDFromContext(ctx)
	orchCtx, orchCancel := context.WithTimeout(context.Background(), streamBudget)
	if corrID != "" {
		orchCtx = logger.WithCorrelationID(orchCtx, corrID)
	}
	defer orchCancel()

	headers := map[string]string{}
	if corrID != "" {
		headers["X-Correlation-ID"] = corrID
	}
	resp, err := t.deps.Orch.StreamResume(orchCtx, conversationID, batchID, nil, headers)
	if err != nil {
		slog.ErrorContext(ctx, "chatturn: orchestrator resume request failed", "error", err)
		return OutcomeOrchestratorUnavailable, fmt.Errorf("chatturn: orchestrator resume: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return OutcomeError, fmt.Errorf("chatturn: streaming not supported")
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, sseBufferBytes), sseBufferBytes)

	// Work on a local copy so we flush the full final state in one Update.
	// msg.Content is intentionally not cleared — we preserve the Content
	// builder semantics and start from whatever was persisted at pause time.
	msg := *activeMsg
	var postText strings.Builder
	postText.WriteString(msg.Content)

	// Index existing tool calls by call_id so we can update Status on result.
	callIdx := make(map[string]int, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		callIdx[tc.ID] = i
	}

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		ev, err := sse.Unmarshal([]byte(line[6:]))
		if err != nil {
			continue
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
			// Resume failed mid-stream (LLM error, ctx cancel, max-iterations
			// cap). The error event is already forwarded to the client; here
			// we MUST transition the assistant Message off pending_approval/
			// in_progress, otherwise every subsequent POST /chat hits the
			// gate's "turn_already_in_progress" branch and the conversation
			// is permanently stuck.
			msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
			t.persistResumeDone(ctx, &msg)
			return OutcomeRejoinedResume, nil
		case "done":
			msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
			t.persistResumeDone(ctx, &msg)
			t.fireAutoTitleIfPendingResume(ctx, conversationID, &msg)
			return OutcomeRejoinedResume, nil
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(ctx, "chatturn: resume scanner error",
			"error", err, "conversation_id", conversationID)
	}

	// Stream ended without EventDone — transient network drop, orchestrator
	// closed the connection after an unhandled event, or any other non-terminal
	// exit. Transition the message off pending_approval/in_progress here;
	// leaving it active permanently bricks the conversation.
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

// persistResumeDone writes the assistant message at resume-path "done". Runs
// on a fresh persistContext (NOT the request ctx) because the request ctx is
// canceled when the SSE stream closes.
func (t *Turn) persistResumeDone(parentCtx context.Context, msg *domain.Message) {
	saveCtx, cancel := t.persistContext(parentCtx)
	defer cancel()
	if err := t.deps.Messages.Update(saveCtx, msg); err != nil {
		slog.WarnContext(saveCtx, "chatturn: resume: failed to persist completed message",
			"error", err, "message_id", msg.ID)
	}
}
