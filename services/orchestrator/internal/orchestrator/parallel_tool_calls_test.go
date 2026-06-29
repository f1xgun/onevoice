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

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
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
				{ID: "call_yb", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.YandexBusinessGetReviews, Arguments: string(argsA)}},
				{ID: "call_tg", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.TelegramGetReviews, Arguments: string(argsB)}},
				{ID: "call_vk", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.VKGetComments, Arguments: string(argsC)}},
			},
		},
		{Content: "Собрал отзывы со всех трёх платформ.", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	slow := toolregistry.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(toolDelay):
			return map[string]interface{}{"reviews": []interface{}{}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: tools.YandexBusinessGetReviews}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, slow)
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: tools.TelegramGetReviews}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, slow)
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: tools.VKGetComments}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, slow)

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
		case orchestrator.EventToolApprovalRequired, orchestrator.EventToolRejected:
		}
	}
	elapsed := time.Since(start)

	require.Len(t, toolCallEvents, toolCount, "expected 3 tool_call events")
	require.Len(t, toolResultEvents, toolCount, "expected 3 tool_result events")

	maxParallel := 2 * toolDelay
	assert.Lessf(t, elapsed, maxParallel,
		"tools ran serially: elapsed=%s, expected <%s (parallel) not ~%s (serial)",
		elapsed, maxParallel, toolCount*toolDelay)

	assert.Equal(t, "call_yb", toolCallEvents[0].ToolCallID)
	assert.Equal(t, "call_tg", toolCallEvents[1].ToolCallID)
	assert.Equal(t, "call_vk", toolCallEvents[2].ToolCallID)

	resultIDs := map[string]bool{}
	for _, ev := range toolResultEvents {
		resultIDs[ev.ToolCallID] = true
		assert.Empty(t, ev.ToolError, "no tool errors expected")
	}
	assert.True(t, resultIDs["call_yb"])
	assert.True(t, resultIDs["call_tg"])
	assert.True(t, resultIDs["call_vk"])
}

// TestRun_ParallelToolCalls_ResultsEmitAsTheyFinish asserts that each tool's
// tool_result arrives on the SSE channel as soon as THAT tool finishes — not
// after the slowest one in the batch completes. Without this, every task on
// the UI shows the batch's max duration (the symptom the user caught in prod).
func TestRun_ParallelToolCalls_ResultsEmitAsTheyFinish(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{})
	const fastDelay = 100 * time.Millisecond
	const slowDelay = 600 * time.Millisecond

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_slow", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "slow_tool", Arguments: string(args)}},
				{ID: "call_fast", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "fast_tool", Arguments: string(args)}},
			},
		},
		{Content: "ok", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "slow_tool"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(slowDelay):
			return map[string]interface{}{"ok": true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "fast_tool"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(fastDelay):
			return map[string]interface{}{"ok": true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))

	orch := orchestrator.New(stub, reg)

	start := time.Now()
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)

	resultArrivedAt := make(map[string]time.Duration)
	for e := range events {
		if e.Type == orchestrator.EventToolResult {
			resultArrivedAt[e.ToolCallID] = time.Since(start)
		}
	}

	require.Len(t, resultArrivedAt, 2)

	gap := resultArrivedAt["call_slow"] - resultArrivedAt["call_fast"]
	assert.Greaterf(t, gap, (slowDelay-fastDelay)/2,
		"tool_results batched together: fast=%s slow=%s gap=%s (expected >%s)",
		resultArrivedAt["call_fast"], resultArrivedAt["call_slow"], gap, (slowDelay-fastDelay)/2)

	assert.Less(t, resultArrivedAt["call_fast"], slowDelay,
		"fast tool result arrived as late as the slow tool: %s", resultArrivedAt["call_fast"])
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
				{ID: "call_ok_a", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "tool_ok_a", Arguments: string(args)}},
				{ID: "call_fail", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "tool_fail", Arguments: string(args)}},
				{ID: "call_ok_b", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "tool_ok_b", Arguments: string(args)}},
			},
		},
		{Content: "ok", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	okExec := toolregistry.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(100 * time.Millisecond):
			return map[string]interface{}{"ok": true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	failExec := toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return nil, errors.New("boom")
	})
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "tool_ok_a"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, okExec)
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "tool_fail"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, failExec)
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "tool_ok_b"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, okExec)

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
				{ID: "call_a", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.TelegramSendChannelPost, Arguments: string(argsA)}},
				{ID: "call_b", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.TelegramSendChannelPost, Arguments: string(argsB)}},
			},
		},
		{Content: "оба опубликованы", FinishReason: "stop"},
	}}

	var counter atomic.Int32
	exec := toolregistry.ExecutorFunc(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
		n := counter.Add(1)
		return map[string]interface{}{"message_id": n, "text": args["text"]}, nil
	})

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: tools.TelegramSendChannelPost}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, exec)

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Test"},
		Messages:           []llm.Message{{Role: "user", Content: "пост"}},
		ActiveIntegrations: []string{"telegram"},
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
		case orchestrator.EventToolApprovalRequired, orchestrator.EventToolRejected:
		}
	}

	require.Len(t, calls, 2)
	require.Len(t, results, 2)

	assert.Equal(t, "call_a", calls[0].ToolCallID)
	assert.Equal(t, "call_b", calls[1].ToolCallID)
	assert.Equal(t, "первый", calls[0].ToolArgs["text"])
	assert.Equal(t, "второй", calls[1].ToolArgs["text"])

	resultByID := map[string]orchestrator.Event{}
	for _, r := range results {
		resultByID[r.ToolCallID] = r
	}
	assert.Contains(t, resultByID, "call_a")
	assert.Contains(t, resultByID, "call_b")
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
				{ID: "call_hang", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "hang", Arguments: string(args)}},
				{ID: "call_fast", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "fast", Arguments: string(args)}},
			},
		},
		{Content: "done", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "hang"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "fast"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
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

	assert.Less(t, elapsed, time.Second, "expected per-tool timeout to bound wall time, got %s", elapsed)

	require.Len(t, results, 2)
	assert.Contains(t, results["call_hang"].ToolError, "deadline exceeded")
	assert.Empty(t, results["call_fast"].ToolError)
}

// TestRun_ToolPanic_RecoversToErrorEvent asserts that a panic inside one tool's
// executor in a parallel batch is contained to THAT tool — the run completes,
// emits a tool_result error event for the panicking call, and the sibling tools
// still produce their normal results. Without the per-goroutine recover in
// dispatchToolCalls the panic propagates uncaught and aborts the whole process
// (the parent-goroutine recoverToError cannot catch a child-goroutine panic).
func TestRun_ToolPanic_RecoversToErrorEvent(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{})

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_ok_a", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "tool_ok_a", Arguments: string(args)}},
				{ID: "call_panic", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "tool_panic", Arguments: string(args)}},
				{ID: "call_ok_b", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: "tool_ok_b", Arguments: string(args)}},
			},
		},
		{Content: "ok", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	okExec := toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})
	panicExec := toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		panic("boom in tool executor")
	})
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "tool_ok_a"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, okExec)
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "tool_panic"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, panicExec)
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{Name: "tool_ok_b"}}, Floor: domain.ToolFloorAuto, EditableFields: nil}, okExec)

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

	require.Len(t, results, 3, "the run must complete and emit a tool_result for every call, including the panicking one")
	assert.NotEmpty(t, results["call_panic"].ToolError,
		"the panicking tool must surface as that call's tool_result error, not crash the process")
	assert.Empty(t, results["call_ok_a"].ToolError, "sibling tools must still produce their normal results")
	assert.Empty(t, results["call_ok_b"].ToolError, "sibling tools must still produce their normal results")
}
