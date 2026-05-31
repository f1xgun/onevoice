package llm

import (
	"time"

	"github.com/google/uuid"
)

// ToolCallTypeFunction is the wire-format value emitted by OpenAI/OpenRouter
// for function-type tool calls. Centralized so test fixtures and any local
// adapter use the same string instead of re-declaring "function".
const ToolCallTypeFunction = "function"

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
	Type     string       `json:"type"` // ToolCallTypeFunction
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function invocation details.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition defines a tool that can be called by the LLM.
type ToolDefinition struct {
	Type     string             `json:"type"` // ToolCallTypeFunction
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition defines the schema for a callable function.
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}

// SystemBlock is one segment of a multi-block system prompt. SystemBlocks are
// the canonical channel for system content.
//
// CacheBoundary is a hint to providers that this block is the LAST member of a
// cache-eligible prefix. Anthropic stamps `cache_control: ephemeral` on the
// LAST block in the slice whose CacheBoundary is true. Providers that do not
// implement prompt caching (OpenAI, OpenRouter, SelfHosted) silently ignore
// the flag and concatenate every block into one leading `role:"system"`
// message.
type SystemBlock struct {
	Text          string `json:"text"`
	CacheBoundary bool   `json:"cache_boundary,omitempty"`
}

// ChatRequest represents a request to generate a chat completion.
//
// SystemBlocks is the preferred channel for system-prompt content. When
// non-empty, providers consume it directly and assume Messages is system-free.
// When empty, providers fall back to scrubbing `role:"system"` entries out of
// Messages.
type ChatRequest struct {
	UserID         uuid.UUID        `json:"user_id"`                   // Use uuid.Nil for system-level calls
	BusinessID     uuid.UUID        `json:"business_id,omitempty"`     // Router skips billing when uuid.Nil
	ConversationID string           `json:"conversation_id,omitempty"` // Mongo ObjectID hex (forensic queries)
	Model          string           `json:"model"`
	Messages       []Message        `json:"messages"`
	SystemBlocks   []SystemBlock    `json:"system_blocks,omitempty"`
	Tools          []ToolDefinition `json:"tools,omitempty"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Temperature    float64          `json:"temperature,omitempty"`
	TopP           float64          `json:"top_p,omitempty"`
	Stop           []string         `json:"stop,omitempty"`
	RequestID      string           `json:"request_id,omitempty"` // For tracing
	// Routing metadata
	Tier     string   `json:"tier,omitempty"`     // Subscription tier for rate limiting
	Strategy Strategy `json:"strategy,omitempty"` // Routing strategy (default: StrategyCost)
}

// ChatResponse represents the response from a chat completion.
type ChatResponse struct {
	Content      string        `json:"content"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	Usage        TokenUsage    `json:"usage"`
	FinishReason string        `json:"finish_reason"` // "stop", "length", "tool_calls", "content_filter"
	Latency      time.Duration `json:"latency"`
	Provider     string        `json:"provider,omitempty"` // Provider identifier (e.g., "openrouter", "openai")
}

// TokenUsage tracks token consumption for a request.
//
// CacheReadTokens / CacheCreationTokens are populated only by providers that
// surface prompt-cache breakdowns (currently Anthropic; OpenAI/OpenRouter leave
// them zero — OpenAI does not expose a comparable cache surface). InputTokens
// post-cache means "tokens consumed AFTER the last cache breakpoint" — billing
// math must compute total billable input as
//
//	CacheReadTokens*0.1 + CacheCreationTokens*1.25 + InputTokens*1.0
//
// for the 5-minute ephemeral cache TTL.
type TokenUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	TotalTokens         int `json:"total_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
}

// StreamChunk represents an incremental response in a streaming chat completion.
type StreamChunk struct {
	Delta         string      `json:"delta"`                     // Incremental content
	ToolCallDelta *ToolCall   `json:"tool_call_delta,omitempty"` // Incremental tool call
	Usage         *TokenUsage `json:"usage,omitempty"`           // Only in final chunk
	Done          bool        `json:"done"`                      // True if this is the final chunk
	Error         error       `json:"-"`                         // If stream encounters error
}

// ModelInfo describes the capabilities and pricing of an LLM model.
type ModelInfo struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Provider           string   `json:"provider"`
	ContextLength      int      `json:"context_length"`
	InputCostPer1MTok  *float64 `json:"input_cost_per_1m_tok,omitempty"`  // USD per 1M tokens, nil for free models
	OutputCostPer1MTok *float64 `json:"output_cost_per_1m_tok,omitempty"` // USD per 1M tokens, nil for free models
	SupportsToolUse    bool     `json:"supports_tool_use"`
	SupportsStreaming  bool     `json:"supports_streaming"`
	SupportsVision     bool     `json:"supports_vision"`
}

// Strategy defines the routing strategy for provider selection.
type Strategy int

const (
	StrategyCost  Strategy = iota // Minimize cost (default)
	StrategySpeed                 // Minimize latency
)

// CostBreakdown separates provider cost from platform commission
type CostBreakdown struct {
	ProviderCost float64 `json:"provider_cost"` // Actual cost to LLM provider
	Commission   float64 `json:"commission"`    // OneVoice markup
	UserCost     float64 `json:"user_cost"`     // Total charged to user
}
