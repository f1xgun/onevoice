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

func TestChatRequest_Validate(t *testing.T) {
	validUserID := uuid.New()

	t.Run("valid request", func(t *testing.T) {
		req := ChatRequest{
			UserID: &validUserID,
			Model:  "gpt-4",
			Messages: []Message{
				{Role: "user", Content: "Hello"},
			},
		}

		err := req.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty model", func(t *testing.T) {
		req := ChatRequest{
			UserID: &validUserID,
			Model:  "",
			Messages: []Message{
				{Role: "user", Content: "Hello"},
			},
		}

		err := req.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model")
	})

	t.Run("empty messages", func(t *testing.T) {
		req := ChatRequest{
			UserID:   &validUserID,
			Model:    "gpt-4",
			Messages: []Message{},
		}

		err := req.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "messages")
	})

	t.Run("nil userID allowed for system calls", func(t *testing.T) {
		req := ChatRequest{
			UserID: nil,
			Model:  "gpt-4",
			Messages: []Message{
				{Role: "user", Content: "Hello"},
			},
		}

		err := req.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid temperature", func(t *testing.T) {
		req := ChatRequest{
			UserID:      &validUserID,
			Model:       "gpt-4",
			Messages:    []Message{{Role: "user", Content: "Hello"}},
			Temperature: 2.5, // > 2.0
		}

		err := req.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "temperature")
	})

	t.Run("invalid top_p", func(t *testing.T) {
		req := ChatRequest{
			UserID:   &validUserID,
			Model:    "gpt-4",
			Messages: []Message{{Role: "user", Content: "Hello"}},
			TopP:     1.5, // > 1.0
		}

		err := req.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "top_p")
	})
}

func TestTokenUsage_CalculateCost(t *testing.T) {
	t.Run("calculates cost correctly", func(t *testing.T) {
		usage := TokenUsage{
			PromptTokens:     1000,
			CompletionTokens: 500,
			TotalTokens:      1500,
		}

		// $10/1M input, $30/1M output
		cost := usage.CalculateCost(10.0, 30.0)
		expected := (1000.0 / 1_000_000.0 * 10.0) + (500.0 / 1_000_000.0 * 30.0)
		assert.InDelta(t, expected, cost, 0.0001)
	})

	t.Run("zero tokens", func(t *testing.T) {
		usage := TokenUsage{}
		cost := usage.CalculateCost(10.0, 30.0)
		assert.Equal(t, 0.0, cost)
	})
}

func TestStrategy_String(t *testing.T) {
	tests := []struct {
		strategy Strategy
		expected string
	}{
		{StrategyAuto, "auto"},
		{StrategyCheap, "cheap"},
		{StrategyFast, "fast"},
		{StrategyQuality, "quality"},
	}

	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.strategy))
		})
	}
}

func TestStrategy_Valid(t *testing.T) {
	t.Run("valid strategies", func(t *testing.T) {
		validStrategies := []Strategy{
			StrategyAuto,
			StrategyCheap,
			StrategyFast,
			StrategyQuality,
		}

		for _, s := range validStrategies {
			assert.True(t, s.Valid())
		}
	})

	t.Run("invalid strategy", func(t *testing.T) {
		invalid := Strategy("invalid")
		assert.False(t, invalid.Valid())
	})
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

func TestChatResponse_HasToolCalls(t *testing.T) {
	t.Run("with tool calls", func(t *testing.T) {
		resp := ChatResponse{
			ToolCalls: []ToolCall{
				{ID: "call_1", Type: "function"},
			},
		}
		assert.True(t, resp.HasToolCalls())
	})

	t.Run("without tool calls", func(t *testing.T) {
		resp := ChatResponse{
			Content: "Hello",
		}
		assert.False(t, resp.HasToolCalls())
	})
}

func TestStreamChunk_IsComplete(t *testing.T) {
	t.Run("complete chunk with finish reason", func(t *testing.T) {
		chunk := StreamChunk{
			FinishReason: "stop",
		}
		assert.True(t, chunk.IsComplete())
	})

	t.Run("incomplete chunk", func(t *testing.T) {
		chunk := StreamChunk{
			Delta: "Hello",
		}
		assert.False(t, chunk.IsComplete())
	})

	t.Run("error chunk is complete", func(t *testing.T) {
		chunk := StreamChunk{
			Error: assert.AnError,
		}
		assert.True(t, chunk.IsComplete())
	})
}
