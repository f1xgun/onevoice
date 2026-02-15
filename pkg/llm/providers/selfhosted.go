package providers

import (
	openai "github.com/sashabaranov/go-openai"
)

// SelfHostedProvider implements llm.Provider for any OpenAI-compatible inference server.
type SelfHostedProvider struct {
	client *openai.Client
	name   string
}

// NewSelfHosted creates a provider pointing at baseURL.
// apiKey is optional — pass "" if the server requires no authentication.
// name must be unique (e.g. "selfhosted-0") to distinguish multiple endpoints in the router.
func NewSelfHosted(name, baseURL, apiKey string) *SelfHostedProvider {
	key := apiKey
	if key == "" {
		key = "none" // go-openai requires a non-empty string; most servers ignore it
	}
	cfg := openai.DefaultConfig(key)
	cfg.BaseURL = baseURL
	return &SelfHostedProvider{
		client: openai.NewClientWithConfig(cfg),
		name:   name,
	}
}

// Name returns the unique provider identifier set at construction time.
func (p *SelfHostedProvider) Name() string { return p.name }
