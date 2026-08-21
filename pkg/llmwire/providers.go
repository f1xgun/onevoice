// Package llmwire holds the LLM Router wiring shared by services/api and
// services/orchestrator: provider registration and rate-limiter policy
// resolution. Both services build their Router the same way, so this lives
// under pkg/ (pulled into each service module via the go.work replace) as the
// single source of truth.
//
// It imports pkg/llm/providers to construct concrete providers, so it must stay
// OUT of pkg/llm itself — providers imports pkg/llm, and pkg/llm importing
// providers (directly or via a sub-package it owns) would be an import cycle.
package llmwire

import (
	"fmt"
	"log/slog"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
)

// ProviderKeys carries the hosted-provider API keys read from env. An empty key
// means that provider is not configured and is skipped.
type ProviderKeys struct {
	OpenRouter string
	OpenAI     string
	Anthropic  string
}

// RegisterConfiguredProviders registers a (provider, model) entry in reg for
// every configured model × every hosted provider whose key is set, plus one
// entry per self-hosted endpoint, and returns the matching WithProvider
// RouterOptions. Each entry carries its rate-card price from llm.PriceFor; a $0
// lookup emits a rate-card-miss WARN so silent pricing drift is operator-visible.
//
// Shared by services/api (titler / draft-reply Router) and services/orchestrator
// (chat Router) so both register the same tuples and price them identically.
func RegisterConfiguredProviders(reg *llm.Registry, log *slog.Logger, keys ProviderKeys, models []string, endpoints []llm.SelfHostedEndpoint) []llm.RouterOption {
	type providerSpec struct {
		name    string
		apiKey  string
		factory func(string) llm.Provider
	}

	specs := []providerSpec{
		{"openrouter", keys.OpenRouter, func(k string) llm.Provider { return providers.NewOpenRouter(k) }},
		{"openai", keys.OpenAI, func(k string) llm.Provider { return providers.NewOpenAI(k) }},
		{"anthropic", keys.Anthropic, func(k string) llm.Provider { return providers.NewAnthropic(k) }},
	}

	opts := make([]llm.RouterOption, 0, len(specs)+len(endpoints))
	for _, spec := range specs {
		if spec.apiKey == "" {
			continue
		}
		opts = append(opts, llm.WithProvider(spec.factory(spec.apiKey)))
		for _, modelID := range models {
			inCost, outCost := registerModel(reg, log, spec.name, modelID)
			log.Info("LLM provider registered",
				"provider", spec.name,
				"model", modelID,
				"input_cost_per_1m_tok", inCost,
				"output_cost_per_1m_tok", outCost,
			)
		}
	}

	for i, ep := range endpoints {
		name := fmt.Sprintf("selfhosted-%d", i)
		p := providers.NewSelfHosted(name, ep.URL, ep.APIKey)
		if p == nil {
			log.Warn("self-hosted endpoint skipped (empty name or URL)", "index", i)
			continue
		}
		opts = append(opts, llm.WithProvider(p))
		registerModel(reg, log, name, ep.Model)
		log.Info("self-hosted LLM registered", "name", name, "url", ep.URL, "model", ep.Model)
	}

	return opts
}

// registerModel stamps one (provider, model) entry into the registry with its
// rate-card price and warns when the lookup is a $0 miss. Returns the resolved
// (input, output) price so the caller can log it.
func registerModel(reg *llm.Registry, log *slog.Logger, provider, modelID string) (inCost, outCost float64) {
	inCost, outCost = llm.PriceFor(modelID)
	reg.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:              modelID,
		Provider:           provider,
		InputCostPer1MTok:  inCost,
		OutputCostPer1MTok: outCost,
		HealthStatus:       llm.HealthStatusHealthy,
		Enabled:            true,
	})
	if inCost == 0 && outCost == 0 {
		warnRateCardMiss(log, provider, modelID)
	}
	return inCost, outCost
}

// warnRateCardMiss flags a (provider, model) registration whose rate-card lookup
// returned $0 for both input and output. llm.PriceFor returns (0,0) for any model
// absent from the shared rate card (pkg/llm/pricing.go), so a typo'd or newly-
// added model prices every call at $0: usage_logs rows land with cost=0, the
// per-business daily-spend gate sums to 0, and the cost guard never trips.
// Emitting this loudly at startup turns silent rate-card drift into an operator-
// visible signal. A genuinely free/internal model trips it too; that warning is
// safe to ignore. Add the model to pkg/llm/pricing.go + docs/llm-pricing.md.
func warnRateCardMiss(log *slog.Logger, provider, modelID string) {
	log.Warn("LLM model missing from rate card: billing records $0 and the daily-spend rate limiter will be ineffective for it — add it to pkg/llm/pricing.go and docs/llm-pricing.md",
		"provider", provider,
		"model", modelID,
		"rate_card", "docs/llm-pricing.md",
	)
}
