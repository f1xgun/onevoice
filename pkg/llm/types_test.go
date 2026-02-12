package llm

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessage_JSON(t *testing.T) {
	t.Run("simple user message", func(t *testing.T) {
		msg := Message{
			Role:    "user",
			Content: "Hello, world!",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var decoded Message
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, msg.Role, decoded.Role)
		assert.Equal(t, msg.Content, decoded.Content)
	})

	t.Run("assistant message with tool calls", func(t *testing.T) {
		msg := Message{
			Role:    "assistant",
			Content: "I'll help you with that.",
			ToolCalls: []ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: FunctionCall{
						Name:      "get_weather",
						Arguments: `{"location":"San Francisco"}`,
					},
				},
			},
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var decoded Message
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, msg.Role, decoded.Role)
		assert.Len(t, decoded.ToolCalls, 1)
		assert.Equal(t, "call_123", decoded.ToolCalls[0].ID)
		assert.Equal(t, "get_weather", decoded.ToolCalls[0].Function.Name)
	})

	t.Run("tool response message", func(t *testing.T) {
		msg := Message{
			Role:       "tool",
			Content:    `{"temperature":72}`,
			ToolCallID: "call_123",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var decoded Message
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, "tool", decoded.Role)
		assert.Equal(t, "call_123", decoded.ToolCallID)
	})
}

func TestChatRequest_UserID(t *testing.T) {
	validUserID := uuid.New()

	t.Run("valid request with userID", func(t *testing.T) {
		req := ChatRequest{
			UserID: validUserID,
			Model:  "gpt-4",
			Messages: []Message{
				{Role: "user", Content: "Hello"},
			},
		}

		assert.Equal(t, validUserID, req.UserID)
	})

	t.Run("valid request with zero userID for system calls", func(t *testing.T) {
		req := ChatRequest{
			UserID: uuid.Nil,
			Model:  "gpt-4",
			Messages: []Message{
				{Role: "user", Content: "Hello"},
			},
		}

		assert.Equal(t, uuid.Nil, req.UserID)
	})
}

func TestTokenUsage_Fields(t *testing.T) {
	t.Run("stores token counts correctly", func(t *testing.T) {
		usage := TokenUsage{
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
		}

		assert.Equal(t, 1000, usage.InputTokens)
		assert.Equal(t, 500, usage.OutputTokens)
		assert.Equal(t, 1500, usage.TotalTokens)
	})
}

func TestStrategy_String(t *testing.T) {
	tests := []struct {
		strategy Strategy
		expected string
	}{
		{StrategyCost, "cost"},
		{StrategySpeed, "speed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.strategy))
		})
	}
}

func TestToolDefinition_JSON(t *testing.T) {
	t.Run("marshals and unmarshals correctly", func(t *testing.T) {
		tool := ToolDefinition{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				},
			},
		}

		data, err := json.Marshal(tool)
		require.NoError(t, err)

		var decoded ToolDefinition
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, "function", decoded.Type)
		assert.Equal(t, "get_weather", decoded.Function.Name)
		assert.NotNil(t, decoded.Function.Parameters)
	})
}

func TestChatResponse_Fields(t *testing.T) {
	t.Run("stores all required fields", func(t *testing.T) {
		usage := TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
		resp := ChatResponse{
			Content:      "Hello",
			ToolCalls:    []ToolCall{{ID: "call_1", Type: "function"}},
			FinishReason: "stop",
			Usage:        usage,
			Latency:      100 * 1000 * 1000, // 100ms in nanoseconds
			RawResponse:  map[string]interface{}{"raw": "data"},
		}

		assert.Equal(t, "Hello", resp.Content)
		assert.Len(t, resp.ToolCalls, 1)
		assert.Equal(t, "stop", resp.FinishReason)
		assert.Equal(t, 100, resp.Usage.InputTokens)
		assert.Equal(t, 100*1000*1000, int(resp.Latency))
		assert.NotNil(t, resp.RawResponse)
	})
}

func TestStreamChunk_Fields(t *testing.T) {
	t.Run("stores all required fields", func(t *testing.T) {
		toolCall := &ToolCall{ID: "call_1", Type: "function"}
		usage := &TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
		chunk := StreamChunk{
			Delta:         "Hello",
			ToolCallDelta: toolCall,
			FinishReason:  "stop",
			Usage:         usage,
			Done:          true,
			Error:         nil,
		}

		assert.Equal(t, "Hello", chunk.Delta)
		assert.NotNil(t, chunk.ToolCallDelta)
		assert.Equal(t, "call_1", chunk.ToolCallDelta.ID)
		assert.Equal(t, "stop", chunk.FinishReason)
		assert.NotNil(t, chunk.Usage)
		assert.True(t, chunk.Done)
		assert.NoError(t, chunk.Error)
	})
}

func TestModelInfo_Fields(t *testing.T) {
	t.Run("stores all required fields with pointers for pricing", func(t *testing.T) {
		inputCost := 10.0
		outputCost := 30.0
		info := ModelInfo{
			ID:                 "gpt-4",
			Provider:           "openai",
			ContextLength:      8192,
			InputCostPer1MTok:  &inputCost,
			OutputCostPer1MTok: &outputCost,
			SupportsToolUse:    true,
			SupportsStreaming:  true,
			SupportsVision:     false,
		}

		assert.Equal(t, "gpt-4", info.ID)
		assert.Equal(t, "openai", info.Provider)
		assert.Equal(t, 8192, info.ContextLength)
		assert.NotNil(t, info.InputCostPer1MTok)
		assert.Equal(t, 10.0, *info.InputCostPer1MTok)
		assert.NotNil(t, info.OutputCostPer1MTok)
		assert.Equal(t, 30.0, *info.OutputCostPer1MTok)
		assert.True(t, info.SupportsToolUse)
		assert.True(t, info.SupportsStreaming)
		assert.False(t, info.SupportsVision)
	})

	t.Run("allows nil pricing for free models", func(t *testing.T) {
		info := ModelInfo{
			ID:                 "llama-2",
			Provider:           "local",
			ContextLength:      4096,
			InputCostPer1MTok:  nil,
			OutputCostPer1MTok: nil,
			SupportsToolUse:    false,
			SupportsStreaming:  true,
			SupportsVision:     false,
		}

		assert.Nil(t, info.InputCostPer1MTok)
		assert.Nil(t, info.OutputCostPer1MTok)
	})
}
