package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// --- Instrumented executor that records approvalID + args ---

type recordingExecutor struct {
	mu     sync.Mutex
	calls  []recordedCall
	delay  time.Duration
	result interface{}
}

type recordedCall struct {
	approvalID string
	args       map[string]interface{}
}

func (r *recordingExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return r.ExecuteWithApproval(ctx, args, "")
}

func (r *recordingExecutor) ExecuteWithApproval(ctx context.Context, args map[string]interface{}, approvalID string) (interface{}, error) {
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	r.mu.Lock()
	r.calls = append(r.calls, recordedCall{approvalID: approvalID, args: args})
	r.mu.Unlock()
	if r.result == nil {
		return map[string]interface{}{"ok": true}, nil
	}
	return r.result, nil
}

func (r *recordingExecutor) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *recordingExecutor) approvalIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.approvalID)
	}
	return out
}

// Compile-time guard: recordingExecutor implements ApprovalExecutor.
var _ toolregistry.ApprovalExecutor = (*recordingExecutor)(nil)

// --- Helpers ---

func batchWithCalls(t *testing.T, batchID string, calls []domain.PendingCall) *domain.PendingToolCallBatch {
	t.Helper()
	snapshot, err := json.Marshal([]llm.Message{{Role: "user", Content: "do it"}})
	require.NoError(t, err)
	return &domain.PendingToolCallBatch{
		ID:             batchID,
		ConversationID: "conv-r",
		BusinessID:     "biz-r",
		ProjectID:      "proj-r",
		UserID:         "user-r",
		MessageID:      "msg-r",
		Status:         "pending",
		Calls:          calls,
		ModelMessages:  snapshot,
		IterationIdx:   0,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
	}
}

// registryWithRecording registers the given toolName with the given floor and
// uses recordingExecutor as the implementation. Returns the executor so tests
// can inspect what it captured.
func registryWithRecording(t *testing.T, toolName string, floor domain.ToolFloor) (*toolregistry.Registry, *recordingExecutor) {
	t.Helper()
	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{
		Def: llm.ToolDefinition{
			Type:     llm.ToolCallTypeFunction,
			Function: llm.FunctionDefinition{Name: toolName, Description: "d", Parameters: map[string]interface{}{}},
		},
		Floor:          floor,
		EditableFields: []string{"text"},
	}, rec)
	return reg, rec
}

// --- Tests ---

func TestResume_BatchMissing_EmitsError(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "done", FinishReason: "stop"}}}
	reg, _ := registryWithRecording(t, "manual_tool", domain.ToolFloorManual)
	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID: "nonexistent",
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "internal_error", errs[0].Code)
	assert.NotEmpty(t, errs[0].Content)
	assert.NotContains(t, errs[0].Content, "batch not found",
		"user-facing content must not leak the raw batch-not-found detail")
}

func TestResume_BatchExpired_EmitsError(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "done", FinishReason: "stop"}}}
	reg, _ := registryWithRecording(t, "manual_tool", domain.ToolFloorManual)
	repo := newMockPendingRepo()
	expired := &domain.PendingToolCallBatch{ID: "batch-x", Status: "expired"}
	repo.store["batch-x"] = expired

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-x"})
	require.NoError(t, err)

	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "approval_expired", errs[0].Code)
	assert.NotEmpty(t, errs[0].Content)
	assert.NotEqual(t, "approval_expired", errs[0].Content,
		"content must be a localized message, not the raw code")
}

func TestResume_AllApproved_DispatchesInParallel(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	rec := &recordingExecutor{
		delay:  100 * time.Millisecond,
		result: map[string]interface{}{"ok": true},
	}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "parallel_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-p", []domain.PendingCall{
		{CallID: "c1", ToolName: "parallel_tool", Arguments: map[string]interface{}{"text": "a"}, Verdict: "approve"},
		{CallID: "c2", ToolName: "parallel_tool", Arguments: map[string]interface{}{"text": "b"}, Verdict: "approve"},
	})
	repo.store["batch-p"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	start := time.Now()
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-p"})
	require.NoError(t, err)

	gotResults := 0
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			gotResults++
		}
	}
	elapsed := time.Since(start)

	assert.Equal(t, 2, gotResults)
	assert.Less(t, elapsed, 180*time.Millisecond, "parallel dispatch must complete in less than 2×delay")

	ids := rec.approvalIDs()
	assert.Equal(t, 2, len(ids))
	assert.Contains(t, ids, "batch-p-c1")
	assert.Contains(t, ids, "batch-p-c2")
}

func TestResume_RejectedCall_SynthesizesToolMessage_SkipsDispatch(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	var dispatched int32
	rec := &instrumentedExecutor{onDispatch: func() { atomic.AddInt32(&dispatched, 1) }}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "rej_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-r", []domain.PendingCall{
		{CallID: "c-rej", ToolName: "rej_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "reject", RejectReason: "off-brand"},
	})
	repo.store["batch-r"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-r"})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, int32(0), atomic.LoadInt32(&dispatched),
		"rejected calls MUST NOT be dispatched")

	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, "off-brand", rejects[0].Content)
	assert.Equal(t, "c-rej", rejects[0].ToolCallID)
}

// TestResume_EmptyVerdict_FailsClosed_SkipsDispatch — when a batch reaches
// "resolving" but RecordDecisions never persisted the per-call verdicts (a
// transient Mongo failure on resolve), every call carries an EMPTY verdict. A
// resume MUST treat such a call as a rejection and NEVER execute it, otherwise a
// tool call the user intended to reject gets published with its original args.
func TestResume_EmptyVerdict_FailsClosed_SkipsDispatch(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	var dispatchedEmpty int32
	rec := &instrumentedExecutor{onDispatch: func() { atomic.AddInt32(&dispatchedEmpty, 1) }}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "empty_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-empty", []domain.PendingCall{
		{CallID: "c-empty", ToolName: "empty_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: ""},
	})
	batch.Status = "resolving"
	repo.store["batch-empty"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-empty"})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, int32(0), atomic.LoadInt32(&dispatchedEmpty),
		"empty-verdict call MUST NOT be dispatched (fail-closed)")
	toolCalls := findEvents(evts, orchestrator.EventToolCall)
	for _, tc := range toolCalls {
		assert.NotEqual(t, "c-empty", tc.ToolCallID,
			"empty-verdict call MUST NOT emit a tool_call event")
	}
	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, "c-empty", rejects[0].ToolCallID)
}

func TestResume_TOCTOU_PolicyRevoked_DropsCallWithSyntheticMessage(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	var dispatched int32
	rec := &instrumentedExecutor{onDispatch: func() { atomic.AddInt32(&dispatched, 1) }}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "toctou_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-toctou", []domain.PendingCall{
		{CallID: "c-t", ToolName: "toctou_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	repo.store["batch-toctou"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID: "batch-toctou",
		BusinessApprovals: map[string]domain.ToolFloor{
			"toctou_tool": domain.ToolFloorForbidden,
		},
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, int32(0), atomic.LoadInt32(&dispatched),
		"policy_revoked → tool must NOT be dispatched")
	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, "policy_revoked", rejects[0].Content)
}

// TestResume_ApprovedPlatformTool_DispatchesUnderProductionBody guards the
// resume dispatch chokepoint against an over-block regression. The withheld-tool
// gate is the offer-time enforceOffered boundary in stepRun: a tool the project
// whitelist omits is moved to the forbidden bucket BEFORE the pending batch is
// built, so it can never become an approval card and never reach this path. The
// resume dispatch must therefore trust the floor/verdict alone — it must NOT
// re-derive an offered set from state.AvailableTools, because the production
// resume bodies (approve / rejoin) carry no ActiveIntegrations / WhitelistMode /
// AllowedTools, so AvailableForWhitelist drops every {platform}__action tool
// (no active integration). Re-introducing that re-check would reject every
// legitimately-approved platform write tool as policy_forbidden.
//
// This test drives a real Manual-floor platform tool through a
// production-equivalent ResumeRequest (no integration/whitelist fields) and
// asserts it is still dispatched. If someone re-adds the AvailableTools offered
// gate to dispatchApprovedCalls, this test fails.
func TestResume_ApprovedPlatformTool_DispatchesUnderProductionBody(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	var dispatched int32
	rec := &instrumentedExecutor{onDispatch: func() { atomic.AddInt32(&dispatched, 1) }}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "telegram__send_channel_post", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-plat", []domain.PendingCall{
		{CallID: "c-plat", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	repo.store["batch-plat"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID: "batch-plat",
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, int32(1), atomic.LoadInt32(&dispatched),
		"an approved platform tool MUST dispatch — the production resume body carries no whitelist fields")
	assert.Empty(t, findEvents(evts, orchestrator.EventToolRejected),
		"no rejection (policy_forbidden) may be emitted for a legitimately-approved platform tool")
}

// TestResume_PostApproval_OffersActivePlatformTools is the multi-step approved
// flow guard. After the first approved platform tool runs, the post-approval
// stepRun re-invokes the LLM, which emits a SECOND platform tool (a different
// active integration). That tool must remain offered — it surfaces as a fresh
// approval card, NOT a policy_forbidden rejection. This only holds when the
// resume request carries ActiveIntegrations: AvailableForWhitelist drops every
// {platform}__action tool from AvailableTools when the active set is empty, so
// offeredToolSet would not contain vk__publish_post and enforceOffered would
// reject it. The api Resume handler must therefore populate active_integrations.
//
// Fail-on-revert: drop ActiveIntegrations from the ResumeRequest below and the
// second vk tool is rejected with policy_forbidden — the assertions fail.
func TestResume_PostApproval_OffersActivePlatformTools(t *testing.T) {
	vkArgs, _ := json.Marshal(map[string]interface{}{"text": "vk post"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "vk-call-1",
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{
					Name:      "vk__publish_post",
					Arguments: string(vkArgs),
				},
			}},
		},
		{Content: "готово", FinishReason: "stop"},
	}}

	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	for _, name := range []string{"telegram__send_channel_post", "vk__publish_post"} {
		reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
			Type:     llm.ToolCallTypeFunction,
			Function: llm.FunctionDefinition{Name: name, Description: "d", Parameters: map[string]interface{}{}},
		}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)
	}

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-multi", []domain.PendingCall{
		{CallID: "tg-call-1", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "tg post"}, Verdict: "approve"},
	})
	repo.store["batch-multi"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID:            "batch-multi",
		ActiveIntegrations: []string{"telegram", "vk"},
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	for _, ev := range findEvents(evts, orchestrator.EventToolRejected) {
		assert.NotEqual(t, "policy_forbidden", ev.Content,
			"the post-approval vk tool must not be rejected as policy_forbidden when vk is active")
		assert.NotEqual(t, "vk__publish_post", ev.ToolName,
			"the post-approval vk tool must not be rejected when vk is active")
	}

	approvals := findEvents(evts, orchestrator.EventToolApprovalRequired)
	require.NotEmpty(t, approvals,
		"the second active-platform tool must surface as a fresh approval card, not be dropped")
	sawVK := false
	for _, ap := range approvals {
		for _, c := range ap.Calls {
			if c.ToolName == "vk__publish_post" {
				sawVK = true
			}
		}
	}
	assert.True(t, sawVK, "vk__publish_post must reach the post-approval approval card")
}

func TestResume_AlreadyDispatched_SkipsReDispatch(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	var dispatched int32
	rec := &instrumentedExecutor{onDispatch: func() { atomic.AddInt32(&dispatched, 1) }}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "done_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-d", []domain.PendingCall{
		{
			CallID:     "c-dispatched",
			ToolName:   "done_tool",
			Arguments:  map[string]interface{}{"text": "x"},
			Verdict:    "approve",
			Dispatched: true,
		},
	})
	repo.store["batch-d"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-d"})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, int32(0), atomic.LoadInt32(&dispatched),
		"already-dispatched call MUST NOT be re-executed")
	toolCalls := findEvents(evts, orchestrator.EventToolCall)
	for _, tc := range toolCalls {
		assert.NotEqual(t, "c-dispatched", tc.ToolCallID,
			"already-dispatched call MUST NOT emit tool_call event")
	}
}

func TestResume_EditedArgs_PassesMergedArgsToExecutor(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "edit_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-e", []domain.PendingCall{
		{
			CallID:    "c-e",
			ToolName:  "edit_tool",
			Arguments: map[string]interface{}{"text": "orig", "channel_id": "-100"},
			Verdict:   "edit",
			EditedArgs: map[string]interface{}{
				"text": "edited",
			},
		},
	})
	repo.store["batch-e"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-e"})
	require.NoError(t, err)

	for range events {
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.calls, 1)
	assert.Equal(t, "edited", rec.calls[0].args["text"])
	assert.Equal(t, "-100", rec.calls[0].args["channel_id"])
}

func TestResume_ApprovalID_IsBatchIDDashCallID(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "appr_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-1", []domain.PendingCall{
		{CallID: "call-a", ToolName: "appr_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	repo.store["batch-1"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-1"})
	require.NoError(t, err)

	for range events {
	}

	ids := rec.approvalIDs()
	require.Len(t, ids, 1)
	assert.Equal(t, "batch-1-call-a", ids[0])
}

func TestResume_CompletesAndContinuesStepRun_ToDone(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "Готово!", FinishReason: "stop"},
	}}

	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "cont_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-cont", []domain.PendingCall{
		{CallID: "c-cont", ToolName: "cont_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	repo.store["batch-cont"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-cont"})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.NotEmpty(t, findEvents(evts, orchestrator.EventToolCall))
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventToolResult))
	texts := findEvents(evts, orchestrator.EventText)
	require.NotEmpty(t, texts)
	assert.Contains(t, texts[0].Content, "Готово")
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventDone))
}

func TestResume_MixedRejectAndApprove_BothProcessed(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "mix_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-mix", []domain.PendingCall{
		{CallID: "c-ok", ToolName: "mix_tool", Arguments: map[string]interface{}{"text": "ok"}, Verdict: "approve"},
		{CallID: "c-no", ToolName: "mix_tool", Arguments: map[string]interface{}{"text": "no"}, Verdict: "reject", RejectReason: "nope"},
	})
	repo.store["batch-mix"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-mix"})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, 1, rec.callCount())
	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, "nope", rejects[0].Content)
}

// resultOrErrExecutor returns a fixed (result, err) pair on every dispatch.
// Used to drive the resume tool_result code-stamping tests: an *a2a.CodedError
// returned here must surface on the emitted EventToolResult.Code.
type resultOrErrExecutor struct {
	result interface{}
	err    error
}

func (e *resultOrErrExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return e.ExecuteWithApproval(ctx, args, "")
}

func (e *resultOrErrExecutor) ExecuteWithApproval(_ context.Context, _ map[string]interface{}, _ string) (interface{}, error) {
	return e.result, e.err
}

var _ toolregistry.ApprovalExecutor = (*resultOrErrExecutor)(nil)

// registryWithExecutor registers toolName at the given floor backed by exec.
func registryWithExecutor(toolName string, floor domain.ToolFloor, exec toolregistry.Executor) *toolregistry.Registry {
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: toolName, Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: floor, EditableFields: []string{"text"}}, exec)
	return reg
}

// TestResume_ToolResult_StampsCodeFromCodedError — when an approved tool's
// executor returns an *a2a.CodedError, the resume EventToolResult must carry
// the classifier code so the api proxy flips the integration to token_expired
// and the FE renders the reconnect badge. Before the fix the resume path
// dropped Code (the fresh path already set it), so the badge never fired.
func TestResume_ToolResult_StampsCodeFromCodedError(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	exec := &resultOrErrExecutor{
		err: a2a.NewCodedError("integration_token_invalid", errors.New("Unauthorized: bot kicked")),
	}
	reg := registryWithExecutor("coded_tool", domain.ToolFloorManual, exec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-coded", []domain.PendingCall{
		{CallID: "c-coded", ToolName: "coded_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	repo.store["batch-coded"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-coded"})
	require.NoError(t, err)

	evts := drainEvents(events)
	results := findEvents(evts, orchestrator.EventToolResult)
	require.Len(t, results, 1)
	assert.Equal(t, "integration_token_invalid", results[0].Code,
		"resume tool_result must stamp the CodedError classifier code")
	assert.NotEmpty(t, results[0].ToolError, "a coded error must also carry a human-readable ToolError")
}

// TestResume_ToolResult_SuccessHasEmptyCode — a successful approved tool emits
// a tool_result with no Code, so the api proxy never spuriously flips token
// health on a healthy publish.
func TestResume_ToolResult_SuccessHasEmptyCode(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	exec := &resultOrErrExecutor{result: map[string]interface{}{"ok": true}}
	reg := registryWithExecutor("ok_tool", domain.ToolFloorManual, exec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-ok", []domain.PendingCall{
		{CallID: "c-ok", ToolName: "ok_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	repo.store["batch-ok"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-ok"})
	require.NoError(t, err)

	evts := drainEvents(events)
	results := findEvents(evts, orchestrator.EventToolResult)
	require.Len(t, results, 1)
	assert.Empty(t, results[0].Code, "successful resume tool_result must have no classifier code")
	assert.Empty(t, results[0].ToolError, "successful resume tool_result must have no error")
}

// --- Supporting mock executor that tracks dispatch count without
//     recording args (used where tests only need pass/no-pass behavior) ---

type instrumentedExecutor struct {
	onDispatch func()
}

func (i *instrumentedExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return i.ExecuteWithApproval(ctx, args, "")
}

func (i *instrumentedExecutor) ExecuteWithApproval(_ context.Context, _ map[string]interface{}, _ string) (interface{}, error) {
	if i.onDispatch != nil {
		i.onDispatch()
	}
	return map[string]interface{}{"ok": true}, nil
}

var _ toolregistry.ApprovalExecutor = (*instrumentedExecutor)(nil)

// Ensure imports are referenced — fmt/strings are used above in minor
// contexts; this no-op keeps the compiler happy if any edit trims a usage.
var _ = fmt.Sprintf
var _ = strings.Contains

// ---------------------------------------------------------------------
// Snapshot hydration tests (accumulated token counts)
// ---------------------------------------------------------------------

// snapshotWithAccumulated marshals a V2 snapshot envelope including the new
// accumulated token-count fields. Used to drive resume hydration tests.
func snapshotWithAccumulated(t *testing.T, accumIn, accumOut int) []byte {
	t.Helper()
	type envV2 struct {
		V                       int           `json:"v"`
		Messages                []llm.Message `json:"messages"`
		SystemPlatform          string        `json:"system_platform,omitempty"`
		SystemBusiness          string        `json:"system_business,omitempty"`
		AccumulatedInputTokens  int           `json:"accumulated_input_tokens,omitempty"`
		AccumulatedOutputTokens int           `json:"accumulated_output_tokens,omitempty"`
	}
	b, err := json.Marshal(envV2{
		V:                       2,
		Messages:                []llm.Message{{Role: "user", Content: "hi"}},
		AccumulatedInputTokens:  accumIn,
		AccumulatedOutputTokens: accumOut,
	})
	require.NoError(t, err)
	return b
}

// TestResume_PreservesAccumulatedTokens — a paused snapshot with 40k input
// hydrates the new RunState fields so the resumed loop's cap measurement
// continues from the pre-pause budget rather than restarting at zero.
func TestResume_PreservesAccumulatedTokens(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "boom", FinishReason: "stop", Usage: llm.TokenUsage{InputTokens: 15000}},
	}}
	reg := toolregistry.NewRegistry()
	repo := newMockPendingRepo()

	batch := batchWithCalls(t, "batch-acc", []domain.PendingCall{})
	batch.ModelMessages = snapshotWithAccumulated(t, 40000, 5000)
	repo.store["batch-acc"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{
		MaxIterations:        10,
		ConversationInputCap: 50000,
	})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-acc"})
	require.NoError(t, err)
	evts := drainEvents(events)

	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "conversation_token_cap", errs[0].Code,
		"resumed loop must use the hydrated accumulator, not reset to 0")
}

// TestResume_LegacyV1SnapshotZeroes — pre-cap snapshot (no accumulated_*
// fields) hydrates with 0 so the resumed loop measures from zero. Pre-cap
// turns were not subject to enforcement; this is the correct semantics.
func TestResume_LegacyV1SnapshotZeroes(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "ok", FinishReason: "stop", Usage: llm.TokenUsage{InputTokens: 5000}},
	}}
	reg := toolregistry.NewRegistry()
	repo := newMockPendingRepo()

	batch := batchWithCalls(t, "batch-legacy", []domain.PendingCall{})
	raw, err := json.Marshal([]llm.Message{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	batch.ModelMessages = raw
	repo.store["batch-legacy"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{
		MaxIterations:        10,
		ConversationInputCap: 50000,
	})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-legacy"})
	require.NoError(t, err)
	evts := drainEvents(events)

	assert.Empty(t, findEvents(evts, orchestrator.EventError),
		"legacy snapshot must hydrate at 0 so the resumed turn does not falsely trip the cap")
}

// TestResume_CtxCancelledMidDispatch_ReturnsPromptly_NoGoroutineLeak —
// regression for the goroutine-leak vector in dispatchApprovedCalls. With a
// large approved batch and a caller that disconnects (ctx cancellation) mid-
// resume, every per-call goroutine must observe ctx.Done() on its channel
// send and return, so wg.Wait() unblocks and the resume goroutine reaches
// close(ch). Before the fix the unbuffered `out <- Event{...}` sends could
// block forever once the 32-slot SSE buffer was full and the consumer stopped
// reading.
func TestResume_CtxCancelledMidDispatch_ReturnsPromptly_NoGoroutineLeak(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	const numCalls = 64
	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "leak_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, rec)

	repo := newMockPendingRepo()
	calls := make([]domain.PendingCall, 0, numCalls)
	for i := 0; i < numCalls; i++ {
		calls = append(calls, domain.PendingCall{
			CallID:    fmt.Sprintf("c-%d", i),
			ToolName:  "leak_tool",
			Arguments: map[string]interface{}{"text": "x"},
			Verdict:   "approve",
		})
	}
	batch := batchWithCalls(t, "batch-leak", calls)
	repo.store["batch-leak"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	ctx, cancel := context.WithCancel(context.Background())
	events, err := orch.Resume(ctx, orchestrator.ResumeRequest{BatchID: "batch-leak"})
	require.NoError(t, err)

	cancel()

	closed := make(chan struct{})
	go func() {
		for range events {
		}
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("resume dispatch did not return after ctx cancellation — goroutine leak regression")
	}
}

// TestResume_AccumulatedTokensSurviveJSONRoundTrip — marshal/unmarshal of a
// V2 snapshot preserves the new fields verbatim.
func TestResume_AccumulatedTokensSurviveJSONRoundTrip(t *testing.T) {
	raw := snapshotWithAccumulated(t, 12345, 6789)

	var env struct {
		V                       int           `json:"v"`
		Messages                []llm.Message `json:"messages"`
		AccumulatedInputTokens  int           `json:"accumulated_input_tokens,omitempty"`
		AccumulatedOutputTokens int           `json:"accumulated_output_tokens,omitempty"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.Equal(t, 12345, env.AccumulatedInputTokens)
	assert.Equal(t, 6789, env.AccumulatedOutputTokens)
}

// batchWithUserID builds a single-approved-call batch carrying the given
// user_id, used to assert the resumed loop threads the user identity into the
// post-resume LLM request (and thus the router's per-user rate limiter).
func batchWithUserID(t *testing.T, batchID, userID string) *domain.PendingToolCallBatch {
	t.Helper()
	b := batchWithCalls(t, batchID, []domain.PendingCall{
		{CallID: "c-rl", ToolName: "ok_tool", Arguments: map[string]interface{}{"text": "x"}, Verdict: "approve"},
	})
	b.UserID = userID
	return b
}

// TestResume_ThreadsUserIDIntoLLMRequest — a resumed run MUST carry the same
// authenticated user id (persisted on the batch at pause time) into the
// post-resume LLM ChatRequest.UserID. Otherwise UserUUID stays uuid.Nil and the
// router skips the per-user rate limit / daily-spend guard on every iteration
// after a HITL approval.
func TestResume_ThreadsUserIDIntoLLMRequest(t *testing.T) {
	userID := uuid.New()
	rec := &capturingLLM{resp: &llm.ChatResponse{Content: "done", FinishReason: "stop"}}

	exec := &resultOrErrExecutor{result: map[string]interface{}{"ok": true}}
	reg := registryWithExecutor("ok_tool", domain.ToolFloorManual, exec)

	repo := newMockPendingRepo()
	repo.store["batch-rl"] = batchWithUserID(t, "batch-rl", userID.String())

	orch := orchestrator.NewWithHITL(rec, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-rl"})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)
	assert.Equal(t, userID, got.UserID,
		"resumed loop must thread batch.UserID into ChatRequest.UserID so the router rate-limits per user")
	assert.NotEqual(t, uuid.Nil, got.UserID,
		"resumed LLM request must not carry uuid.Nil — that bypasses the rate limiter")
}

// TestResume_MalformedUserID_DegradesToNil — a non-UUID persisted user_id must
// degrade to uuid.Nil rather than panic. This re-enables the router's nil-guard
// (limiting still applies once a valid id flows) and mirrors the chat handler's
// conservative parse-failure behavior; it never silently fabricates an id.
func TestResume_MalformedUserID_DegradesToNil(t *testing.T) {
	rec := &capturingLLM{resp: &llm.ChatResponse{Content: "done", FinishReason: "stop"}}

	exec := &resultOrErrExecutor{result: map[string]interface{}{"ok": true}}
	reg := registryWithExecutor("ok_tool", domain.ToolFloorManual, exec)

	repo := newMockPendingRepo()
	repo.store["batch-bad"] = batchWithUserID(t, "batch-bad", "not-a-uuid")

	orch := orchestrator.NewWithHITL(rec, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-bad"})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)
	assert.Equal(t, uuid.Nil, got.UserID,
		"malformed batch user_id must degrade to uuid.Nil, not panic or fabricate an id")
}

// panicOrOKExecutor panics on dispatch when ToolName matches panicTool, and
// otherwise returns a healthy result. Used to drive the resume panic-recovery
// test so one bad approved call is contained without crashing the process.
type panicOrOKExecutor struct {
	panicArg string
}

func (e *panicOrOKExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return e.ExecuteWithApproval(ctx, args, "")
}

func (e *panicOrOKExecutor) ExecuteWithApproval(_ context.Context, args map[string]interface{}, _ string) (interface{}, error) {
	if v, ok := args["text"].(string); ok && v == e.panicArg {
		panic("boom in approved tool executor")
	}
	return map[string]interface{}{"ok": true}, nil
}

var _ toolregistry.ApprovalExecutor = (*panicOrOKExecutor)(nil)

// TestResume_ApprovedToolPanic_RecoversToErrorEvent asserts that a panic inside
// one approved tool's executor on the resume fan-out is contained to THAT call —
// Resume completes, emits a tool_result error event for the panicking call, and
// the sibling approved call still produces its normal result. Without the
// per-goroutine recover in dispatchApprovedCalls the panic propagates uncaught
// and aborts the whole process (the parent-goroutine recoverToError cannot catch
// a child-goroutine panic).
func TestResume_ApprovedToolPanic_RecoversToErrorEvent(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	exec := &panicOrOKExecutor{panicArg: "boom"}
	reg := registryWithExecutor("panic_tool", domain.ToolFloorManual, exec)

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-panic", []domain.PendingCall{
		{CallID: "c-ok", ToolName: "panic_tool", Arguments: map[string]interface{}{"text": "fine"}, Verdict: "approve"},
		{CallID: "c-panic", ToolName: "panic_tool", Arguments: map[string]interface{}{"text": "boom"}, Verdict: "approve"},
	})
	repo.store["batch-panic"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-panic"})
	require.NoError(t, err)

	evts := drainEvents(events)
	results := findEvents(evts, orchestrator.EventToolResult)

	resultByID := map[string]orchestrator.Event{}
	for _, r := range results {
		resultByID[r.ToolCallID] = r
	}

	require.Contains(t, resultByID, "c-panic",
		"the panicking approved call must emit a tool_result, not crash the process")
	assert.NotEmpty(t, resultByID["c-panic"].ToolError,
		"the panicking approved call must surface as that call's tool_result error")
	require.Contains(t, resultByID, "c-ok", "the sibling approved call must still produce a result")
	assert.Empty(t, resultByID["c-ok"].ToolError, "the sibling approved call must succeed normally")
}
