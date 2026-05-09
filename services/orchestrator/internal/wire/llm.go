// Package wire owns the construction of the orchestrator's runtime
// dependencies (LLM router, Mongo connection, tool registry, HTTP handlers)
// so that cmd/main.go stays at the SC-05 ≤200-LOC budget. Each function in
// this package is a pure factory: it takes plain inputs (config, logger,
// connections) and returns the live instance — no global state, no init().
package wire

import (
	"fmt"
	"log/slog"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

// LLMRouter constructs the LLM Router with every provider whose API key is
// set in cfg, plus any SELF_HOSTED_N_* endpoints. At least one provider key
// must be present — otherwise returns an error so the orchestrator fails
// loudly at boot rather than serving requests with no LLM backend.
//
// Mirrors the historical block at services/orchestrator/cmd/main.go:60-66
// (registry creation + buildProviderOpts + NewRouter call). The provider
// builder helper is colocated here as a private function so the wiring stays
// self-contained — services/api has its own copy because the two services
// are separate Go modules and cross-module imports would force a new shared
// package without payoff.
func LLMRouter(cfg *config.Config, log *slog.Logger) (*llm.Router, error) {
	registry := llm.NewRegistry()
	opts := buildProviderOpts(cfg, registry, log)
	if len(opts) == 0 {
		return nil, fmt.Errorf("no LLM provider API key set — set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY")
	}
	return llm.NewRouter(registry, opts...), nil
}

// buildProviderOpts creates RouterOptions for every API key that is set in
// config, and registers the LLM model → provider mapping in the registry for
// each. Returns at least one option if any key is set, nil if none.
func buildProviderOpts(cfg *config.Config, reg *llm.Registry, log *slog.Logger) []llm.RouterOption {
	type providerSpec struct {
		name    string
		apiKey  string
		factory func(string) llm.Provider
	}

	specs := []providerSpec{
		{"openrouter", cfg.OpenRouterAPIKey, func(k string) llm.Provider { return providers.NewOpenRouter(k) }},
		{"openai", cfg.OpenAIAPIKey, func(k string) llm.Provider { return providers.NewOpenAI(k) }},
		{"anthropic", cfg.AnthropicAPIKey, func(k string) llm.Provider { return providers.NewAnthropic(k) }},
	}

	opts := make([]llm.RouterOption, 0, len(specs)+len(cfg.SelfHostedEndpoints))
	for _, spec := range specs {
		if spec.apiKey == "" {
			continue
		}
		p := spec.factory(spec.apiKey)
		opts = append(opts, llm.WithProvider(p))
		reg.RegisterModelProvider(&llm.ModelProviderEntry{
			Model:        cfg.LLMModel,
			Provider:     spec.name,
			HealthStatus: "healthy",
			Enabled:      true,
		})
		log.Info("LLM provider registered", "provider", spec.name, "model", cfg.LLMModel)
	}

	// Wire self-hosted endpoints
	for i, ep := range cfg.SelfHostedEndpoints {
		name := fmt.Sprintf("selfhosted-%d", i)
		p := providers.NewSelfHosted(name, ep.URL, ep.APIKey)
		if p == nil {
			log.Warn("self-hosted endpoint skipped (empty name or URL)", "index", i)
			continue
		}
		opts = append(opts, llm.WithProvider(p))
		reg.RegisterModelProvider(&llm.ModelProviderEntry{
			Model:        ep.Model,
			Provider:     name,
			HealthStatus: "healthy",
			Enabled:      true,
		})
		log.Info("self-hosted LLM registered", "name", name, "url", ep.URL, "model", ep.Model)
	}

	return opts
}
