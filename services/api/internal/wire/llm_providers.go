package wire

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/services/api/internal/config"
)

// apiModelPricing mirrors the orchestrator-side modelPricing rate card
// (services/orchestrator/internal/wire/llm.go). Source of truth for these
// numbers lives in docs/llm-pricing.md; both copies must stay in lockstep —
// the regression test in this package fails if priceFor diverges from the
// orchestrator's known-model prices. Forgetting to update one side drops
// billing rows to $0 for the cheap-tier titler / draft-reply paths.
var apiModelPricing = map[string]struct {
	InputCostPer1MTok  float64
	OutputCostPer1MTok float64
}{
	"anthropic/claude-sonnet-4-6": {3.00, 15.00},
	"anthropic/claude-haiku-4-5":  {1.00, 5.00},
	"anthropic/claude-opus-4-7":   {5.00, 25.00},
	"openai/gpt-4o-mini":          {0.15, 0.60},
	// DeepSeek V4 Flash via Yandex AI Studio (RU prod primary). Kept in lockstep
	// with the orchestrator rate card — see docs/llm-pricing.md. Model IDs arrive
	// folder-qualified; priceFor normalizes them to this bare slug.
	"deepseek-v4-flash": {3.60, 6.00},
}

// priceFor returns (input, output) USD-per-1M-token prices for a model ID.
// Unknown models return (0, 0) so the router still constructs; the operator
// sees zero-cost usage_logs rows as a drift signal.
func priceFor(modelID string) (inputUSDPer1MTok, outputUSDPer1MTok float64) {
	entry, ok := apiModelPricing[normalizeModelID(modelID)]
	if !ok {
		return 0, 0
	}
	return entry.InputCostPer1MTok, entry.OutputCostPer1MTok
}

// normalizeModelID reduces a provider-qualified model identifier to the bare
// slug used as an apiModelPricing key. Yandex AI Studio addresses models as
// gpt://<folder>/<model>[/<version>]; the folder segment is deployment-specific,
// so the rate card keys on <model> alone. Non-URI IDs pass through unchanged.
// Kept identical to the orchestrator's normalizeModelID.
func normalizeModelID(modelID string) string {
	rest, ok := strings.CutPrefix(modelID, "gpt://")
	if !ok {
		return modelID
	}
	if segs := strings.Split(rest, "/"); len(segs) >= 2 {
		return segs[1]
	}
	return modelID
}

// allConfiguredModelIDs returns the deduplicated set of model IDs the API
// service can route to: the main chat model (LLMModel) and the auto-titler
// model (TitlerModel). Both must be registered so the Router can resolve
// TITLER_MODEL when it differs from LLM_MODEL (the documented cheap-tier
// path with haiku titler over sonnet main).
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
	add(cfg.TitlerModel)
	return ids
}

// LLMProviderOpts creates RouterOptions for every API key that is set in
// config, and registers (provider, model) entries in the registry for every
// configured model ID. Each entry carries its rate-card pricing from priceFor
// so usage_logs rows surface a non-zero cost on the titler Router. Returns at
// least one option if any key is set, nil if none.
//
// Mirrors services/orchestrator/internal/wire/llm.go buildProviderOpts —
// the api Router must register the same (provider, model) tuples as the
// orchestrator so that Pick can resolve TITLER_MODEL on the cheap tier.
func LLMProviderOpts(cfg *config.Config, reg *llm.Registry, log *slog.Logger) []llm.RouterOption {
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
		log.Info("self-hosted LLM registered", "name", name, "url", ep.URL, "model", ep.Model)
	}

	return opts
}

// secondsPerHourAPI is the divisor that turns a per-hour request budget into
// a per-second rate.Limit. Kept distinct from the orchestrator's copy because
// services/api is a separate Go module.
const secondsPerHourAPI = 3600.0

// localFallbackBurstDivisorAPI is the ~1% bucket-burst sizing for the api-
// side local fallback bucket.
const localFallbackBurstDivisorAPI = 100

// repoDailySpender adapts an llm.BillingRepository to the llm.DailySpender
// interface so the api-side rate-limiter does not need a billingclient round-
// trip — it calls the repository in-process.
type repoDailySpender struct {
	repo llm.BillingRepository
}

func (s repoDailySpender) GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	return s.repo.GetDailySpend(ctx, businessID, day)
}

// BuildAPIRateLimiter constructs the api-side *llm.RateLimiter consumed by
// the titler / draft-reply Router. Mirrors the orchestrator's BuildRateLimiter
// in policy resolution but reads the daily-spend value directly from the
// supplied repository — the api holds the DB pool so a billingclient HTTP
// round-trip would be wasted work.
func BuildAPIRateLimiter(cfg *config.Config, log *slog.Logger, rdb *goredis.Client, billing llm.BillingRepository) (*llm.RateLimiter, error) {
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

	opts := []llm.RateLimiterOption{
		llm.WithDailySpender(repoDailySpender{repo: billing}),
	}

	switch cfg.RedisDownPolicy {
	case "block":
		opts = append(opts, llm.WithRedisDownPolicy(llm.RedisDownPolicyBlock))
	case "local_fallback":
		if cfg.LocalFallbackRequestsPerHour <= 0 {
			return nil, fmt.Errorf("BuildAPIRateLimiter: LocalFallbackRequestsPerHour must be > 0 for local_fallback policy")
		}
		limit := rate.Limit(float64(cfg.LocalFallbackRequestsPerHour) / secondsPerHourAPI)
		burst := cfg.LocalFallbackRequestsPerHour / localFallbackBurstDivisorAPI
		if burst < 1 {
			burst = 1
		}
		opts = append(opts,
			llm.WithRedisDownPolicy(llm.RedisDownPolicyLocalFallback),
			llm.WithLocalBucket(limit, burst),
		)
		log.Info("api rate limiter: local_fallback policy active",
			"requests_per_hour", cfg.LocalFallbackRequestsPerHour,
			"burst", burst,
		)
	default:
		return nil, fmt.Errorf("BuildAPIRateLimiter: unknown RedisDownPolicy %q", cfg.RedisDownPolicy)
	}

	return llm.NewRateLimiter(rdb, limits, opts...), nil
}
