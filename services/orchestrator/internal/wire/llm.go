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
	"golang.org/x/time/rate"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

// modelPricing is the static rate card consulted at registry-construction time
// to stamp every ModelProviderEntry with real per-1M-token costs. Source of
// truth for these numbers lives in docs/llm-pricing.md alongside the operator
// runbook for rate-card refresh. Adding a model means: (1) edit this map,
// (2) edit docs/llm-pricing.md, (3) extend TestPriceFor_KnownModel in
// llm_test.go. Forgetting any one of the three drops billing rows for that
// model to $0, which silently breaks the daily-spend rate limiter.
//
// As-of date: 2026-05-30 (see docs/llm-pricing.md "Last verified").
var modelPricing = map[string]struct {
	InputCostPer1MTok  float64
	OutputCostPer1MTok float64
}{
	"anthropic/claude-sonnet-4-6": {3.00, 15.00},
	"anthropic/claude-haiku-4-5":  {1.00, 5.00},
	"anthropic/claude-opus-4-7":   {5.00, 25.00},
	"openai/gpt-4o-mini":          {0.15, 0.60},
}

// priceFor returns the (input, output) USD-per-1M-token list price for the
// given model ID. Unknown models return (0, 0) so the router still constructs
// without error but billing rows surface cost=0 — visible in usage_logs as the
// operator's drift signal that the rate card needs an update.
func priceFor(modelID string) (inputUSDPer1MTok, outputUSDPer1MTok float64) {
	entry, ok := modelPricing[modelID]
	if !ok {
		return 0, 0
	}
	return entry.InputCostPer1MTok, entry.OutputCostPer1MTok
}

// allConfiguredModelIDs returns the deduplicated set of model IDs the
// orchestrator can route to: the main chat model (LLMModel), the draft-reply
// model (DraftReplyModel — falls back to LLMModel in config.Load when unset),
// and one entry per self-hosted endpoint. Registry entries land per
// (provider, model) pair, so this drives the outer loop of buildProviderOpts.
func allConfiguredModelIDs(cfg *config.Config) []string {
	ids := make([]string, 0, 2+len(cfg.SelfHostedEndpoints))
	seen := make(map[string]bool, 2+len(cfg.SelfHostedEndpoints))
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

// secondsPerHour is the divisor that turns a per-hour request budget into a
// per-second rate.Limit. Named so the conversion is self-documenting.
const secondsPerHour = 3600.0

// localFallbackBurstDivisor sizes the in-process bucket's burst proportional
// to the configured per-hour rate so a short traffic spike during a Redis
// outage is not artificially clamped. ~1% of the hourly rate, floored at 1.
const localFallbackBurstDivisor = 100

// BuildRateLimiter assembles the *llm.RateLimiter consumed by the orchestrator
// and api routers. Honors the operator's cost-guard policy:
//
//   - Daily-spend gate is wired via the supplied DailySpender.
//   - When cfg.FreeTierDailySpendUSD > 0 it overrides the compiled
//     DefaultTierLimits["free"].DailySpendUSD; a value of -1 disables the
//     gate ("unlimited"); 0 keeps the compiled default.
//   - Redis-down policy is "block" (fail-closed) by default; "local_fallback"
//     consults an in-process bucket sized off LocalFallbackRequestsPerHour.
func BuildRateLimiter(cfg *config.Config, log *slog.Logger, rdb *redis.Client, spender llm.DailySpender) (*llm.RateLimiter, error) {
	limits := make(llm.TierLimits, len(llm.DefaultTierLimits))
	for k, v := range llm.DefaultTierLimits {
		limits[k] = v
	}
	switch {
	case cfg.FreeTierDailySpendUSD > 0:
		free := limits["free"]
		free.DailySpendUSD = cfg.FreeTierDailySpendUSD
		limits["free"] = free
	case cfg.FreeTierDailySpendUSD < 0:
		free := limits["free"]
		free.DailySpendUSD = 0
		limits["free"] = free
	}

	opts := []llm.RateLimiterOption{}
	if spender != nil {
		// Cache the per-business reading: the gate consults it once per LLM
		// iteration (up to MaxIterations per turn), each otherwise an HTTP call
		// to the api plus a PG SUM. A short TTL keeps the daily cap effective
		// while collapsing a turn's fan-out to one lookup per business per window.
		opts = append(opts, llm.WithDailySpender(llm.NewCachedDailySpender(spender, 0)))
	}

	switch cfg.RedisDownPolicy {
	case "block":
		opts = append(opts, llm.WithRedisDownPolicy(llm.RedisDownPolicyBlock))
	case "local_fallback":
		if cfg.LocalFallbackRequestsPerHour <= 0 {
			return nil, fmt.Errorf("BuildRateLimiter: LocalFallbackRequestsPerHour must be > 0 for local_fallback policy")
		}
		limit := rate.Limit(float64(cfg.LocalFallbackRequestsPerHour) / secondsPerHour)
		burst := cfg.LocalFallbackRequestsPerHour / localFallbackBurstDivisor
		if burst < 1 {
			burst = 1
		}
		opts = append(opts,
			llm.WithRedisDownPolicy(llm.RedisDownPolicyLocalFallback),
			llm.WithLocalBucket(limit, burst),
		)
		log.Info("rate limiter: local_fallback policy active",
			"requests_per_hour", cfg.LocalFallbackRequestsPerHour,
			"burst", burst,
		)
	default:
		return nil, fmt.Errorf("BuildRateLimiter: unknown RedisDownPolicy %q", cfg.RedisDownPolicy)
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
// the provider registrations built here. This keeps the wire layer in charge
// of provider/pricing wiring while letting cmd/main.go own infrastructure
// concerns (HTTP client + base URL) for the billing sink.
//
// Mirrors the historical block at services/orchestrator/cmd/main.go:60-66
// (registry creation + buildProviderOpts + NewRouter call). The provider
// builder helper is colocated here as a private function so the wiring stays
// self-contained — services/api has its own copy because the two services
// are separate Go modules and cross-module imports would force a new shared
// package without payoff.
func LLMRouter(cfg *config.Config, log *slog.Logger, extraOpts ...llm.RouterOption) (*llm.Router, error) {
	registry := llm.NewRegistry()
	opts := buildProviderOpts(cfg, registry, log)
	if len(opts) == 0 {
		return nil, fmt.Errorf("no LLM provider API key set — set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY")
	}
	opts = append(opts, extraOpts...)
	return llm.NewRouter(registry, opts...), nil
}

// buildProviderOpts creates RouterOptions for every API key that is set in
// config, and registers (provider, model) entries in the registry for every
// configured model ID. Each entry carries its rate-card pricing pulled from
// priceFor so logBilling stamps the correct cost on the resulting usage_logs
// row. Registering ONLY cfg.LLMModel previously left the cheap-tier titler /
// draft-reply rows at $0 because their model IDs were never present in the
// registry — fixed here by iterating allConfiguredModelIDs.
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

	configuredModels := allConfiguredModelIDs(cfg)

	opts := make([]llm.RouterOption, 0, len(specs)+len(cfg.SelfHostedEndpoints))
	for _, spec := range specs {
		if spec.apiKey == "" {
			continue
		}
		p := spec.factory(spec.apiKey)
		opts = append(opts, llm.WithProvider(p))
		for _, modelID := range configuredModels {
			inCost, outCost := priceFor(modelID)
			reg.RegisterModelProvider(&llm.ModelProviderEntry{
				Model:              modelID,
				Provider:           spec.name,
				InputCostPer1MTok:  inCost,
				OutputCostPer1MTok: outCost,
				HealthStatus:       llm.HealthStatusHealthy,
				Enabled:            true,
			})
			if inCost == 0 && outCost == 0 {
				warnRateCardMiss(log, spec.name, modelID)
			}
			log.Info("LLM provider registered",
				"provider", spec.name,
				"model", modelID,
				"input_cost_per_1m_tok", inCost,
				"output_cost_per_1m_tok", outCost,
			)
		}
	}

	for i, ep := range cfg.SelfHostedEndpoints {
		name := fmt.Sprintf("selfhosted-%d", i)
		p := providers.NewSelfHosted(name, ep.URL, ep.APIKey)
		if p == nil {
			log.Warn("self-hosted endpoint skipped (empty name or URL)", "index", i)
			continue
		}
		opts = append(opts, llm.WithProvider(p))
		inCost, outCost := priceFor(ep.Model)
		reg.RegisterModelProvider(&llm.ModelProviderEntry{
			Model:              ep.Model,
			Provider:           name,
			InputCostPer1MTok:  inCost,
			OutputCostPer1MTok: outCost,
			HealthStatus:       llm.HealthStatusHealthy,
			Enabled:            true,
		})
		if inCost == 0 && outCost == 0 {
			warnRateCardMiss(log, name, ep.Model)
		}
		log.Info("self-hosted LLM registered", "name", name, "url", ep.URL, "model", ep.Model)
	}

	return opts
}

// warnRateCardMiss flags a (provider, model) registration whose rate-card
// lookup returned $0 for both input and output. priceFor returns (0,0) for any
// model absent from modelPricing, so a typo'd or newly-added LLM_MODEL prices
// every call at $0: usage_logs rows land with provider_cost_usd=0, the
// per-business daily-spend gate (pkg/llm/ratelimit.go) sums to 0, and the cost
// guard never trips. Emitting this loudly at startup turns silent rate-card
// drift into an operator-visible signal. A genuinely free/internal model
// trips it too; that warning is safe to ignore. Add the model to modelPricing
// and docs/llm-pricing.md to clear it.
func warnRateCardMiss(log *slog.Logger, provider, modelID string) {
	log.Warn("LLM model missing from rate card: billing records $0 and the daily-spend rate limiter will be ineffective for it — add it to modelPricing and docs/llm-pricing.md",
		"provider", provider,
		"model", modelID,
		"rate_card", "docs/llm-pricing.md",
	)
}
