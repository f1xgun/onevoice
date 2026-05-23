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

// tokensPerMillion is the divisor used when converting per-1M-token list prices
// (the unit billing providers publish) into a per-token cost.
const tokensPerMillion = 1_000_000

// RateLimitChecker is a testable interface for rate limit enforcement.
type RateLimitChecker interface {
	CheckLimit(ctx context.Context, userID uuid.UUID, tier string, tokens int) (bool, error)
}

// Router calls LLM providers selected by a Selector seam (pkg/llm/selector.go).
// Router itself owns the cross-cutting concerns that bracket every call —
// rate limiting, billing, prometheus metrics — and delegates "which
// provider for this model?" to Selector so neither concern has to reason
// about the other.
type Router struct {
	registry    *Registry
	selector    Selector
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

// WithSelector injects a Selector. Production callers don't need this:
// NewRouter auto-wraps a defaultSelector from the registry and the
// providers registered via WithProvider. Tests use this option to skip
// the registry+entries dance — a fake Selector answers Pick directly.
func WithSelector(s Selector) RouterOption {
	return func(r *Router) { r.selector = s }
}

// NewRouter creates a Router with the given registry and options.
// If no Selector is injected via WithSelector, a defaultSelector is
// constructed over `registry` and the providers registered via
// WithProvider so existing call sites don't need to change.
func NewRouter(registry *Registry, opts ...RouterOption) *Router {
	r := &Router{
		registry:  registry,
		providers: make(map[string]Provider),
	}
	for _, o := range opts {
		o(r)
	}
	if r.selector == nil {
		r.selector = NewSelector(registry, r.providers)
	}
	return r
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

	entry, provider, err := r.selector.Pick(req.Model, req.Strategy)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := provider.Chat(ctx, req)
	if err != nil {
		r.selector.Record(entry, Outcome{Success: false})
		metrics.RecordLLMRequest(req.Model, entry.Provider, "error", time.Since(start))
		return nil, err
	}

	metrics.RecordLLMRequest(req.Model, entry.Provider, "success", time.Since(start))
	r.selector.Record(entry, Outcome{Success: true, Latency: resp.Latency})
	resp.Provider = entry.Provider

	if r.billing != nil {
		go r.logBilling(context.Background(), req, entry, resp)
	}

	return resp, nil
}

// ChatStream performs a streaming LLM chat request using the selected provider.
// The channel-open latency is not the user-perceived latency so we report
// success with a zero Latency — the rolling window stays untouched for
// streaming starts (defaultSelector skips zero-latency samples by design).
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

	entry, provider, err := r.selector.Pick(req.Model, req.Strategy)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	ch, err := provider.ChatStream(ctx, req)
	if err != nil {
		r.selector.Record(entry, Outcome{Success: false})
		metrics.RecordLLMRequest(req.Model, entry.Provider, "error", time.Since(start))
		return nil, err
	}

	metrics.RecordLLMRequest(req.Model, entry.Provider, "success", time.Since(start))
	r.selector.Record(entry, Outcome{Success: true, Latency: 0})
	return ch, nil
}

// logBilling calculates costs and logs a UsageLog entry.
func (r *Router) logBilling(ctx context.Context, req ChatRequest, entry *ModelProviderEntry, resp *ChatResponse) {
	inputCostUSD := float64(resp.Usage.InputTokens) * entry.InputCostPer1MTok / tokensPerMillion
	outputCostUSD := float64(resp.Usage.OutputTokens) * entry.OutputCostPer1MTok / tokensPerMillion
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
