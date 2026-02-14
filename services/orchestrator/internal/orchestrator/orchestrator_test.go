package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubLLM returns canned responses in order.
type stubLLM struct {
	responses []*llm.ChatResponse
	idx       int
}

func (s *stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if s.idx >= len(s.responses) {
		return &llm.ChatResponse{Content: "done", FinishReason: "stop"}, nil
	}
	resp := s.responses[s.idx]
	s.idx++
	return resp, nil
}

func TestRun_TextResponse_EmitsTextEvent(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "Привет! Чем могу помочь?", FinishReason: "stop"},
	}}
	reg := tools.NewRegistry()
	biz := prompt.BusinessContext{Name: "Кофейня"}
	orch := orchestrator.New(stub, reg)

	req := orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: biz,
		Messages:        []llm.Message{{Role: "user", Content: "Привет"}},
	}

	events, err := orch.Run(context.Background(), req)
	require.NoError(t, err)

	var texts []string
	for e := range events {
		if e.Type == orchestrator.EventText {
			texts = append(texts, e.Content)
		}
	}
	assert.NotEmpty(t, texts)
}

func TestRun_ToolCall_ExecutesToolAndLoops(t *testing.T) {
	toolCallArgs, _ := json.Marshal(map[string]interface{}{"message": "hello"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "get_business_info",
					Arguments: string(toolCallArgs),
				},
			}},
		},
		{Content: "Вот информация о бизнесе", FinishReason: "stop"},
	}}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDefinition{Name: "get_business_info", Description: "get info", Parameters: map[string]interface{}{}},
	}, tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"name": "Кофейня Уют"}, nil
	}))

	biz := prompt.BusinessContext{Name: "Кофейня"}
	orch := orchestrator.New(stub, reg)

	req := orchestrator.RunRequest{
		UserID:          uuid.New(),
		BusinessContext: biz,
		Messages:        []llm.Message{{Role: "user", Content: "Расскажи о бизнесе"}},
	}

	events, err := orch.Run(context.Background(), req)
	require.NoError(t, err)

	var toolEvents, textEvents []orchestrator.Event
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolCall:
			toolEvents = append(toolEvents, e)
		case orchestrator.EventText:
			textEvents = append(textEvents, e)
		}
	}

	assert.Len(t, toolEvents, 1, "expected one tool call event")
	assert.Equal(t, "get_business_info", toolEvents[0].ToolName)
	assert.NotEmpty(t, textEvents, "expected text response after tool execution")
}

func TestRun_MaxIterations_Stops(t *testing.T) {
	stub := &stubLLM{}
	// Always return a tool call — should stop at max iterations
	for i := 0; i < 15; i++ {
		args, _ := json.Marshal(map[string]interface{}{})
		stub.responses = append(stub.responses, &llm.ChatResponse{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_loop",
				Type: "function",
				Function: llm.FunctionCall{Name: "get_business_info", Arguments: string(args)},
			}},
		})
	}

	reg := tools.NewRegistry()
	reg.Register(llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDefinition{Name: "get_business_info", Description: "d", Parameters: map[string]interface{}{}},
	}, tools.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}))

	orch := orchestrator.NewWithOptions(stub, reg, orchestrator.Options{MaxIterations: 3})
	req := orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "loop"}},
	}

	events, err := orch.Run(context.Background(), req)
	require.NoError(t, err)

	var errorEvents []orchestrator.Event
	for e := range events {
		if e.Type == orchestrator.EventError {
			errorEvents = append(errorEvents, e)
		}
	}
	assert.Len(t, errorEvents, 1)
	assert.Contains(t, errorEvents[0].Content, "max iterations")
}
