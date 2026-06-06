package providers

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// defaultOpenRouterBaseURL is the OpenRouter OpenAI-compatible API base URL.
const defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

// OpenRouterProvider implements llm.Provider using OpenRouter's OpenAI-compatible API
type OpenRouterProvider struct {
	openAICompatProvider
}

// newOpenRouterProvider wraps client in the OpenRouter-flavored shared implementation.
func newOpenRouterProvider(client *openai.Client) *OpenRouterProvider {
	return &OpenRouterProvider{openAICompatProvider{
		client:       client,
		providerName: "openrouter",
		errPrefix:    "openrouter",
	}}
}

// NewOpenRouter creates a new OpenRouter provider. Returns nil if apiKey is empty.
func NewOpenRouter(apiKey string) *OpenRouterProvider {
	if apiKey == "" {
		return nil
	}
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = defaultOpenRouterBaseURL
	return newOpenRouterProvider(openai.NewClientWithConfig(cfg))
}

// Name returns the provider identifier
func (p *OpenRouterProvider) Name() string { return "openrouter" }

// HealthCheck verifies the provider is reachable
func (p *OpenRouterProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("openrouter health check: %w", err)
	}
	return nil
}

// ListModels returns available models from OpenRouter
func (p *OpenRouterProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	models, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	result := make([]llm.ModelInfo, 0, len(models.Models))
	for _, m := range models.Models {
		result = append(result, llm.ModelInfo{
			ID:                m.ID,
			Name:              m.ID,
			Provider:          "openrouter",
			SupportsStreaming: true,
		})
	}
	return result, nil
}
