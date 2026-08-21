package wire

import (
	"context"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/llmwire"
	"github.com/f1xgun/onevoice/services/api/internal/config"
)

// The LLM rate card (model → per-1M-token price), llm.PriceFor, and
// llm.NormalizeModelID live in pkg/llm/pricing.go — the single source of truth
// shared with services/orchestrator. Provider registration and rate-limiter
// policy resolution are shared via pkg/llmwire. This service used to keep its
// own copies here, which silently drifted; add a model / change wiring once now.

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

// LLMProviderOpts registers the api Router's providers and their (provider,
// model) entries via the shared llmwire builder, so the api registers the same
// tuples and prices them identically to the orchestrator. The api routes the
// main chat model plus the auto-titler model. Returns at least one option if any
// key is set, nil if none.
func LLMProviderOpts(cfg *config.Config, reg *llm.Registry, log *slog.Logger) []llm.RouterOption {
	return llmwire.RegisterConfiguredProviders(reg, log, llmwire.ProviderKeys{
		OpenRouter: cfg.OpenRouterAPIKey,
		OpenAI:     cfg.OpenAIAPIKey,
		Anthropic:  cfg.AnthropicAPIKey,
	}, allConfiguredModelIDs(cfg), cfg.SelfHostedEndpoints)
}

// repoDailySpender adapts an llm.BillingRepository to the llm.DailySpender
// interface so the api-side rate-limiter does not need a billingclient round-
// trip — it calls the repository in-process.
type repoDailySpender struct {
	repo llm.BillingRepository
}

func (s repoDailySpender) GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	return s.repo.GetDailySpend(ctx, businessID, day)
}

// BuildAPIRateLimiter constructs the api-side *llm.RateLimiter consumed by the
// titler / draft-reply Router. Policy resolution (tier limits, free-tier
// override, Redis-down policy) is shared with the orchestrator via
// llmwire.RateLimiterPolicy; the api reads the daily-spend value directly from
// the supplied repository (it holds the DB pool, so a billingclient HTTP round-
// trip would be wasted work).
func BuildAPIRateLimiter(cfg *config.Config, log *slog.Logger, rdb *goredis.Client, billing llm.BillingRepository) (*llm.RateLimiter, error) {
	limits, opts, err := llmwire.RateLimiterPolicy(llmwire.RateLimiterConfig{
		FreeTierDailySpendUSD:        cfg.FreeTierDailySpendUSD,
		RedisDownPolicy:              cfg.RedisDownPolicy,
		LocalFallbackRequestsPerHour: cfg.LocalFallbackRequestsPerHour,
	}, log)
	if err != nil {
		return nil, err
	}

	opts = append(opts, llm.WithDailySpender(repoDailySpender{repo: billing}))
	return llm.NewRateLimiter(rdb, limits, opts...), nil
}
