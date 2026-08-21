// Package wire owns the construction of the orchestrator's runtime
// dependencies (LLM router, Mongo connection, tool registry, HTTP handlers)
// so that cmd/main.go stays at the SC-05 ≤200-LOC budget. Each function in
// this package is a pure factory: it takes plain inputs (config, logger,
// connections) and returns the live instance — no global state, no init().
package wire

import (
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llmwire"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

// The LLM rate card (model → per-1M-token price), PriceFor, and NormalizeModelID
// live in pkg/llm/pricing.go — the single source of truth shared by this service
// and services/api. Add a model there + docs/llm-pricing.md + pricing_test.go.
//
// Provider registration and rate-limiter policy resolution are shared with
// services/api via pkg/llmwire, so the two services build their Router the same
// way (see llmwire.RegisterConfiguredProviders / llmwire.RateLimiterPolicy).

// allConfiguredModelIDs returns the deduplicated set of model IDs the
// orchestrator can route to: the main chat model (LLMModel) and the draft-reply
// model (DraftReplyModel — falls back to LLMModel in config.Load when unset).
// Registry entries land per (provider, model) pair, so this drives the outer
// loop of provider registration.
func allConfiguredModelIDs(cfg *config.Config) []string {
	ids := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	add := func(m string) {
		if m == "" || seen[m] {
			return
		}
		ids = append(ids, m)
		seen[m] = true
	}
	add(cfg.LLMModel)
	add(cfg.DraftReplyModel)
	return ids
}

// BuildRateLimiter assembles the *llm.RateLimiter consumed by the orchestrator
// router. Policy resolution (tier limits, free-tier override, Redis-down policy)
// is shared with the api via llmwire.RateLimiterPolicy; the orchestrator adds
// its own daily-spend source: a cached wrapper over the supplied DailySpender
// (typically a billingclient), consulted once per LLM iteration.
func BuildRateLimiter(cfg *config.Config, log *slog.Logger, rdb *redis.Client, spender llm.DailySpender) (*llm.RateLimiter, error) {
	limits, opts, err := llmwire.RateLimiterPolicy(llmwire.RateLimiterConfig{
		FreeTierDailySpendUSD:        cfg.FreeTierDailySpendUSD,
		RedisDownPolicy:              cfg.RedisDownPolicy,
		LocalFallbackRequestsPerHour: cfg.LocalFallbackRequestsPerHour,
	}, log)
	if err != nil {
		return nil, err
	}

	if spender != nil {
		// Cache the per-business reading: the gate consults it once per LLM
		// iteration (up to MaxIterations per turn), each otherwise an HTTP call
		// to the api plus a PG SUM. A short TTL keeps the daily cap effective
		// while collapsing a turn's fan-out to one lookup per business per window.
		opts = append(opts, llm.WithDailySpender(llm.NewCachedDailySpender(spender, 0)))
	}

	return llm.NewRateLimiter(rdb, limits, opts...), nil
}

// LLMRouter constructs the LLM Router with every provider whose API key is
// set in cfg, plus any SELF_HOSTED_N_* endpoints. At least one provider key
// must be present — otherwise returns an error so the orchestrator fails
// loudly at boot rather than serving requests with no LLM backend.
//
// extraOpts threads caller-supplied RouterOptions (typically WithBilling
// wired from cmd/main.go via pkg/billingclient) into llm.NewRouter alongside
// the provider registrations. This keeps the wire layer in charge of provider/
// pricing wiring while letting cmd/main.go own infrastructure concerns (HTTP
// client + base URL) for the billing sink. Provider registration itself is
// shared with services/api via llmwire.RegisterConfiguredProviders.
func LLMRouter(cfg *config.Config, log *slog.Logger, extraOpts ...llm.RouterOption) (*llm.Router, error) {
	registry := llm.NewRegistry()
	opts := llmwire.RegisterConfiguredProviders(registry, log, llmwire.ProviderKeys{
		OpenRouter: cfg.OpenRouterAPIKey,
		OpenAI:     cfg.OpenAIAPIKey,
		Anthropic:  cfg.AnthropicAPIKey,
	}, allConfiguredModelIDs(cfg), cfg.SelfHostedEndpoints)
	if len(opts) == 0 {
		return nil, fmt.Errorf("no LLM provider API key set — set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY")
	}
	opts = append(opts, extraOpts...)
	return llm.NewRouter(registry, opts...), nil
}
