package llm

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

var (
	ErrNoProvider        = errors.New("no healthy provider available for model")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// RateLimitChecker is a testable interface for rate limit enforcement.
type RateLimitChecker interface {
	CheckLimit(ctx context.Context, userID uuid.UUID, tier string, tokens int) (bool, error)
}

// Router selects and calls LLM providers based on routing strategy.
type Router struct {
	registry    *Registry
	rateLimiter RateLimitChecker
	billing     BillingRepository
	providers   map[string]Provider
	commission  CommissionConfig
}

// RouterOption is a functional option for Router construction.
type RouterOption func(*Router)

// WithRateLimiter sets a concrete *RateLimiter as the rate limit checker.
func WithRateLimiter(rl *RateLimiter) RouterOption {
	return func(r *Router) { r.rateLimiter = rl }
}

// WithRateLimitChecker sets any RateLimitChecker (useful for testing with fakes).
func WithRateLimitChecker(rlc RateLimitChecker) RouterOption {
	return func(r *Router) { r.rateLimiter = rlc }
}

// WithBilling sets the billing repository for usage logging.
func WithBilling(br BillingRepository) RouterOption {
	return func(r *Router) { r.billing = br }
}

// WithProvider registers a Provider implementation by name.
func WithProvider(p Provider) RouterOption {
	return func(r *Router) {
		if p == nil {
			return
		}
		r.providers[p.Name()] = p
	}
}

// WithCommission sets the commission configuration for billing.
func WithCommission(cfg CommissionConfig) RouterOption {
	return func(r *Router) { r.commission = cfg }
}

// NewRouter creates a Router with the given registry and options.
func NewRouter(registry *Registry, opts ...RouterOption) *Router {
	r := &Router{
		registry:  registry,
		providers: make(map[string]Provider),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// pickProvider selects the best healthy, enabled, registered provider for model.
func (r *Router) pickProvider(model string, strategy Strategy) (*ModelProviderEntry, Provider, error) {
	entries := r.registry.GetModelProviders(model)

	var best *ModelProviderEntry
	var bestProvider Provider

	for _, e := range entries {
		if e.HealthStatus != "healthy" || !e.Enabled {
			continue
		}
		p, ok := r.providers[e.Provider]
		if !ok {
			continue
		}

		if best == nil {
			best = e
			bestProvider = p
			continue
		}

		if strategy == StrategyCost {
			if avgCost(e) < avgCost(best) {
				best = e
				bestProvider = p
			}
		} else {
			if betterLatency(e, best) {
				best = e
				bestProvider = p
			}
		}
	}

	if best == nil {
		// Fallback: if all providers are unhealthy, try the first enabled one anyway
		// to avoid permanent deadlock when a single provider recovers.
		for _, e := range entries {
			if !e.Enabled {
				continue
			}
			p, ok := r.providers[e.Provider]
			if !ok {
				continue
			}
			return e, p, nil
		}
		return nil, nil, ErrNoProvider
	}
	return best, bestProvider, nil
}

// avgCost returns the average of input and output cost per 1M tokens.
func avgCost(e *ModelProviderEntry) float64 {
	return (e.InputCostPer1MTok + e.OutputCostPer1MTok) / 2.0
}

// betterLatency returns true if candidate has a lower non-zero latency than current.
// Zero latency means no data and is ranked last.
func betterLatency(candidate, current *ModelProviderEntry) bool {
	if candidate.AvgLatencyMs == 0 {
		return false
	}
	if current.AvgLatencyMs == 0 {
		return true
	}
	return candidate.AvgLatencyMs < current.AvgLatencyMs
}

// tierFromRequest returns the effective tier for rate limiting.
func tierFromRequest(req ChatRequest) string {
	if req.Tier == "" {
		return "free"
	}
	return req.Tier
}

// Chat performs a blocking LLM chat request using the selected provider.
func (r *Router) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if r.rateLimiter != nil && req.UserID != uuid.Nil {
		tier := tierFromRequest(req)
		allowed, err := r.rateLimiter.CheckLimit(ctx, req.UserID, tier, 0)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrRateLimitExceeded
		}
	}

	entry, provider, err := r.pickProvider(req.Model, req.Strategy)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := provider.Chat(ctx, req)
	if err != nil {
		r.registry.RecordFailure(entry.Provider, req.Model)
		metrics.RecordLLMRequest(req.Model, entry.Provider, "error", time.Since(start))
		return nil, err
	}

	metrics.RecordLLMRequest(req.Model, entry.Provider, "success", time.Since(start))
	r.registry.RecordSuccess(entry.Provider, req.Model, resp.Latency)
	resp.Provider = entry.Provider

	if r.billing != nil {
		go r.logBilling(context.Background(), req, entry, resp)
	}

	return resp, nil
}

// ChatStream performs a streaming LLM chat request using the selected provider.
func (r *Router) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if r.rateLimiter != nil && req.UserID != uuid.Nil {
		tier := tierFromRequest(req)
		allowed, err := r.rateLimiter.CheckLimit(ctx, req.UserID, tier, 0)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrRateLimitExceeded
		}
	}

	entry, provider, err := r.pickProvider(req.Model, req.Strategy)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	ch, err := provider.ChatStream(ctx, req)
	if err != nil {
		r.registry.RecordFailure(entry.Provider, req.Model)
		metrics.RecordLLMRequest(req.Model, entry.Provider, "error", time.Since(start))
		return nil, err
	}

	metrics.RecordLLMRequest(req.Model, entry.Provider, "success", time.Since(start))
	// RecordSuccess with 0 latency — streaming latency is not available at channel-open time.
	// Latency tracking for streaming must be handled at a higher layer.
	r.registry.RecordSuccess(entry.Provider, req.Model, 0)
	return ch, nil
}

// logBilling calculates costs and logs a UsageLog entry.
func (r *Router) logBilling(ctx context.Context, req ChatRequest, entry *ModelProviderEntry, resp *ChatResponse) {
	inputCostUSD := float64(resp.Usage.InputTokens) * entry.InputCostPer1MTok / 1_000_000
	outputCostUSD := float64(resp.Usage.OutputTokens) * entry.OutputCostPer1MTok / 1_000_000
	providerCost := inputCostUSD + outputCostUSD

	tier := tierFromRequest(req)
	commission := CalculateCommission(providerCost, r.commission.Mode, tier)

	_ = r.billing.LogUsage(ctx, &UsageLog{
		ID:              uuid.New(),
		UserID:          req.UserID,
		Model:           req.Model,
		Provider:        entry.Provider,
		InputTokens:     resp.Usage.InputTokens,
		OutputTokens:    resp.Usage.OutputTokens,
		ProviderCostUSD: providerCost,
		CommissionUSD:   commission,
		UserCostUSD:     providerCost + commission,
		UserTier:        tier,
		CreatedAt:       time.Now(),
	})
}
