package providers

import (
	"context"
	"net/http"

	openai "github.com/sashabaranov/go-openai"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// dataLoggingHeader is the Yandex AI Studio request header that controls whether
// the platform persists the request payload on its side. AI Studio logs every
// request by default; sending "false" opts the request out so prompt contents
// (which carry business and customer personal data) are not retained by the
// provider. Other OpenAI-compatible servers ignore the unknown header, so the
// self-hosted provider sends it unconditionally.
const dataLoggingHeader = "x-data-logging-enabled"

// dataLoggingDisabled is the header value that turns provider-side request
// logging off.
const dataLoggingDisabled = "false"

// SelfHostedProvider implements llm.Provider for any OpenAI-compatible inference server.
type SelfHostedProvider struct {
	openAICompatProvider
}

// NewSelfHosted creates a provider pointing at baseURL.
// Returns nil if name or baseURL is empty.
// apiKey is optional — pass "" if the server requires no authentication.
// name must be unique (e.g. "selfhosted-0") to distinguish multiple endpoints in the router.
//
// Every request carries x-data-logging-enabled: false so a Yandex AI Studio
// endpoint does not retain the prompt payload (152-ФЗ data minimisation for
// the personal data that flows through the prompts).
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
	cfg.HTTPClient = &http.Client{Transport: newPrivacyHeaderTransport(http.DefaultTransport)}
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

// privacyHeaderTransport stamps the provider-side logging opt-out header on
// every outgoing request before handing it to the wrapped RoundTripper.
type privacyHeaderTransport struct {
	base http.RoundTripper
}

func newPrivacyHeaderTransport(base http.RoundTripper) *privacyHeaderTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &privacyHeaderTransport{base: base}
}

// RoundTrip clones the request (RoundTrippers must not mutate the caller's
// request) and sets the opt-out header unless the caller already chose a value.
func (t *privacyHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	if cloned.Header.Get(dataLoggingHeader) == "" {
		cloned.Header.Set(dataLoggingHeader, dataLoggingDisabled)
	}
	return t.base.RoundTrip(cloned)
}
