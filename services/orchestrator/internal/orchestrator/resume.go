package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitl"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// ResumeRequest carries the FRESH state passed to Resume at approval-
// resolution time. "Fresh" is load-bearing for TOCTOU.
// See docs/orchestrator/resume.md.
type ResumeRequest struct {
	BatchID                  string
	BusinessApprovals        map[string]domain.ToolFloor
	ProjectApprovalOverrides map[string]domain.ToolFloor
	ActiveIntegrations       []string
	WhitelistMode            domain.WhitelistMode
	AllowedTools             []string
	// Model / Tier preserved from the original request so post-resume
	// iterations keep routing to the same provider tier.
	Model string
	Tier  string
}

// Resume continues a paused agent turn from its persisted PendingToolCallBatch
// snapshot. See docs/orchestrator/resume.md for the goroutine lifecycle.
func (o *Orchestrator) Resume(ctx context.Context, req ResumeRequest) (<-chan Event, error) {
	ch := make(chan Event, 32)

	if o.pendingRepo == nil {
		slog.ErrorContext(ctx, "resume: HITL not configured but resume requested", "batch_id", req.BatchID)
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, Code: "internal_error", Content: friendlyInternalErrorMessage(ctx)}
		}()
		return ch, nil
	}

	batch, err := o.pendingRepo.GetByBatchID(ctx, req.BatchID)
	if err != nil {
		slog.ErrorContext(ctx, "resume: approval batch not found", "error", err, "batch_id", req.BatchID)
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, Code: "internal_error", Content: friendlyInternalErrorMessage(ctx)}
		}()
		return ch, nil
	}
	if batch.Status == "expired" {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, Code: "approval_expired", Content: friendlyApprovalExpiredMessage(ctx)}
		}()
		return ch, nil
	}

	go func() {
		defer close(ch)
		defer recoverToError(ctx, ch)
		o.resumeGoroutine(ctx, batch, req, ch)
	}()
	return ch, nil
}

// snapshotDecoded carries the decoded fields of a pause snapshot.
// See docs/orchestrator/resume.md for V1-legacy vs V2-envelope semantics.
type snapshotDecoded struct {
	Messages                []llm.Message
	SystemPlatform          string
	SystemBusiness          string
	AccumulatedInputTokens  int
	AccumulatedOutputTokens int
	Legacy                  bool
}

// decodeSnapshot reads batch.ModelMessages into a snapshotDecoded. Accepts
// versioned envelope ({...}) and legacy raw array ([...]). Shape is
// discriminated by the first non-whitespace byte.
func decodeSnapshot(raw []byte) (snapshotDecoded, error) {
	var out snapshotDecoded
	if len(raw) == 0 {
		return out, nil
	}
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) {
		return out, nil
	}
	if raw[i] == '{' {
		var env modelMessagesSnapshotV2
		if uErr := json.Unmarshal(raw, &env); uErr != nil {
			return snapshotDecoded{}, uErr
		}
		out.Messages = env.Messages
		out.SystemPlatform = env.SystemPlatform
		out.SystemBusiness = env.SystemBusiness
		out.AccumulatedInputTokens = env.AccumulatedInputTokens
		out.AccumulatedOutputTokens = env.AccumulatedOutputTokens
		return out, nil
	}
	var msgs []llm.Message
	if uErr := json.Unmarshal(raw, &msgs); uErr != nil {
		return snapshotDecoded{}, uErr
	}
	out.Messages = msgs
	out.Legacy = true
	return out, nil
}

// resumeUserUUID parses the persisted batch user id into the uuid.UUID form the
// LLM router rate-limits on. On an empty or malformed id it logs a structured
// warning and returns uuid.Nil — mirroring the chat handler's fresh-run path so
// the router RE-ENABLES limiting rather than silently bypassing it. Without this
// the resume RunState would carry a zero UserUUID, and router.checkRateLimit
// skips the per-user request / daily-spend guard whenever UserID == uuid.Nil.
func resumeUserUUID(ctx context.Context, userID string) uuid.UUID {
	if userID == "" {
		slog.WarnContext(ctx, "resume: batch has empty user_id, leaving UserUUID zero (rate limit may not key per-user)")
		return uuid.Nil
	}
	u, err := uuid.Parse(userID)
	if err != nil {
		slog.WarnContext(ctx, "resume: malformed user_id on batch, leaving UserUUID zero",
			"user_id", userID, "error", err)
		return uuid.Nil
	}
	return u
}

// resumeGoroutine is the body of the spawned resume goroutine. Extracted from
// Resume so tests can target narrower helpers and the spawn wrapper stays
// trivially inspectable. See docs/orchestrator/resume.md.
func (o *Orchestrator) resumeGoroutine(ctx context.Context, batch *domain.PendingToolCallBatch, req ResumeRequest, out chan<- Event) {
	snap, err := decodeSnapshot(batch.ModelMessages)
	if err != nil {
		slog.ErrorContext(ctx, "resume: corrupt pause snapshot", "error", err, "batch_id", batch.ID)
		out <- Event{Type: EventError, Code: "internal_error", Content: friendlyInternalErrorMessage(ctx)}
		return
	}
	if snap.Legacy {
		slog.DebugContext(ctx, "resume: legacy snapshot detected — using provider scrub fallback",
			"batch_id", batch.ID,
		)
	}
	state := &RunState{
		Messages:                 snap.Messages,
		SystemPlatform:           snap.SystemPlatform,
		SystemBusiness:           snap.SystemBusiness,
		AvailableTools:           o.tools.AvailableForWhitelist(ctx, req.ActiveIntegrations, req.WhitelistMode, req.AllowedTools),
		BusinessApprovals:        req.BusinessApprovals,
		ProjectApprovalOverrides: req.ProjectApprovalOverrides,
		ConversationID:           batch.ConversationID,
		BusinessID:               batch.BusinessID,
		ProjectID:                batch.ProjectID,
		UserID:                   batch.UserID,
		UserUUID:                 resumeUserUUID(ctx, batch.UserID),
		MessageID:                batch.MessageID,
		Model:                    req.Model,
		Tier:                     req.Tier,
		Iter:                     batch.IterationIdx + 1,
		AccumulatedInputTokens:   snap.AccumulatedInputTokens,
		AccumulatedOutputTokens:  snap.AccumulatedOutputTokens,
	}

	ctx = a2a.WithBusinessID(ctx, batch.BusinessID)

	o.dispatchApprovedCalls(ctx, batch, req, state, out)

	if err := o.pendingRepo.MarkResolved(ctx, batch.ID); err != nil {
		slog.WarnContext(ctx, "resume: failed to mark batch resolved",
			"error", err,
			"batch_id", batch.ID,
		)
	}

	_, _, _ = o.stepRun(ctx, state, out)
}

// dispatchApprovedCalls is the parallel fan-out core. WaitGroup joins all
// in-flight dispatches; mutex on state.Messages keeps appends race-safe
// (go test -race invariant). Events are emitted in goroutine-completion
// order; consumers correlate by ToolCallID.
// See docs/orchestrator/resume.md.
func (o *Orchestrator) dispatchApprovedCalls(
	ctx context.Context,
	batch *domain.PendingToolCallBatch,
	req ResumeRequest,
	state *RunState,
	out chan<- Event,
) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	sendOrCancel := func(ev Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for i := range batch.Calls {
		if ctx.Err() != nil {
			break
		}
		call := batch.Calls[i]

		// Fail-closed: a call is dispatched ONLY when its verdict is an explicit
		// approval (approve/edit). Any other verdict — reject, or an EMPTY/unknown
		// verdict left behind when RecordDecisions failed to persist after the
		// batch transitioned to "resolving" — is treated as a rejection and never
		// executes, so a call the user intended to reject is not published.
		if call.Verdict != "approve" && call.Verdict != "edit" {
			reason := call.RejectReason
			if reason == "" {
				reason = "user_rejected"
			}
			rejectionMsg := fmt.Sprintf(`{"rejected":true,"reason":%q}`, reason)
			mu.Lock()
			state.Messages = append(state.Messages, llm.Message{
				Role:       "tool",
				Content:    rejectionMsg,
				ToolCallID: call.CallID,
			})
			mu.Unlock()
			if !sendOrCancel(Event{
				Type:       EventToolRejected,
				ToolCallID: call.CallID,
				ToolName:   call.ToolName,
				Content:    reason,
			}) {
				return
			}
			continue
		}

		floor := o.tools.Floor(call.ToolName)
		effective := hitl.Resolve(floor, req.BusinessApprovals, req.ProjectApprovalOverrides, call.ToolName)
		if effective == domain.ToolFloorForbidden {
			rejectionMsg := `{"rejected":true,"reason":"policy_revoked"}`
			mu.Lock()
			state.Messages = append(state.Messages, llm.Message{
				Role:       "tool",
				Content:    rejectionMsg,
				ToolCallID: call.CallID,
			})
			mu.Unlock()
			if !sendOrCancel(Event{
				Type:       EventToolRejected,
				ToolCallID: call.CallID,
				ToolName:   call.ToolName,
				Content:    "policy_revoked",
			}) {
				return
			}
			continue
		}

		if call.Dispatched {
			mu.Lock()
			state.Messages = append(state.Messages, llm.Message{
				Role:       "tool",
				Content:    `{"dispatched":true,"note":"result not persisted across resume"}`,
				ToolCallID: call.CallID,
			})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(c domain.PendingCall) {
			defer wg.Done()

			args := c.Arguments
			if c.Verdict == "edit" && c.EditedArgs != nil {
				merged := make(map[string]interface{}, len(args)+len(c.EditedArgs))
				for k, v := range args {
					merged[k] = v
				}
				for k, v := range c.EditedArgs {
					merged[k] = v
				}
				args = merged
			}

			approvalID := fmt.Sprintf("%s-%s", batch.ID, c.CallID)

			if !sendOrCancel(Event{
				Type:               EventToolCall,
				ToolCallID:         c.CallID,
				ToolName:           c.ToolName,
				ToolDisplayName:    o.tools.DisplayName(c.ToolName),
				ToolDisplayNameKey: o.tools.DisplayNameKey(c.ToolName),
				ToolArgs:           args,
			}) {
				return
			}

			callCtx, cancel := o.toolCallContext(ctx)
			result, execErr := o.tools.ExecuteWithApproval(callCtx, c.ToolName, args, approvalID)
			cancel()

			mu.Lock()
			errStr := ""
			var resultJSON []byte
			if execErr != nil {
				errStr = execErr.Error()
				resultJSON = []byte(fmt.Sprintf(`{"error":%q}`, errStr))
			} else {
				if b, marshalErr := json.Marshal(result); marshalErr == nil {
					resultJSON = b
				} else {
					resultJSON = []byte(fmt.Sprintf(`{"error":"marshal failed: %s"}`, marshalErr.Error()))
				}
			}
			state.Messages = append(state.Messages, llm.Message{
				Role:       "tool",
				Content:    string(resultJSON),
				ToolCallID: c.CallID,
			})
			mu.Unlock()

			if markErr := o.pendingRepo.MarkDispatched(ctx, batch.ID, c.CallID); markErr != nil {
				slog.WarnContext(ctx, "resume: failed to mark call dispatched",
					"error", markErr,
					"batch_id", batch.ID,
					"call_id", c.CallID,
				)
			}

			_ = sendOrCancel(Event{
				Type:               EventToolResult,
				ToolCallID:         c.CallID,
				ToolName:           c.ToolName,
				ToolDisplayName:    o.tools.DisplayName(c.ToolName),
				ToolDisplayNameKey: o.tools.DisplayNameKey(c.ToolName),
				ToolResult:         result,
				ToolError:          errStr,
				Code:               a2a.CodeOf(execErr),
			})
		}(call)
	}

	wg.Wait()
}
