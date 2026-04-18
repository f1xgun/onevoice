package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// safeStubLLM is the goroutine-safe variant of stubLLM. The orchestrator calls
// Chat sequentially, but -race still flags unsynchronized field access, so we
// guard idx with a mutex here.
type safeStubLLM struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	idx       int
}

func (s *safeStubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.responses) {
		return &llm.ChatResponse{Content: "done", FinishReason: "stop"}, nil
	}
	resp := s.responses[s.idx]
	s.idx++
	return resp, nil
}

// TestRun_ParallelToolCalls_WallTime asserts that three slow tools dispatched
// in one LLM response run concurrently — total wall time ≈ max(per-tool) rather
// than sum(per-tool). This is the regression guarded against the original bug
// (correlation 40be6df2-..., yandex 67s serialized the other two into a ctx-
// cancel).
func TestRun_ParallelToolCalls_WallTime(t *testing.T) {
	const toolDelay = 300 * time.Millisecond
	const toolCount = 3

	argsA, _ := json.Marshal(map[string]interface{}{"limit": 5})
	argsB, _ := json.Marshal(map[string]interface{}{"limit": 5})
	argsC, _ := json.Marshal(map[string]interface{}{"limit": 5})

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_yb", Type: "function", Function: llm.FunctionCall{Name: "yandex_business__get_reviews", Arguments: string(argsA)}},
				{ID: "call_tg", Type: "function", Function: llm.FunctionCall{Name: "telegram__get_reviews", Arguments: string(argsB)}},
				{ID: "call_vk", Type: "function", Function: llm.FunctionCall{Name: "vk__get_comments", Arguments: string(argsC)}},
			},
		},
		{Content: "Собрал отзывы со всех трёх платформ.", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	slow := tools.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(toolDelay):
			return map[string]interface{}{"reviews": []interface{}{}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "yandex_business__get_reviews"}}, "", slow)
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "telegram__get_reviews"}}, "", slow)
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "vk__get_comments"}}, "", slow)

	orch := orchestrator.New(stub, reg)

	start := time.Now()
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Test"},
		Messages:           []llm.Message{{Role: "user", Content: "Проверить отзывы"}},
		ActiveIntegrations: []string{"telegram", "vk", "yandex_business"},
	})
	require.NoError(t, err)

	var toolCallEvents, toolResultEvents []orchestrator.Event
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolCall:
			toolCallEvents = append(toolCallEvents, e)
		case orchestrator.EventToolResult:
			toolResultEvents = append(toolResultEvents, e)
		case orchestrator.EventText, orchestrator.EventError, orchestrator.EventDone:
		}
	}
	elapsed := time.Since(start)

	require.Len(t, toolCallEvents, toolCount, "expected 3 tool_call events")
	require.Len(t, toolResultEvents, toolCount, "expected 3 tool_result events")

	// Wall time must be much closer to toolDelay than to toolCount*toolDelay.
	// Allow slack for scheduling; <2x toolDelay proves parallelism (serial would be ~3x).
	maxParallel := 2 * toolDelay
	assert.Lessf(t, elapsed, maxParallel,
		"tools ran serially: elapsed=%s, expected <%s (parallel) not ~%s (serial)",
		elapsed, maxParallel, toolCount*toolDelay)

	// Tool_call events carry the LLM-issued tool_call_id.
	assert.Equal(t, "call_yb", toolCallEvents[0].ToolCallID)
	assert.Equal(t, "call_tg", toolCallEvents[1].ToolCallID)
	assert.Equal(t, "call_vk", toolCallEvents[2].ToolCallID)

	// tool_result events must be in the ORIGINAL order — OpenAI/Anthropic
	// require role:tool messages to match the original assistant.tool_calls
	// order for the next iteration.
	assert.Equal(t, "call_yb", toolResultEvents[0].ToolCallID)
	assert.Equal(t, "call_tg", toolResultEvents[1].ToolCallID)
	assert.Equal(t, "call_vk", toolResultEvents[2].ToolCallID)

	for _, ev := range toolResultEvents {
		assert.Empty(t, ev.ToolError, "no tool errors expected")
	}
}

// TestRun_ParallelToolCalls_OneFailsOthersSucceed asserts that errgroup-style
// "cancel siblings on first error" is NOT what we do — one failing tool must
// not cancel the in-flight peers. This is critical when telegram flakes but
// yandex+vk are already producing useful data.
func TestRun_ParallelToolCalls_OneFailsOthersSucceed(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{})

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_ok_a", Type: "function", Function: llm.FunctionCall{Name: "tool_ok_a", Arguments: string(args)}},
				{ID: "call_fail", Type: "function", Function: llm.FunctionCall{Name: "tool_fail", Arguments: string(args)}},
				{ID: "call_ok_b", Type: "function", Function: llm.FunctionCall{Name: "tool_ok_b", Arguments: string(args)}},
			},
		},
		{Content: "ok", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	okExec := tools.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(100 * time.Millisecond):
			return map[string]interface{}{"ok": true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	failExec := tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return nil, errors.New("boom")
	})
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "tool_ok_a"}}, "", okExec)
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "tool_fail"}}, "", failExec)
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "tool_ok_b"}}, "", okExec)

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "do three"}},
	})
	require.NoError(t, err)

	results := make(map[string]orchestrator.Event)
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			results[e.ToolCallID] = e
		}
	}

	require.Len(t, results, 3)
	assert.Empty(t, results["call_ok_a"].ToolError)
	assert.Equal(t, "boom", results["call_fail"].ToolError)
	assert.Empty(t, results["call_ok_b"].ToolError)
}

// TestRun_DuplicateToolName_CorrelatesByID asserts that calling the same tool
// twice in one batch produces two independent tool_call / tool_result pairs,
// each keyed by the LLM's tool_call_id. The old proxy correlated by tool name,
// which would collapse duplicates — this test pins the new contract.
func TestRun_DuplicateToolName_CorrelatesByID(t *testing.T) {
	argsA, _ := json.Marshal(map[string]interface{}{"text": "первый"})
	argsB, _ := json.Marshal(map[string]interface{}{"text": "второй"})

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_a", Type: "function", Function: llm.FunctionCall{Name: "telegram__send_channel_post", Arguments: string(argsA)}},
				{ID: "call_b", Type: "function", Function: llm.FunctionCall{Name: "telegram__send_channel_post", Arguments: string(argsB)}},
			},
		},
		{Content: "оба опубликованы", FinishReason: "stop"},
	}}

	var counter atomic.Int32
	exec := tools.ExecutorFunc(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
		n := counter.Add(1)
		return map[string]interface{}{"message_id": n, "text": args["text"]}, nil
	})

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "telegram__send_channel_post"}}, "", exec)

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "пост"}},
	})
	require.NoError(t, err)

	var calls, results []orchestrator.Event
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolCall:
			calls = append(calls, e)
		case orchestrator.EventToolResult:
			results = append(results, e)
		case orchestrator.EventText, orchestrator.EventError, orchestrator.EventDone:
		}
	}

	require.Len(t, calls, 2)
	require.Len(t, results, 2)
	assert.Equal(t, "call_a", calls[0].ToolCallID)
	assert.Equal(t, "call_b", calls[1].ToolCallID)
	assert.Equal(t, "call_a", results[0].ToolCallID)
	assert.Equal(t, "call_b", results[1].ToolCallID)
	assert.Equal(t, "первый", calls[0].ToolArgs["text"])
	assert.Equal(t, "второй", calls[1].ToolArgs["text"])
}

// TestRun_PerToolTimeout_BoundsSingleTool asserts that when ToolExecTimeout is
// set, a hanging tool fails fast with a deadline error instead of blocking the
// whole batch indefinitely.
func TestRun_PerToolTimeout_BoundsSingleTool(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{})

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_hang", Type: "function", Function: llm.FunctionCall{Name: "hang", Arguments: string(args)}},
				{ID: "call_fast", Type: "function", Function: llm.FunctionCall{Name: "fast", Arguments: string(args)}},
			},
		},
		{Content: "done", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "hang"}}, "",
		tools.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}))
	reg.Register(llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{Name: "fast"}}, "",
		tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		}))

	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{
		MaxIterations:   3,
		ToolExecTimeout: 150 * time.Millisecond,
	})

	start := time.Now()
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	results := make(map[string]orchestrator.Event)
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			results[e.ToolCallID] = e
		}
	}
	elapsed := time.Since(start)

	// Should not hang — total time bounded by the per-tool timeout (plus some slack).
	assert.Less(t, elapsed, time.Second, "expected per-tool timeout to bound wall time, got %s", elapsed)

	require.Len(t, results, 2)
	assert.Contains(t, results["call_hang"].ToolError, "deadline exceeded")
	assert.Empty(t, results["call_fast"].ToolError)
}
