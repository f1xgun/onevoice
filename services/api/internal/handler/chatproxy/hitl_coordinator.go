package chatproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// GateAction enumerates the four outcomes of HITLCoordinator.GateOnRequest.
// The tri-case implicit-resume + explicit-resume contract is locked in
// 16-CONTEXT.md (D-04) — the four constants must NOT collapse to two.
type GateAction int

const (
	// GateActionFresh — no active message, no resume header → start a new
	// LLM turn (entry handler proceeds with body parse + Enrich).
	GateActionFresh GateAction = iota
	// GateActionRejoinResume — explicit resume header set, OR implicit
	// rejoin (active msg + resolving batch) → entry handler calls
	// StreamResume.
	GateActionRejoinResume
	// GateActionReemitApproval — D-04 case (c): pending batch but UI lost
	// the approval card → entry handler calls ReemitApprovalEvent (no
	// orchestrator roundtrip).
	GateActionReemitApproval
	// GateActionInlineError — D-04 case (d): orphan in_progress message
	// with no active batch → entry handler calls SSEInlineError.
	GateActionInlineError
)

// HITLCoordinator owns the D-04 stream-open gate, the resume SSE proxy, and
// the synthetic SSE writers (reemit / inline-error). Mirrors the chat_proxy.go
// pause/resume seam verbatim — only the receiver type changes.
type HITLCoordinator struct {
	pending   domain.PendingToolCallRepository
	msgs      domain.MessageRepository
	persister *MessagePersister
	orch      *orchestratorclient.Client
}

// NewHITLCoordinator constructs a HITLCoordinator. All deps are required;
// nil triggers a panic at construction time (wiring-time invariant).
func NewHITLCoordinator(
	pending domain.PendingToolCallRepository,
	msgs domain.MessageRepository,
	persister *MessagePersister,
	orch *orchestratorclient.Client,
) *HITLCoordinator {
	if pending == nil {
		panic("chatproxy.NewHITLCoordinator: pending cannot be nil")
	}
	if msgs == nil {
		panic("chatproxy.NewHITLCoordinator: msgs cannot be nil")
	}
	if persister == nil {
		panic("chatproxy.NewHITLCoordinator: persister cannot be nil")
	}
	if orch == nil {
		panic("chatproxy.NewHITLCoordinator: orch cannot be nil")
	}
	return &HITLCoordinator{
		pending:   pending,
		msgs:      msgs,
		persister: persister,
		orch:      orch,
	}
}

// GateOnRequest inspects the conversation's active message + pending batches
// and returns one of four actions. headerBatchID is the resolved batch id
// from X-Onevoice-Resume-Batch-Id (or ?batch_id query) — empty means the
// client did not request explicit resume.
//
// Return values:
//   - action — what the entry handler should do next
//   - activeMsg — the current pending_approval/in_progress Message, or nil
//   - batch — the pending batch for ReemitApprovalEvent (nil otherwise)
//   - batchID — resolved batch ID for the resume call (empty for fresh)
//   - err — only when ListPendingByConversation hard-fails; soft-errors fall
//     through (legacy chat_proxy.go behavior preserved)
func (c *HITLCoordinator) GateOnRequest(ctx context.Context, conversationID, headerBatchID string) (GateAction, *domain.Message, *domain.PendingToolCallBatch, string, error) {
	activeMsg, activeErr := c.msgs.FindByConversationActive(ctx, conversationID)
	if activeErr != nil && !errors.Is(activeErr, domain.ErrMessageNotFound) {
		slog.WarnContext(ctx, "chat proxy: FindByConversationActive failed, falling through",
			"error", activeErr, "conversation_id", conversationID)
		activeMsg = nil
	}

	// Implicit-resume branch: no header, but an active message exists. Look
	// up the conversation's active batches and apply the D-04 tri-case.
	if activeMsg != nil && headerBatchID == "" {
		batches, berr := c.pending.ListPendingByConversation(ctx, conversationID)
		if berr != nil {
			slog.WarnContext(ctx, "chat proxy: ListPendingByConversation failed",
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
			// D-04 case (b): orchestrator is mid-dispatch; rejoin keyed on
			// this batch.ID; reuse activeMsg.ID so tool_result events extend
			// the same Message (D-17).
			return GateActionRejoinResume, activeMsg, resolving, resolving.ID, nil
		case pending != nil:
			// D-04 case (c): approval card dropped off the client but the
			// batch is still pending → re-emit and close.
			return GateActionReemitApproval, activeMsg, pending, "", nil
		default:
			// D-04 case (d): orphan in_progress Message with no active batch.
			return GateActionInlineError, activeMsg, nil, "", nil
		}
	}

	// Explicit-resume branch: header AND active message → forward to resume.
	if activeMsg != nil && headerBatchID != "" {
		return GateActionRejoinResume, activeMsg, nil, headerBatchID, nil
	}
	return GateActionFresh, nil, nil, "", nil
}

// ReemitApprovalEvent writes a tool_approval_required SSE event built from
// the persisted PendingToolCallBatch. Used by the D-04 implicit-resume gate
// when the client reopens the chat mid-approval (network flap, page reload)
// and the batch is still in status="pending". No orchestrator roundtrip.
func (c *HITLCoordinator) ReemitApprovalEvent(w http.ResponseWriter, batch *domain.PendingToolCallBatch) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	calls := make([]map[string]interface{}, 0, len(batch.Calls))
	for _, call := range batch.Calls {
		calls = append(calls, map[string]interface{}{
			"call_id":         call.CallID,
			"tool_name":       call.ToolName,
			"args":            call.Arguments,
			"editable_fields": []string{},
			"floor":           "manual",
		})
	}
	payload := map[string]interface{}{
		"type":     "tool_approval_required",
		"batch_id": batch.ID,
		"calls":    calls,
	}
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// SSEInlineError writes a single {"type":"error","content":reason} SSE event
// and closes the stream. Used by the D-04 gate's orphan-in-progress case and
// by the resume guard when the batch is missing or mis-scoped.
func (c *HITLCoordinator) SSEInlineError(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	payload := map[string]interface{}{"type": sseEventError, "content": reason}
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// StreamResume proxies to the orchestrator's resume endpoint and folds
// tool_result events into the existing assistant Message (D-17). On done,
// transitions Message.Status from pending_approval/in_progress to complete.
func (c *HITLCoordinator) StreamResume(
	w http.ResponseWriter,
	r *http.Request,
	conversationID string,
	activeMsg *domain.Message,
	batchID string,
	persistCtx PersistContextFn,
) {
	// Validate the batch exists for this conversation before proxying.
	batch, err := c.pending.GetByBatchID(r.Context(), batchID)
	if err != nil || batch == nil || batch.ConversationID != conversationID {
		c.SSEInlineError(w, "no_active_approval_for_conversation")
		return
	}

	// Detach the orchestrator request from the client's request context so a
	// client-side reconnect cannot cancel the in-flight resume mid-LLM-call.
	corrID := logger.CorrelationIDFromContext(r.Context())
	orchCtx, orchCancel := context.WithTimeout(context.Background(), streamBudget)
	if corrID != "" {
		orchCtx = logger.WithCorrelationID(orchCtx, corrID)
	}
	defer orchCancel()

	headers := map[string]string{}
	if corrID != "" {
		headers["X-Correlation-ID"] = corrID
	}
	resp, err := c.orch.StreamResume(orchCtx, conversationID, batchID, nil, headers)
	if err != nil {
		slog.ErrorContext(r.Context(), "orchestrator resume request failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Stream SSE response back to client.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
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
		var ev SSEPayload
		if err := json.Unmarshal([]byte(line[6:]), &ev); err != nil {
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
			// Resume failed mid-stream (LLM error, ctx cancellation,
			// max-iterations cap). The error event is already forwarded
			// to the client; here we MUST transition the assistant
			// Message off pending_approval/in_progress, otherwise every
			// subsequent POST /chat hits the D-04 gate's
			// "turn_already_in_progress" branch and the conversation is
			// permanently stuck.
			msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
			c.persistResumeDone(persistCtx, &msg)
			return
		case "done":
			msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
			c.persistResumeDone(persistCtx, &msg)
			c.persister.FireAutoTitleIfPendingResume(persistCtx, conversationID, &msg)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(r.Context(), "chat proxy: resume scanner error",
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
	saveCtx, cancel := persistCtx()
	defer cancel()
	if err := c.msgs.Update(saveCtx, &msg); err != nil {
		slog.WarnContext(saveCtx, "resume: failed to persist partial message",
			"error", err, "message_id", msg.ID)
	}
}

// persistResumeDone writes the assistant message at resume-path "done". It
// runs on a fresh persistCtx (NOT r.Context()) because the request ctx is
// canceled when the SSE stream closes.
func (c *HITLCoordinator) persistResumeDone(persistCtx PersistContextFn, msg *domain.Message) {
	saveCtx, cancel := persistCtx()
	defer cancel()
	if err := c.msgs.Update(saveCtx, msg); err != nil {
		slog.WarnContext(saveCtx, "resume: failed to persist completed message",
			"error", err, "message_id", msg.ID)
	}
}

// writeJSONError is a chatproxy-local copy of the package-level helper in
// services/api/internal/handler/response.go. Duplicated here to avoid an
// import cycle once the entry handler in package handler imports chatproxy.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]string{"error": message}
	_ = json.NewEncoder(w).Encode(payload)
}
