package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitl"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// ResumeRequest carries the fresh state passed to Resume at approval-resolution
// time. The "fresh" qualifier is load-bearing for TOCTOU: the caller
// (resolve handler + chat_proxy) re-fetches
// business.settings.tool_approvals and project.approval_overrides from
// Postgres at the moment of resume — they may have changed since the batch
// was persisted. dispatchApprovedCalls re-runs hitl.Resolve against THESE
// maps, never the maps embedded in the snapshot.
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
// snapshot. Returns a fresh event channel; the spawned goroutine:
//
//  1. Loads the batch via pendingRepo.GetByBatchID; emits EventError on
//     missing or expired batches without advancing state.
//  2. Reconstructs RunState from the snapshot (batch.ModelMessages) — this
//     is what makes pause survive process restarts.
//  3. Dispatches each approved call in parallel (Redis dedupe absorbs
//     retries) with TOCTOU re-check via hitl.Resolve against the FRESH
//     approval maps in the ResumeRequest. Forbidden-after-pause → synthetic
//     policy_revoked rejection, no dispatch.
//  4. Skips calls with Dispatched==true (orchestrator crash-recovery).
//  5. Reject verdicts → synthetic rejection message + tool_rejected event,
//     no NATS dispatch.
//  6. After all parallel dispatches complete, MarkResolved is called and
//     stepRun re-enters the agent loop at Iter = batch.IterationIdx + 1.
func (o *Orchestrator) Resume(ctx context.Context, req ResumeRequest) (<-chan Event, error) {
	ch := make(chan Event, 32)

	if o.pendingRepo == nil {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, Content: "HITL not configured"}
		}()
		return ch, nil
	}

	batch, err := o.pendingRepo.GetByBatchID(ctx, req.BatchID)
	if err != nil {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, Content: fmt.Sprintf("batch not found: %v", err)}
		}()
		return ch, nil
	}
	if batch.Status == "expired" {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, Content: "approval_expired"}
		}()
		return ch, nil
	}

	go func() {
		defer close(ch)
		o.resumeGoroutine(ctx, batch, req, ch)
	}()
	return ch, nil
}

// decodeSnapshot reads batch.ModelMessages and returns the RunState fields it
// carries. Two shapes are accepted:
//
//   - Plan 24-02 envelope: {"v":2,"messages":[...],"system_platform":"...","system_business":"..."}
//     Messages is system-free; SystemPlatform/SystemBusiness travel separately
//     and are wired into llm.ChatRequest.SystemBlocks on the next stepRun.
//   - Legacy raw array: [{"role":"system",...}, ...] (pre Plan 24-02). Messages
//     still carries a leading role:"system" entry; SystemPlatform/Business
//     stay empty so stepRun falls through to provider-side legacy scrub.
//
// Distinguishing shape: a versioned envelope begins with '{' as the first
// non-whitespace byte, a legacy raw array begins with '['. Resume logs a
// debug entry on legacy batches so operators can confirm in-flight pre-24-02
// batches drained naturally.
func decodeSnapshot(raw []byte) (messages []llm.Message, platform, business string, legacy bool, err error) {
	if len(raw) == 0 {
		return nil, "", "", false, nil
	}
	// Skip leading whitespace to find the first JSON token.
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) {
		return nil, "", "", false, nil
	}
	if raw[i] == '{' {
		var env modelMessagesSnapshotV2
		if uErr := json.Unmarshal(raw, &env); uErr != nil {
			return nil, "", "", false, uErr
		}
		return env.Messages, env.SystemPlatform, env.SystemBusiness, false, nil
	}
	// Legacy array shape.
	var msgs []llm.Message
	if uErr := json.Unmarshal(raw, &msgs); uErr != nil {
		return nil, "", "", false, uErr
	}
	return msgs, "", "", true, nil
}

// resumeGoroutine is the body of the spawned resume goroutine. Extracted so
// tests can be written against narrower helpers and so the spawn wrapper in
// Resume stays trivially inspectable.
func (o *Orchestrator) resumeGoroutine(ctx context.Context, batch *domain.PendingToolCallBatch, req ResumeRequest, out chan<- Event) {
	// 1. Reconstruct state from the snapshot. decodeSnapshot accepts both
	// the Plan 24-02 envelope and the legacy raw-array shape; pre-24-02
	// batches in flight at deploy time drain through the legacy fallback.
	messages, platform, business, legacy, err := decodeSnapshot(batch.ModelMessages)
	if err != nil {
		out <- Event{Type: EventError, Content: fmt.Sprintf("corrupt snapshot: %v", err)}
		return
	}
	if legacy {
		slog.DebugContext(ctx, "resume: legacy snapshot detected — using provider scrub fallback",
			"batch_id", batch.ID,
		)
	}
	state := &RunState{
		Messages:                 messages,
		SystemPlatform:           platform,
		SystemBusiness:           business,
		AvailableTools:           o.tools.AvailableForWhitelist(ctx, req.ActiveIntegrations, req.WhitelistMode, req.AllowedTools),
		BusinessApprovals:        req.BusinessApprovals,
		ProjectApprovalOverrides: req.ProjectApprovalOverrides,
		ConversationID:           batch.ConversationID,
		BusinessID:               batch.BusinessID,
		ProjectID:                batch.ProjectID,
		UserID:                   batch.UserID,
		MessageID:                batch.MessageID,
		Model:                    req.Model,
		Tier:                     req.Tier,
		Iter:                     batch.IterationIdx + 1,
	}

	// Inject batch.BusinessID into the dispatch context so the NATS executor's
	// a2a.BusinessIDFromContext lookup picks it up. The handler entrypoint for
	// /chat does this on the regular path (handler/chat.go), but the resume
	// handler does not — without this line, every HITL-approved tool call
	// reaches platform agents with business_id="" and fails token resolution.
	ctx = a2a.WithBusinessID(ctx, batch.BusinessID)

	// 2. Dispatch approved calls in parallel with TOCTOU re-check. Pass
	//    the ResumeRequest so that hitl.Resolve inside the fan-out uses
	//    the FRESH approval maps, not the ones embedded in
	//    the snapshot which may be stale.
	o.dispatchApprovedCalls(ctx, batch, req, state, out)

	// 3. Mark batch resolved (best-effort — marking is hygienic, not
	//    load-bearing). If the mark fails we still continue stepRun: the
	//    conversation state is already correct; the batch will eventually
	//    be reaped by the TTL / reconciliation.
	if err := o.pendingRepo.MarkResolved(ctx, batch.ID); err != nil {
		slog.WarnContext(ctx, "resume: failed to mark batch resolved",
			"error", err,
			"batch_id", batch.ID,
		)
	}

	// 4. Continue the agent loop.
	_, _, _ = o.stepRun(ctx, state, out)
}

// dispatchApprovedCalls is the parallel fan-out core. Holds a WaitGroup to
// join all in-flight dispatches before returning; a mutex around
// state.Messages to keep llm.Message appends race-safe (go test -race
// mandatory). Emits tool_call / tool_result / tool_rejected events in
// whatever order goroutines finish — the caller (chat_proxy + frontend)
// associates them by ToolCallID, not by arrival order.
func (o *Orchestrator) dispatchApprovedCalls(
	ctx context.Context,
	batch *domain.PendingToolCallBatch,
	req ResumeRequest,
	state *RunState,
	out chan<- Event,
) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := range batch.Calls {
		call := batch.Calls[i]

		// Reject verdict → synthetic rejection, no dispatch.
		if call.Verdict == "reject" {
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
			out <- Event{
				Type:       EventToolRejected,
				ToolCallID: call.CallID,
				ToolName:   call.ToolName,
				Content:    reason,
			}
			continue
		}

		// TOCTOU re-check — re-run hitl.Resolve against the
		// FRESH approval maps carried in the ResumeRequest. If the
		// effective floor flipped to Forbidden after pause, synthesize
		// a policy_revoked rejection and skip dispatch.
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
			out <- Event{
				Type:       EventToolRejected,
				ToolCallID: call.CallID,
				ToolName:   call.ToolName,
				Content:    "policy_revoked",
			}
			continue
		}

		// Crash-recovery: skip calls that were already dispatched in a
		// prior attempt (belt-and-suspenders with the agent's Redis
		// SetNX dedupe).
		if call.Dispatched {
			continue
		}

		// Parallel dispatch
		wg.Add(1)
		go func(c domain.PendingCall) {
			defer wg.Done()

			args := c.Arguments
			if c.Verdict == "edit" && c.EditedArgs != nil {
				// Merge: EditedArgs values override originals. The
				// EditableFields whitelist was already enforced by
				// the resolve handler, so any key present
				// in EditedArgs is safe to overwrite.
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

			// Emit tool_call event so chat_proxy can persist it on
			// Message.ToolCalls (the LLM's real call_id flows all
			// the way through — no synthetic tc-N). DisplayName +
			// DisplayNameKey populated so the AgentTask row created on
			// the resume path also carries the i18n key —
			// matches the fresh-turn path in dispatchToolCalls.
			out <- Event{
				Type:               EventToolCall,
				ToolCallID:         c.CallID,
				ToolName:           c.ToolName,
				ToolDisplayName:    o.tools.DisplayName(c.ToolName),
				ToolDisplayNameKey: o.tools.DisplayNameKey(c.ToolName),
				ToolArgs:           args,
			}

			result, execErr := o.tools.ExecuteWithApproval(ctx, c.ToolName, args, approvalID)

			// Append tool result to conversation (race-safe append).
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

			// Mark the call dispatched (best-effort — Redis dedupe
			// at the agent is the primary safety layer; this is the
			// belt-and-suspenders Mongo flag).
			if markErr := o.pendingRepo.MarkDispatched(ctx, batch.ID, c.CallID); markErr != nil {
				slog.WarnContext(ctx, "resume: failed to mark call dispatched",
					"error", markErr,
					"batch_id", batch.ID,
					"call_id", c.CallID,
				)
			}

			// Emit tool_result event to the SSE stream. DisplayName +
			// DisplayNameKey carried through so chat_proxy can update
			// the AgentTask with the localizable key even when the row
			// was first created by a different orchestrator instance.
			out <- Event{
				Type:               EventToolResult,
				ToolCallID:         c.CallID,
				ToolName:           c.ToolName,
				ToolDisplayName:    o.tools.DisplayName(c.ToolName),
				ToolDisplayNameKey: o.tools.DisplayNameKey(c.ToolName),
				ToolResult:         result,
				ToolError:          errStr,
			}
		}(call)
	}

	wg.Wait()
}
