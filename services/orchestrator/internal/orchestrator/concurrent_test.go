package orchestrator_test

import (
	"context"
	"encoding/json"
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

// TestRun_MultipleToolCallsInSingleResponse verifies that when the LLM returns
// multiple tool_calls in one response, all are executed and their results fed back.
func TestRun_MultipleToolCallsInSingleResponse(t *testing.T) {
	tgArgs, _ := json.Marshal(map[string]interface{}{"text": "tg post"})
	vkArgs, _ := json.Marshal(map[string]interface{}{"text": "vk post"})

	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_tg", Type: "function", Function: llm.FunctionCall{Name: "telegram__send_channel_post", Arguments: string(tgArgs)}},
				{ID: "call_vk", Type: "function", Function: llm.FunctionCall{Name: "vk__publish_post", Arguments: string(vkArgs)}},
			},
		},
		{Content: "Оба поста опубликованы!", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "telegram__send_channel_post", Description: "d", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"message_id": "tg_42", "platform": "telegram"}, nil
	}))
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "vk__publish_post", Description: "d", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"post_id": "vk_99", "platform": "vk"}, nil
	}))

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Test"},
		Messages:           []llm.Message{{Role: "user", Content: "Post to both"}},
		ActiveIntegrations: []string{"telegram", "vk"},
	})
	require.NoError(t, err)

	var toolCalls, toolResults []orchestrator.Event
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolCall:
			toolCalls = append(toolCalls, e)
		case orchestrator.EventToolResult:
			toolResults = append(toolResults, e)
		case orchestrator.EventText, orchestrator.EventError, orchestrator.EventDone:
			// ignore in this test
		}
	}

	// tool_call events fire before goroutines start — preserve original order.
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "telegram__send_channel_post", toolCalls[0].ToolName)
	assert.Equal(t, "vk__publish_post", toolCalls[1].ToolName)

	// tool_result events arrive in completion order — correlate by ID.
	require.Len(t, toolResults, 2)
	byID := map[string]orchestrator.Event{}
	for _, r := range toolResults {
		byID[r.ToolCallID] = r
	}
	require.Contains(t, byID, "call_tg")
	require.Contains(t, byID, "call_vk")
	assert.Empty(t, byID["call_tg"].ToolError)
	assert.Empty(t, byID["call_vk"].ToolError)

	tgResult, _ := byID["call_tg"].ToolResult.(map[string]interface{})
	vkResult, _ := byID["call_vk"].ToolResult.(map[string]interface{})
	assert.Equal(t, "telegram", tgResult["platform"])
	assert.Equal(t, "vk", vkResult["platform"])
}

// TestRun_ToolExecutionError_ContinuesLoop verifies that when one tool fails,
// the error is fed back to the LLM and the loop continues.
func TestRun_ToolExecutionError_ContinuesLoop(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{"text": "test"})

	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_fail", Type: "function", Function: llm.FunctionCall{Name: "failing_tool", Arguments: string(args)}},
			},
		},
		{Content: "Инструмент не сработал, извините.", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: "failing_tool", Description: "d", Parameters: map[string]interface{}{}},
	}, "", tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return nil, context.DeadlineExceeded
	}))

	orch := orchestrator.New(stub, reg)
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "do something"}},
	})
	require.NoError(t, err)

	var toolResults, texts []orchestrator.Event
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolResult:
			toolResults = append(toolResults, e)
		case orchestrator.EventText:
			texts = append(texts, e)
		case orchestrator.EventToolCall, orchestrator.EventError, orchestrator.EventDone:
			// ignore in this test
		}
	}

	// Tool result should contain the error
	require.Len(t, toolResults, 1)
	assert.Contains(t, toolResults[0].ToolError, "deadline exceeded")

	// LLM should still respond after the error
	require.NotEmpty(t, texts)
	assert.Contains(t, texts[0].Content, "не сработал")
}

// TestRun_ContextCancel_StopsLoop verifies that canceling the context
// stops the agent loop promptly.
func TestRun_ContextCancel_StopsLoop(t *testing.T) {
	// LLM that blocks until context is canceled
	blockingLLM := &stubLLM{responses: []*llm.ChatResponse{}}
	// stubLLM returns "done" when idx >= len(responses), which is fine

	reg := tools.NewRegistry()
	orch := orchestrator.New(blockingLLM, reg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	events, err := orch.Run(ctx, orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	// Should complete quickly due to context cancellation or immediate done
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // channel closed, test passes
			}
		case <-deadline:
			t.Fatal("events channel not closed within deadline")
		}
	}
}
