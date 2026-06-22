package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
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
	reg := toolregistry.NewRegistry()
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
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{
					Name:      "get_business_info",
					Arguments: string(toolCallArgs),
				},
			}},
		},
		{Content: "Вот информация о бизнесе", FinishReason: "stop"},
	}}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "get_business_info", Description: "get info", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
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

	var toolEvents, toolResultEvents, textEvents []orchestrator.Event
	for e := range events {
		switch e.Type {
		case orchestrator.EventToolCall:
			toolEvents = append(toolEvents, e)
		case orchestrator.EventToolResult:
			toolResultEvents = append(toolResultEvents, e)
		case orchestrator.EventText:
			textEvents = append(textEvents, e)
		case orchestrator.EventError, orchestrator.EventDone:
		case orchestrator.EventToolApprovalRequired, orchestrator.EventToolRejected:
		}
	}

	assert.Len(t, toolEvents, 1, "expected one tool call event")
	assert.Equal(t, "get_business_info", toolEvents[0].ToolName)
	assert.Equal(t, "call_1", toolEvents[0].ToolCallID, "tool_call_id must be propagated to EventToolCall")
	assert.Len(t, toolResultEvents, 1, "expected one tool result event")
	assert.Equal(t, "call_1", toolResultEvents[0].ToolCallID, "tool_call_id must match between EventToolCall and EventToolResult")
	assert.NotEmpty(t, textEvents, "expected text response after tool execution")
}

func TestRun_MaxIterations_Stops(t *testing.T) {
	stub := &stubLLM{}
	for i := 0; i < 15; i++ {
		args, _ := json.Marshal(map[string]interface{}{})
		stub.responses = append(stub.responses, &llm.ChatResponse{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_loop",
				Type:     llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{Name: "get_business_info", Arguments: string(args)},
			}},
		})
	}

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "get_business_info", Description: "d", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorAuto, EditableFields: nil}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
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
	assert.Equal(t, "max_iterations", errorEvents[0].Code)
	assert.NotEmpty(t, errorEvents[0].Content)
	assert.NotContains(t, errorEvents[0].Content, "max iterations",
		"user-facing content must not leak the raw English error string")
}
