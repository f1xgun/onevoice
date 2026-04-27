package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// --- Mocks ---

// mockPendingRepo records every call so tests can assert ordering.
// All methods are safe for concurrent use.
type mockPendingRepo struct {
	mu sync.Mutex

	// ops is an ordered list of method names invoked, used for
	// two-phase-write ordering assertions (Insert → Promote → emit).
	ops []string

	// insertedBatches captures the batch snapshots passed to InsertPreparing.
	insertedBatches []*domain.PendingToolCallBatch

	// Configurable failures — tests set these to simulate crashes between
	// pause phases.
	insertErr  error
	promoteErr error

	// Per-batch stored state (for GetByBatchID, MarkDispatched, MarkResolved).
	store map[string]*domain.PendingToolCallBatch
}

func newMockPendingRepo() *mockPendingRepo {
	return &mockPendingRepo{store: make(map[string]*domain.PendingToolCallBatch)}
}

func (m *mockPendingRepo) InsertPreparing(_ context.Context, b *domain.PendingToolCallBatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "InsertPreparing")
	if m.insertErr != nil {
		return m.insertErr
	}
	// Copy pointer (callers don't mutate after insert in the hot path).
	m.insertedBatches = append(m.insertedBatches, b)
	m.store[b.ID] = b
	return nil
}

func (m *mockPendingRepo) PromoteToPending(_ context.Context, batchID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "PromoteToPending")
	if m.promoteErr != nil {
		return m.promoteErr
	}
	if b, ok := m.store[batchID]; ok {
		b.Status = "pending"
	}
	return nil
}

func (m *mockPendingRepo) GetByBatchID(_ context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.store[batchID]
	if !ok {
		return nil, domain.ErrBatchNotFound
	}
	return b, nil
}

func (m *mockPendingRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return nil, nil
}

func (m *mockPendingRepo) AtomicTransitionToResolving(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockPendingRepo) RecordDecisions(_ context.Context, _ string, _ []domain.PendingCall) error {
	return nil
}

func (m *mockPendingRepo) MarkDispatched(_ context.Context, batchID, callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "MarkDispatched:"+batchID+":"+callID)
	return nil
}

func (m *mockPendingRepo) MarkResolved(_ context.Context, batchID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "MarkResolved:"+batchID)
	return nil
}

func (m *mockPendingRepo) MarkExpired(_ context.Context, _ string) error { return nil }

func (m *mockPendingRepo) ReconcileOrphanPreparing(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// Satisfy the full interface — this mock is used by both step_test.go and
// resume_test.go. If interface drifts, compile fails here first.
var _ domain.PendingToolCallRepository = (*mockPendingRepo)(nil)

// --- Registry helpers ---

func newRegistryWithFloor(name string, floor domain.ToolFloor, exec tools.Executor) *tools.Registry {
	reg := tools.NewRegistry()
	def := llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDefinition{
			Name:        name,
			Description: "d",
			Parameters:  map[string]interface{}{},
		},
	}
	if exec == nil {
		exec = tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		})
	}
	reg.Register(def, "", exec, floor, []string{"text"})
	return reg
}

// Helper to drain all events from a channel into a slice.
func drainEvents(ch <-chan orchestrator.Event) []orchestrator.Event {
	var evts []orchestrator.Event
	for e := range ch {
		evts = append(evts, e)
	}
	return evts
}

// findEvents returns all events of the given type.
func findEvents(evts []orchestrator.Event, t orchestrator.EventType) []orchestrator.Event {
	var out []orchestrator.Event
	for _, e := range evts {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// ---- Tests ----

func TestStepRun_NoToolCalls_ReturnsDoneWithText(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "Привет!", FinishReason: "stop"},
	}}
	reg := tools.NewRegistry()
	orch := orchestrator.New(stub, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	texts := findEvents(evts, orchestrator.EventText)
	dones := findEvents(evts, orchestrator.EventDone)
	require.Len(t, texts, 1)
	assert.Equal(t, "Привет!", texts[0].Content)
	require.Len(t, dones, 1)
}

func TestStepRun_AutoFloorTool_DispatchesInline(t *testing.T) {
	toolCallArgs, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_a",
				Type:     "function",
				Function: llm.FunctionCall{Name: "auto_tool", Arguments: string(toolCallArgs)},
			}},
		},
		{Content: "Done!", FinishReason: "stop"},
	}}

	var executed int32
	reg := newRegistryWithFloor("auto_tool", domain.ToolFloorAuto, tools.ExecutorFunc(
		func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
			executed = 1
			return map[string]interface{}{"ok": true}, nil
		}))

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	assert.Equal(t, int32(1), executed, "auto tool must be executed inline")
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventToolCall))
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventToolResult))
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventDone))
}

func TestStepRun_ManualFloorTool_PersistsBatchAndReturnsPaused(t *testing.T) {
	toolCallArgs, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_m",
				Type:     "function",
				Function: llm.FunctionCall{Name: "manual_tool", Arguments: string(toolCallArgs)},
			}},
		},
	}}

	reg := newRegistryWithFloor("manual_tool", domain.ToolFloorManual, nil)
	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "post it"}},
		ConversationID:  "conv-1",
		BusinessID:      "biz-1",
		ProjectID:       "proj-1",
		UserIDString:    "user-1",
		MessageID:       "msg-1",
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	// Ordering invariant: InsertPreparing → PromoteToPending → pause event.
	// Two-phase persist must complete BEFORE the SSE event.
	repo.mu.Lock()
	ops := append([]string{}, repo.ops...)
	repo.mu.Unlock()
	require.GreaterOrEqual(t, len(ops), 2, "must call InsertPreparing + PromoteToPending")
	assert.Equal(t, "InsertPreparing", ops[0])
	assert.Equal(t, "PromoteToPending", ops[1])

	// Pause event emitted
	pauseEvts := findEvents(evts, orchestrator.EventToolApprovalRequired)
	require.Len(t, pauseEvts, 1, "must emit exactly one tool_approval_required event")
	assert.NotEmpty(t, pauseEvts[0].BatchID)
	require.Len(t, pauseEvts[0].Calls, 1)
	assert.Equal(t, "call_m", pauseEvts[0].Calls[0].CallID)
	assert.Equal(t, "manual_tool", pauseEvts[0].Calls[0].ToolName)
	assert.Equal(t, domain.ToolFloorManual, pauseEvts[0].Calls[0].Floor)

	// Batch was persisted with all identity fields (incl. ProjectID — D-30
	// threading required by Plan 16-07's TOCTOU re-check).
	require.Len(t, repo.insertedBatches, 1)
	b := repo.insertedBatches[0]
	assert.Equal(t, "conv-1", b.ConversationID)
	assert.Equal(t, "biz-1", b.BusinessID)
	assert.Equal(t, "proj-1", b.ProjectID)
	assert.Equal(t, "user-1", b.UserID)
	assert.Equal(t, "msg-1", b.MessageID)
	require.Len(t, b.Calls, 1)
	assert.Equal(t, "call_m", b.Calls[0].CallID)

	// No done/error event after pause (goroutine exited, OutcomePaused)
	assert.Empty(t, findEvents(evts, orchestrator.EventDone))
	assert.Empty(t, findEvents(evts, orchestrator.EventError))
}

func TestStepRun_ManualFloor_PersistFails_EmitsErrorAndDoesNotEmitPauseEvent(t *testing.T) {
	toolCallArgs, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_m",
				Type:     "function",
				Function: llm.FunctionCall{Name: "manual_tool", Arguments: string(toolCallArgs)},
			}},
		},
	}}

	reg := newRegistryWithFloor("manual_tool", domain.ToolFloorManual, nil)
	repo := newMockPendingRepo()
	repo.insertErr = errors.New("mongo unavailable")

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Content, "failed to persist approval batch")
	assert.Empty(t, findEvents(evts, orchestrator.EventToolApprovalRequired),
		"MUST NOT emit pause event when persist failed (Pitfall 1/3)")
}

func TestStepRun_BusinessRaisesAutoToManual_PausesCorrectly(t *testing.T) {
	toolCallArgs, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_r",
				Type:     "function",
				Function: llm.FunctionCall{Name: "raisable_tool", Arguments: string(toolCallArgs)},
			}},
		},
	}}

	// Registry says auto; business flips it to manual.
	reg := newRegistryWithFloor("raisable_tool", domain.ToolFloorAuto, nil)
	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
		BusinessApprovals: map[string]domain.ToolFloor{
			"raisable_tool": domain.ToolFloorManual,
		},
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	pauses := findEvents(evts, orchestrator.EventToolApprovalRequired)
	require.Len(t, pauses, 1, "strictest-wins resolver must classify auto+biz=manual as manual")
	assert.Equal(t, domain.ToolFloorManual, pauses[0].Calls[0].Floor)
}

func TestStepRun_ForbiddenTool_SynthesizesRejection_AndContinues(t *testing.T) {
	forbiddenArgs, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	autoArgs, _ := json.Marshal(map[string]interface{}{"text": "ok"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_f", Type: "function", Function: llm.FunctionCall{Name: "forbidden_tool", Arguments: string(forbiddenArgs)}},
				{ID: "call_a", Type: "function", Function: llm.FunctionCall{Name: "auto_tool", Arguments: string(autoArgs)}},
			},
		},
		{Content: "Ok done.", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "forbidden_tool", Description: "x", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return nil, errors.New("must not be called")
	}), domain.ToolFloorForbidden, nil)
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "auto_tool", Description: "x", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}), domain.ToolFloorAuto, nil)

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "do both"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	// Forbidden emits tool_rejected
	rejections := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejections, 1)
	assert.Equal(t, "forbidden_tool", rejections[0].ToolName)
	assert.Equal(t, "call_f", rejections[0].ToolCallID)
	assert.Equal(t, "policy_forbidden", rejections[0].Content)

	// Auto still executed, text still arrived, outcome == done
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventToolCall))
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventToolResult))
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventDone))
}

func TestStepRun_MixedAutoAndManual_PausesAfterAutoComplete(t *testing.T) {
	autoArgs, _ := json.Marshal(map[string]interface{}{"text": "ok"})
	manualArgs, _ := json.Marshal(map[string]interface{}{"text": "approve_me"})

	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_a", Type: "function", Function: llm.FunctionCall{Name: "auto_t", Arguments: string(autoArgs)}},
				{ID: "call_m", Type: "function", Function: llm.FunctionCall{Name: "manual_t", Arguments: string(manualArgs)}},
			},
		},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "auto_t", Description: "x", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}), domain.ToolFloorAuto, nil)
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "manual_t", Description: "x", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}), domain.ToolFloorManual, []string{"text"})

	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "do both"}},
		ConversationID:  "conv-mix",
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	// Auto tool executed first (tool_call + tool_result emitted)
	toolCalls := findEvents(evts, orchestrator.EventToolCall)
	toolResults := findEvents(evts, orchestrator.EventToolResult)
	require.Len(t, toolCalls, 1, "only the auto tool should emit tool_call (manual tools go through approval card)")
	require.Len(t, toolResults, 1)
	assert.Equal(t, "auto_t", toolCalls[0].ToolName)

	// Then manual pause
	pauses := findEvents(evts, orchestrator.EventToolApprovalRequired)
	require.Len(t, pauses, 1, "HITL-02: one card per turn for ALL manual calls")
	require.Len(t, pauses[0].Calls, 1)
	assert.Equal(t, "manual_t", pauses[0].Calls[0].ToolName)

	// Outcome must be paused — no done event
	assert.Empty(t, findEvents(evts, orchestrator.EventDone))
}

func TestStepRun_NilPendingRepo_ManualFloor_EmitsConfigError(t *testing.T) {
	toolCallArgs, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_m",
				Type:     "function",
				Function: llm.FunctionCall{Name: "manual_tool", Arguments: string(toolCallArgs)},
			}},
		},
	}}

	reg := newRegistryWithFloor("manual_tool", domain.ToolFloorManual, nil)
	// No pendingRepo — use plain New (nil repo)
	orch := orchestrator.New(stub, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.True(t, strings.Contains(errs[0].Content, "HITL not configured"),
		"nil pendingRepo + manual-floor must emit EventError 'HITL not configured'")
}

// TestBuildPendingBatch_PopulatesFloorAtPauseManual — Plan 17-11 / GAP-04.
// Every PendingCall persisted at orchestrator pause time must carry
// FloorAtPause=ToolFloorManual so the resolve-time TOCTOU re-check can
// consult the same registry that classified the call at pause (eliminating
// divergence with the api-side ToolsRegistryCache, which is HTTP-backed and
// lazily warmed). We exercise this via the public Run path (buildPendingBatch
// is package-private) and assert on repo.insertedBatches.
func TestBuildPendingBatch_PopulatesFloorAtPauseManual(t *testing.T) {
	manualArgs1, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	manualArgs2, _ := json.Marshal(map[string]interface{}{"text": "yo"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "tc-1", Type: "function", Function: llm.FunctionCall{Name: "telegram_post", Arguments: string(manualArgs1)}},
				{ID: "tc-2", Type: "function", Function: llm.FunctionCall{Name: "vk_post", Arguments: string(manualArgs2)}},
			},
		},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "telegram_post", Description: "x", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}), domain.ToolFloorManual, []string{"text"})
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "vk_post", Description: "x", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}), domain.ToolFloorManual, []string{"text"})

	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "post both"}},
		ConversationID:  "conv-fap",
		BusinessID:      "biz-fap",
		UserIDString:    "user-fap",
		MessageID:       "msg-fap",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	require.Len(t, repo.insertedBatches, 1, "must persist exactly one batch")
	b := repo.insertedBatches[0]
	require.Len(t, b.Calls, 2, "two manual calls must be persisted")
	for i, c := range b.Calls {
		assert.Equal(t, domain.ToolFloorManual, c.FloorAtPause,
			"Calls[%d].FloorAtPause = %q, want %q", i, c.FloorAtPause, domain.ToolFloorManual)
	}
}
