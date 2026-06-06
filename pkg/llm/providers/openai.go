package providers

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// OpenAIProvider implements llm.Provider using the official OpenAI API
type OpenAIProvider struct {
	openAICompatProvider
}

// newOpenAIProvider wraps client in the OpenAI-flavored shared implementation.
func newOpenAIProvider(client *openai.Client) *OpenAIProvider {
	return &OpenAIProvider{openAICompatProvider{
		client:       client,
		providerName: "openai",
		errPrefix:    "openai",
	}}
}

// NewOpenAI creates a new OpenAI provider. Returns nil if apiKey is empty.
func NewOpenAI(apiKey string) *OpenAIProvider {
	if apiKey == "" {
		return nil
	}
	return newOpenAIProvider(openai.NewClient(apiKey))
}

// Name returns the provider identifier
func (p *OpenAIProvider) Name() string { return "openai" }

// HealthCheck verifies the provider is reachable
func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("openai health check: %w", err)
	}
	return nil
}

// ListModels returns available models from OpenAI
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	models, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	result := make([]llm.ModelInfo, 0, len(models.Models))
	for _, m := range models.Models {
		result = append(result, llm.ModelInfo{
			ID:                m.ID,
			Name:              m.ID,
			Provider:          "openai",
			SupportsStreaming: true,
			SupportsToolUse:   true,
		})
	}
	return result, nil
}
