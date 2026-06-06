package providers

import (
	"context"

	openai "github.com/sashabaranov/go-openai"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// SelfHostedProvider implements llm.Provider for any OpenAI-compatible inference server.
type SelfHostedProvider struct {
	openAICompatProvider
}

// NewSelfHosted creates a provider pointing at baseURL.
// Returns nil if name or baseURL is empty.
// apiKey is optional — pass "" if the server requires no authentication.
// name must be unique (e.g. "selfhosted-0") to distinguish multiple endpoints in the router.
func NewSelfHosted(name, baseURL, apiKey string) *SelfHostedProvider {
	if name == "" || baseURL == "" {
		return nil
	}
	key := apiKey
	if key == "" {
		key = "none"
	}
	cfg := openai.DefaultConfig(key)
	cfg.BaseURL = baseURL
	return &SelfHostedProvider{openAICompatProvider{
		client:       openai.NewClientWithConfig(cfg),
		providerName: name,
		errPrefix:    "selfhosted",
	}}
}

// Name returns the unique provider identifier set at construction time.
func (p *SelfHostedProvider) Name() string { return p.providerName }

// HealthCheck always returns nil — self-hosted servers may not support /v1/models.
func (p *SelfHostedProvider) HealthCheck(_ context.Context) error { return nil }

// ListModels returns empty — model discovery is not reliable on self-hosted servers.
func (p *SelfHostedProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}
