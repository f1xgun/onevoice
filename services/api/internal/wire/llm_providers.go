package wire

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/f1xgun/onevoice/services/api/internal/config"
)

// LLMProviderOpts creates RouterOptions for every API key that is set in
// config, and registers the LLM model → provider mapping in the registry
// for each. Returns at least one option if any key is set, nil if none.
//
// Lifted verbatim from services/orchestrator/cmd/main.go so the API-side
// titler Router constructs over byte-identical provider semantics. The
// only intentional difference between this copy and the orchestrator's
// is package locality; the body is unchanged so future audits can diff
// the two and confirm parity.
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
