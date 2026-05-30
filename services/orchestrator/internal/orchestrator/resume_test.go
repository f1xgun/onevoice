package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Contains(t, errs[0].Content, "batch not found")
}

func TestResume_BatchExpired_EmitsError(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "done", FinishReason: "stop"}}}
	reg, _ := registryWithRecording(t, "manual_tool", domain.ToolFloorManual)
	repo := newMockPendingRepo()
	// Pre-insert an expired batch
	expired := &domain.PendingToolCallBatch{ID: "batch-x", Status: "expired"}
	repo.store["batch-x"] = expired

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-x"})
	require.NoError(t, err)

	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "approval_expired", errs[0].Content)
}

func TestResume_AllApproved_DispatchesInParallel(t *testing.T) {
	// Stub LLM follows up with text-only after resume finishes dispatch
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

	// Wait for both tool_result events
	gotResults := 0
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			gotResults++
		}
	}
	elapsed := time.Since(start)

	assert.Equal(t, 2, gotResults)
	// Parallel: each call takes 100ms; sequential would be 200ms+.
	// Give a generous budget to avoid flakes on slow CI.
	assert.Less(t, elapsed, 180*time.Millisecond, "parallel dispatch must complete in less than 2×delay")

	// Both calls dispatched with approval_id = <batch_id>-<call_id>
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

	// Executor MUST NOT be called
	assert.Equal(t, int32(0), atomic.LoadInt32(&dispatched),
		"rejected calls MUST NOT be dispatched")

	// tool_rejected event emitted with reason
	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, "off-brand", rejects[0].Content)
	assert.Equal(t, "c-rej", rejects[0].ToolCallID)
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
	// FRESH approval map: business flipped the tool to forbidden
	// AFTER the batch was persisted — the TOCTOU re-check must see this
	// and drop the call with a policy_revoked synthetic rejection.
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
			Dispatched: true, // crash-recovery: already dispatched in prior attempt
		},
	})
	repo.store["batch-d"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-d"})
	require.NoError(t, err)

	evts := drainEvents(events)

	assert.Equal(t, int32(0), atomic.LoadInt32(&dispatched),
		"already-dispatched call MUST NOT be re-executed")
	// No tool_call event emitted either
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

	// Drain events
	for range events {
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.calls, 1)
	// Merge preserves un-edited keys: channel_id stays, text is overwritten
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

	// Drain events
	for range events {
	}

	ids := rec.approvalIDs()
	require.Len(t, ids, 1)
	assert.Equal(t, "batch-1-call-a", ids[0])
}

func TestResume_CompletesAndContinuesStepRun_ToDone(t *testing.T) {
	// After tool executes, LLM responds with text-only → Done
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

	// tool_call + tool_result + text + done all emitted
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

	// Exactly 1 dispatch, 1 rejection
	assert.Equal(t, 1, rec.callCount())
	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, "nope", rejects[0].Content)
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
	// LLM returns 15k input on the resumed call. Total 40k + 15k = 55k > 50k
	// cap → error fires on the first iteration after resume.
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
	// Legacy V1 raw-array shape — no accumulated fields.
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

	// 5k from the new call sits under the 50k cap → no error.
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
	// stubLLM is never reached because stepRun runs after dispatch returns;
	// even if it is, providing a benign response keeps the assertion local
	// to the dispatch behavior.
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}}

	// 64 approved calls > 32-slot SSE buffer; each call emits tool_call +
	// tool_result so naive sends would block well before drain completes.
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

	// Simulate caller hang-up: cancel immediately and DO NOT drain `events`.
	// The 32-slot buffer will fill quickly; per-call goroutines then must
	// observe ctx.Done() rather than block on the send.
	cancel()

	// The producer goroutine must close `events` within a generous bound.
	// If the leak regressed, this would hang forever and the test would
	// time out at the package timeout — assert closure explicitly so the
	// failure is obvious.
	closed := make(chan struct{})
	go func() {
		// Drain whatever events were already buffered so the channel can be
		// closed by the producer; we don't care about the content here.
		for range events {
		}
		close(closed)
	}()

	select {
	case <-closed:
		// Producer closed `events` promptly after cancellation — fix works.
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
