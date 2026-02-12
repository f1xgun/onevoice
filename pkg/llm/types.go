package llm

import (
	"errors"

	"github.com/google/uuid"
)

// Message represents a single message in a conversation.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // For tool response messages
}

// ToolCall represents a function call request from the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function invocation details.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition defines a tool that can be called by the LLM.
type ToolDefinition struct {
	Type     string             `json:"type"` // "function"
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition defines the schema for a callable function.
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}

// ChatRequest represents a request to generate a chat completion.
type ChatRequest struct {
	UserID      *uuid.UUID       `json:"user_id,omitempty"` // Optional for system-level calls
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	Stop        []string         `json:"stop,omitempty"`
	RequestID   string           `json:"request_id,omitempty"` // For tracing
}

// Validate checks if the ChatRequest is valid.
func (r ChatRequest) Validate() error {
	if r.Model == "" {
		return errors.New("model is required")
	}
	if len(r.Messages) == 0 {
		return errors.New("messages cannot be empty")
	}
	if r.Temperature < 0 || r.Temperature > 2.0 {
		return errors.New("temperature must be between 0 and 2.0")
	}
	if r.TopP < 0 || r.TopP > 1.0 {
		return errors.New("top_p must be between 0 and 1.0")
	}
	return nil
}

// ChatResponse represents the response from a chat completion.
type ChatResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"` // "stop", "length", "tool_calls", "content_filter"
	Usage        TokenUsage `json:"usage"`
	Model        string     `json:"model"`
	RequestID    string     `json:"request_id,omitempty"`
	Provider     string     `json:"provider"` // Which provider handled this
}

// HasToolCalls returns true if the response contains tool calls.
func (r ChatResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// TokenUsage tracks token consumption for a request.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// CalculateCost computes the cost in USD for this token usage.
// inputPricing and outputPricing are in USD per 1M tokens.
func (u TokenUsage) CalculateCost(inputPricing, outputPricing float64) float64 {
	inputCost := (float64(u.PromptTokens) / 1_000_000.0) * inputPricing
	outputCost := (float64(u.CompletionTokens) / 1_000_000.0) * outputPricing
	return inputCost + outputCost
}

// StreamChunk represents an incremental response in a streaming chat completion.
type StreamChunk struct {
	Delta        string      `json:"delta"`         // Incremental content
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"` // Incremental tool calls
	FinishReason string      `json:"finish_reason,omitempty"`
	Usage        *TokenUsage `json:"usage,omitempty"` // Only in final chunk
	Error        error       `json:"-"`               // If stream encounters error
}

// IsComplete returns true if this is the final chunk in the stream.
func (s StreamChunk) IsComplete() bool {
	return s.FinishReason != "" || s.Error != nil
}

// ModelInfo describes the capabilities and pricing of an LLM model.
type ModelInfo struct {
	ID              string   `json:"id"`
	Provider        string   `json:"provider"`
	ContextWindow   int      `json:"context_window"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	InputPricing    float64  `json:"input_pricing"`  // USD per 1M tokens
	OutputPricing   float64  `json:"output_pricing"` // USD per 1M tokens
	Capabilities    []string `json:"capabilities"`   // ["chat", "tools", "vision"]
}

// Strategy defines the routing strategy for provider selection.
type Strategy string

const (
	StrategyAuto    Strategy = "auto"    // Smart routing based on load
	StrategyCheap   Strategy = "cheap"   // Lowest cost
	StrategyFast    Strategy = "fast"    // Lowest latency
	StrategyQuality Strategy = "quality" // Best model quality
)

// Valid returns true if the strategy is a recognized value.
func (s Strategy) Valid() bool {
	switch s {
	case StrategyAuto, StrategyCheap, StrategyFast, StrategyQuality:
		return true
	default:
		return false
	}
}
