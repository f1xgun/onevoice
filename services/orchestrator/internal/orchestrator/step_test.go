package orchestrator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/billingclient"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/wire"
)

// --- Mocks ---

// mockPendingRepo records every call so tests can assert ordering.
// All methods are safe for concurrent use.
type mockPendingRepo struct {
	mu sync.Mutex

	// ops is an ordered list of method names invoked, used for
	// pause-ordering assertions (Persist → emit pause event).
	ops []string

	// insertedBatches captures the batch snapshots passed to Persist.
	insertedBatches []*domain.PendingToolCallBatch

	// persistErr lets tests simulate a Mongo failure inside Persist —
	// it short-circuits before the batch is stored so callers see the
	// same outcome as a crash mid-Persist.
	persistErr error

	// Per-batch stored state (for GetByBatchID, MarkDispatched, MarkResolved).
	store map[string]*domain.PendingToolCallBatch
}

func newMockPendingRepo() *mockPendingRepo {
	return &mockPendingRepo{store: make(map[string]*domain.PendingToolCallBatch)}
}

func (m *mockPendingRepo) Persist(_ context.Context, b *domain.PendingToolCallBatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, "Persist")
	if m.persistErr != nil {
		return m.persistErr
	}
	// Real Persist promotes the batch to status=pending before returning;
	// mirror that here so consumers observing GetByBatchID after Persist
	// see the post-promote state.
	b.Status = "pending"
	m.insertedBatches = append(m.insertedBatches, b)
	m.store[b.ID] = b
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

func newRegistryWithFloor(name string, floor domain.ToolFloor, exec toolregistry.Executor) *toolregistry.Registry {
	reg := toolregistry.NewRegistry()
	def := llm.ToolDefinition{
		Type: llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{
			Name:        name,
			Description: "d",
			Parameters:  map[string]interface{}{},
		},
	}
	if exec == nil {
		exec = toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		})
	}
	reg.Register(toolregistry.ToolSpec{Def: def, Floor: floor, EditableFields: []string{"text"}}, exec)
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
	reg := toolregistry.NewRegistry()
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
				Type:     llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{Name: "auto_tool", Arguments: string(toolCallArgs)},
			}},
		},
		{Content: "Done!", FinishReason: "stop"},
	}}

	var executed int32
	reg := newRegistryWithFloor("auto_tool", domain.ToolFloorAuto, toolregistry.ExecutorFunc(
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
				Type:     llm.ToolCallTypeFunction,
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

	// Ordering invariant: Persist → pause event. Persist must complete
	// (status=pending committed) BEFORE the SSE event fires; otherwise a
	// crash between persist and emit leaves an unrecoverable in-flight
	// batch from the user's POV.
	repo.mu.Lock()
	ops := append([]string{}, repo.ops...)
	repo.mu.Unlock()
	require.GreaterOrEqual(t, len(ops), 1, "must call Persist")
	assert.Equal(t, "Persist", ops[0])

	// Pause event emitted
	pauseEvts := findEvents(evts, orchestrator.EventToolApprovalRequired)
	require.Len(t, pauseEvts, 1, "must emit exactly one tool_approval_required event")
	assert.NotEmpty(t, pauseEvts[0].BatchID)
	require.Len(t, pauseEvts[0].Calls, 1)
	assert.Equal(t, "call_m", pauseEvts[0].Calls[0].CallID)
	assert.Equal(t, "manual_tool", pauseEvts[0].Calls[0].ToolName)
	assert.Equal(t, domain.ToolFloorManual, pauseEvts[0].Calls[0].Floor)

	// Batch was persisted with all identity fields (incl. ProjectID
	// threading required by the TOCTOU re-check).
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
				Type:     llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{Name: "manual_tool", Arguments: string(toolCallArgs)},
			}},
		},
	}}

	reg := newRegistryWithFloor("manual_tool", domain.ToolFloorManual, nil)
	repo := newMockPendingRepo()
	repo.persistErr = errors.New("mongo unavailable")

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
				Type:     llm.ToolCallTypeFunction,
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
				{ID: "call_f", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "forbidden_tool", Arguments: string(forbiddenArgs)}},
				{ID: "call_a", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "auto_tool", Arguments: string(autoArgs)}},
			},
		},
		{Content: "Ok done.", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "forbidden_tool", Description: "x", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorForbidden, EditableFields: nil}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return nil, errors.New("must not be called")
	}))
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "auto_tool", Description: "x", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}))

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
				{ID: "call_a", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "auto_t", Arguments: string(autoArgs)}},
				{ID: "call_m", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "manual_t", Arguments: string(manualArgs)}},
			},
		},
	}}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "auto_t", Description: "x", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}))
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "manual_t", Description: "x", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}))

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
	require.Len(t, pauses, 1, "one card per turn for ALL manual calls")
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
				Type:     llm.ToolCallTypeFunction,
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

// --- end-to-end billing smoke ---

// TestStepRun_BillingPostedE2E exercises the full chain from stepRun →
// llm.Router.Chat → goroutine-fired logBilling → pkg/billingclient.LogUsage →
// httptest server. Proves the wiring lands a single UsageLog with the
// correct BusinessID + non-zero cost when BusinessID is set.
//
// We use the real wire.LLMRouter constructor + real pkg/billingclient (not
// a mock) so the option-pass-through + router cost-math + internal billing
// endpoint shape are exercised together. The fake selector returns a fake
// provider whose ChatResponse has known token counts; the assertion pins
// providerCost = inputTokens × in/1e6 + outputTokens × out/1e6.
func TestStepRun_BillingPostedE2E(t *testing.T) {
	t.Setenv("LLM_MODEL", "anthropic/claude-sonnet-4-6")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	// 1. httptest server that captures the POST.
	type captureRec struct {
		mu     sync.Mutex
		bodies [][]byte
	}
	rec := &captureRec{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, body)
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	cfg, err := config.Load()
	require.NoError(t, err)

	// 2. Build pkg/billingclient pointed at the httptest server.
	bc, err := billingclient.New(srv.URL, nil)
	require.NoError(t, err)

	// 3. Build a router with fake provider + WithBilling. The fakeSelector
	// returns a known entry so we control input/output cost.
	fakeProv := &e2eFakeProvider{resp: &llm.ChatResponse{
		Content: "ok", FinishReason: "stop",
		Usage: llm.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}}
	fakeSel := &e2eFakeSelector{entry: &llm.ModelProviderEntry{
		Model: "anthropic/claude-sonnet-4-6", Provider: "openrouter",
		InputCostPer1MTok: 3.00, OutputCostPer1MTok: 15.00,
		HealthStatus: llm.HealthStatusHealthy, Enabled: true,
	}, prov: fakeProv}
	logBuf := &bytes.Buffer{}
	router, err := wire.LLMRouter(cfg, slog.New(slog.NewTextHandler(logBuf, nil)),
		llm.WithBilling(bc),
		llm.WithSelector(fakeSel),
	)
	require.NoError(t, err)

	// 4. Run stepRun via the orchestrator with a real BusinessID.
	bizID := uuid.New()
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(router, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		BusinessID:      bizID.String(),
		ConversationID:  "conv-e2e-1",
		Model:           "anthropic/claude-sonnet-4-6",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	// 5. Wait for the fire-and-forget goroutine to land its POST.
	require.Eventually(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.bodies) >= 1
	}, 2*time.Second, 10*time.Millisecond, "billing POST never landed")

	rec.mu.Lock()
	bodies := append([][]byte{}, rec.bodies...)
	rec.mu.Unlock()
	require.Len(t, bodies, 1, "expected exactly one billing POST per Chat call")

	// 6. Decode + assert the row carries the BusinessID and a real cost.
	// providerCost = (100 × 3) / 1e6 + (50 × 15) / 1e6 = 3e-4 + 7.5e-4 = 1.05e-3
	var got llm.UsageLog
	require.NoError(t, json.Unmarshal(bodies[0], &got))
	assert.Equal(t, bizID, got.BusinessID)
	assert.Equal(t, 100, got.InputTokens)
	assert.Equal(t, 50, got.OutputTokens)
	assert.InDelta(t, 1.05e-3, got.ProviderCostUSD, 1e-9,
		"provider_cost_usd = inputTokens × in/1e6 + outputTokens × out/1e6 = 1.05e-3")
}

// TestStepRun_BillingSkippedWhenBusinessIDNil — router's nil-guard invariant
// exercised through the full e2e path: empty state.BusinessID MUST result
// in zero POSTs to the billing endpoint.
func TestStepRun_BillingSkippedWhenBusinessIDNil(t *testing.T) {
	t.Setenv("LLM_MODEL", "anthropic/claude-sonnet-4-6")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	type captureRec struct {
		mu    sync.Mutex
		count int
	}
	rec := &captureRec{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rec.mu.Lock()
		rec.count++
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	cfg, err := config.Load()
	require.NoError(t, err)
	bc, err := billingclient.New(srv.URL, nil)
	require.NoError(t, err)
	fakeProv := &e2eFakeProvider{resp: &llm.ChatResponse{
		Content: "ok", FinishReason: "stop",
		Usage: llm.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}}
	fakeSel := &e2eFakeSelector{entry: &llm.ModelProviderEntry{
		Model: "anthropic/claude-sonnet-4-6", Provider: "openrouter",
		InputCostPer1MTok: 3.00, OutputCostPer1MTok: 15.00,
		HealthStatus: llm.HealthStatusHealthy, Enabled: true,
	}, prov: fakeProv}
	logBuf := &bytes.Buffer{}
	router, err := wire.LLMRouter(cfg, slog.New(slog.NewTextHandler(logBuf, nil)),
		llm.WithBilling(bc),
		llm.WithSelector(fakeSel),
	)
	require.NoError(t, err)

	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(router, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		BusinessID:      "", // intentionally empty
		Model:           "anthropic/claude-sonnet-4-6",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	// Wait a beat so any pending goroutine has time to misbehave.
	time.Sleep(200 * time.Millisecond)

	rec.mu.Lock()
	got := rec.count
	rec.mu.Unlock()
	assert.Equal(t, 0, got, "uuid.Nil BusinessID must NOT produce a billing POST (router nil-guard)")
}

// --- e2e fakes (separate from the unit-level fakes elsewhere) ---

type e2eFakeProvider struct {
	resp *llm.ChatResponse
}

func (f *e2eFakeProvider) Name() string { return "openrouter" }
func (f *e2eFakeProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return f.resp, nil
}
func (f *e2eFakeProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, nil
}
func (f *e2eFakeProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}
func (f *e2eFakeProvider) HealthCheck(_ context.Context) error { return nil }

type e2eFakeSelector struct {
	entry *llm.ModelProviderEntry
	prov  llm.Provider
}

func (f *e2eFakeSelector) Pick(_ string, _ llm.Strategy) (*llm.ModelProviderEntry, llm.Provider, error) {
	return f.entry, f.prov, nil
}
func (f *e2eFakeSelector) Candidates(_ string, _ llm.Strategy) []llm.Candidate {
	if f.entry == nil {
		return nil
	}
	return []llm.Candidate{{Entry: f.entry, Provider: f.prov}}
}
func (f *e2eFakeSelector) Record(_ *llm.ModelProviderEntry, _ llm.Outcome) {}

// TestBuildPendingBatch_PopulatesFloorAtPauseManual verifies that every
// PendingCall persisted at orchestrator pause time must carry
// FloorAtPause=ToolFloorManual so the resolve-time TOCTOU re-check can
// consult the same registry that classified the call at pause (eliminating
// divergence with the api-side ToolsRegistryCache, which is HTTP-backed and
// lazily warmed). We exercise this via the public Run path (buildPendingBatch
// is package-private) and assert on repo.insertedBatches.
// --- BusinessID propagation tests ---

// capturingLLM records every ChatRequest it receives so tests can assert on
// BusinessID + ConversationID propagation through stepRun.
type capturingLLM struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	resp     *llm.ChatResponse
}

func (c *capturingLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	if c.resp != nil {
		return c.resp, nil
	}
	return &llm.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
}

func (c *capturingLLM) lastRequest(t *testing.T) llm.ChatRequest {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		t.Fatalf("capturingLLM.Chat was never called")
	}
	return c.requests[len(c.requests)-1]
}

// TestStepRun_PropagatesBusinessID — when state.BusinessID is a valid UUID
// string, the ChatRequest the orchestrator dispatches MUST carry the parsed
// uuid.UUID in BusinessID so logBilling stamps the right business on every
// usage_logs row.
func TestStepRun_PropagatesBusinessID(t *testing.T) {
	bizID := uuid.New()
	rec := &capturingLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(rec, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		BusinessID:      bizID.String(),
		ConversationID:  "conv-x",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)
	assert.Equal(t, bizID, got.BusinessID, "stepRun must thread state.BusinessID into ChatRequest.BusinessID")
}

// TestStepRun_PropagatesConversationID — ChatRequest.ConversationID lands
// the conversation's Mongo ObjectID hex from RunState so usage_logs rows can
// be forensically grouped per-chat-turn.
func TestStepRun_PropagatesConversationID(t *testing.T) {
	rec := &capturingLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(rec, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		BusinessID:      uuid.New().String(),
		ConversationID:  "conv-forensic-123",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)
	assert.Equal(t, "conv-forensic-123", got.ConversationID)
}

// TestStepRun_MalformedBusinessID_ParsesToNil — fail-closed behavior: a
// non-UUID state.BusinessID (from a compromised orchestrator state OR a bug
// elsewhere in the chain) MUST degrade to uuid.Nil so the router's nil-guard
// skips the billing POST. Loss of one row is preferable to writing a corrupt
// row.
func TestStepRun_MalformedBusinessID_ParsesToNil(t *testing.T) {
	rec := &capturingLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(rec, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		BusinessID:      "not-a-uuid",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)
	assert.Equal(t, uuid.Nil, got.BusinessID,
		"malformed BusinessID must parse to uuid.Nil so router skips billing (fail-closed)")
}

// TestStepRun_EmptyBusinessID_ParsesToNil — empty BusinessID maps to
// uuid.Nil same as malformed: legacy callers + system contexts continue
// to work, just without a billing row.
func TestStepRun_EmptyBusinessID_ParsesToNil(t *testing.T) {
	rec := &capturingLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(rec, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		BusinessID:      "",
	})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)
	assert.Equal(t, uuid.Nil, got.BusinessID)
}

func TestBuildPendingBatch_PopulatesFloorAtPauseManual(t *testing.T) {
	manualArgs1, _ := json.Marshal(map[string]interface{}{"text": "hi"})
	manualArgs2, _ := json.Marshal(map[string]interface{}{"text": "yo"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "tc-1", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "telegram_post", Arguments: string(manualArgs1)}},
				{ID: "tc-2", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "vk_post", Arguments: string(manualArgs2)}},
			},
		},
	}}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "telegram_post", Description: "x", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}))
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "vk_post", Description: "x", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorManual, EditableFields: []string{"text"}}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}))

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

// ---------------------------------------------------------------------
// Conversation token cap tests
// ---------------------------------------------------------------------

// makeToolCallResponse builds a synthetic LLM response that requests one auto
// tool call. The Usage values are stamped on so the cap accumulator can drive.
func makeToolCallResponse(toolName string, inputTokens, outputTokens int) *llm.ChatResponse {
	args, _ := json.Marshal(map[string]interface{}{"text": "go"})
	return &llm.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{{
			ID:       "call_" + toolName,
			Type:     llm.ToolCallTypeFunction,
			Function: llm.FunctionCall{Name: toolName, Arguments: string(args)},
		}},
		Usage: llm.TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens},
	}
}

// TestStepRun_ConversationCap_PreIter — three iters of 20k tokens each, cap
// 50k → blocks on iter 3.
func TestStepRun_ConversationCap_PreIter(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		makeToolCallResponse("auto_tool", 20000, 0),
		makeToolCallResponse("auto_tool", 20000, 0),
		makeToolCallResponse("auto_tool", 20000, 0),
	}}
	reg := newRegistryWithFloor("auto_tool", domain.ToolFloorAuto, nil)
	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations:        10,
		ConversationInputCap: 50000,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "conversation_token_cap", errs[0].Code)
	assert.NotEmpty(t, errs[0].Content)

	// Exactly three Chat calls were made (sums to 60k > 50k on the third).
	assert.Equal(t, 3, stub.idx, "must make 3 LLM calls before tripping the cap")
}

// TestStepRun_ConversationCap_MidIter — single response carries 60k input.
func TestStepRun_ConversationCap_MidIter(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		makeToolCallResponse("auto_tool", 60000, 0),
	}}
	reg := newRegistryWithFloor("auto_tool", domain.ToolFloorAuto, nil)
	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations:        10,
		ConversationInputCap: 50000,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)
	evts := drainEvents(events)

	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "conversation_token_cap", errs[0].Code)
	assert.Equal(t, 1, stub.idx, "must trip on first iter — no second LLM call")
	assert.Empty(t, findEvents(evts, orchestrator.EventToolCall),
		"tool calls must NOT be dispatched once the cap fires")
}

// TestStepRun_ConversationCap_OutputAxis — cap on the output axis is
// independent of input.
func TestStepRun_ConversationCap_OutputAxis(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		makeToolCallResponse("auto_tool", 1000, 11000),
	}}
	reg := newRegistryWithFloor("auto_tool", domain.ToolFloorAuto, nil)
	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations:         10,
		ConversationInputCap:  50000,
		ConversationOutputCap: 10000,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)
	evts := drainEvents(events)

	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "conversation_token_cap", errs[0].Code)
}

// TestStepRun_ConversationCap_Disabled — caps at 0 means the loop runs to
// MaxIterations as before.
func TestStepRun_ConversationCap_Disabled(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "ok", FinishReason: "stop", Usage: llm.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}},
	}}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations: 10,
		// caps unset (zero) — gate disabled.
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	evts := drainEvents(events)
	assert.NotEmpty(t, findEvents(evts, orchestrator.EventDone))
	assert.Empty(t, findEvents(evts, orchestrator.EventError))
}

// TestStepRun_ConversationCap_FriendlyMessageRussian — default locale = RU.
func TestStepRun_ConversationCap_FriendlyMessageRussian(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "stop", FinishReason: "stop", Usage: llm.TokenUsage{InputTokens: 100000}},
	}}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations:        5,
		ConversationInputCap: 50000,
	})
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	errs := findEvents(drainEvents(events), orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "conversation_token_cap", errs[0].Code)
	assert.Contains(t, errs[0].Content, "лимита токенов")
}

// TestStepRun_ConversationCap_FriendlyMessageEnglish — locale=en switches the
// fallback content string.
func TestStepRun_ConversationCap_FriendlyMessageEnglish(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "stop", FinishReason: "stop", Usage: llm.TokenUsage{InputTokens: 100000}},
	}}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations:        5,
		ConversationInputCap: 50000,
	})
	// language.English wins over middleware default.
	ctx := i18nWithEnglish(t)
	events, err := orch.Run(ctx, orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	errs := findEvents(drainEvents(events), orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "conversation_token_cap", errs[0].Code)
	assert.Contains(t, errs[0].Content, "token limit")
}

// i18nWithEnglish injects language.English into a fresh context for the
// English-locale cap tests. Kept private to step_test.go so the helper does
// not leak into production code.
func i18nWithEnglish(t *testing.T) context.Context {
	t.Helper()
	return i18n.WithLocale(context.Background(), language.English)
}

// erroringLLM returns the canned err from every Chat call. Used to drive the
// translateChatError code paths.
type erroringLLM struct{ err error }

func (e *erroringLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, e.err
}

// TestStepRun_DailySpendExceeded_FriendlyEvent — when the Router surfaces
// ErrDailySpendExceeded, step.go translates it to an SSE error event with
// the machine-readable code.
func TestStepRun_DailySpendExceeded_FriendlyEvent(t *testing.T) {
	stub := &erroringLLM{err: llm.ErrDailySpendExceeded}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(stub, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "daily_spend_exceeded", errs[0].Code)
}

// TestStepRun_RateLimitUnavailable_FriendlyEvent — parallel coverage for the
// infra-failure sentinel.
func TestStepRun_RateLimitUnavailable_FriendlyEvent(t *testing.T) {
	stub := &erroringLLM{err: llm.ErrRateLimitUnavailable}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(stub, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	evts := drainEvents(events)
	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1)
	assert.Equal(t, "rate_limit_unavailable", errs[0].Code)
}
