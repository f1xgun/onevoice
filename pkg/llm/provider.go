package llm

import "context"

// Provider defines the interface for LLM service providers.
// Implementations must handle provider-specific API calls, rate limiting,
// and error handling.
type Provider interface {
	// Chat sends a request and waits for complete response.
	// Returns an error if the request fails or times out.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream returns a channel of incremental responses.
	// The channel is closed when the stream completes or encounters an error.
	// Callers should check StreamChunk.Error to detect failures.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)

	// ListModels returns available models from this provider.
	// Results may be cached to reduce API calls.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// HealthCheck verifies the provider is reachable and credentials are valid.
	// Returns an error if the provider is unavailable.
	HealthCheck(ctx context.Context) error

	// Name returns the provider identifier (e.g., "openai", "anthropic", "openrouter").
	Name() string
}
