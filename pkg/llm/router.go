package llm

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNoProvider        = errors.New("no healthy provider available for model")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// tokensPerMillion is the divisor used when converting per-1M-token list prices
// (the unit billing providers publish) into a per-token cost.
const tokensPerMillion = 1_000_000

// RateLimitChecker is a testable interface for rate limit enforcement.
// businessID is the attribution key for the per-business daily-spend gate;
// callers without business context pass uuid.Nil so the gate is skipped (same
// nil-guard discipline applied to billing writes).
type RateLimitChecker interface {
	CheckLimit(ctx context.Context, userID, businessID uuid.UUID, tier string, tokens int) (bool, error)
}

// Router calls LLM providers selected by a Selector seam (pkg/llm/selector.go).
// Router itself owns the cross-cutting concerns that bracket every call —
// rate limiting, billing, prometheus metrics — and delegates "which
// provider for this model?" to Selector so neither concern has to reason
// about the other.
//
// The billing field is the narrow Writer interface so production can wire a
// Writer-only HTTP adapter (pkg/billingclient) without the orchestrator
// depending on read-path methods. Any BillingRepository also satisfies
// Writer, so existing WithBilling(BillingRepository) callers continue to
// compile unchanged.
type Router struct {
	registry    *Registry
	selector    Selector
	rateLimiter RateLimitChecker
	billing     Writer
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

// WithBilling sets the billing writer for usage logging. Accepts the narrow
// Writer interface so production can pass a Writer-only HTTP adapter
// (pkg/billingclient); any BillingRepository also satisfies Writer via
// interface embedding, so existing callers continue to work.
func WithBilling(w Writer) RouterOption {
	return func(r *Router) { r.billing = w }
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

// checkRateLimit enforces the per-user/tier limit before any provider work.
// Returns nil when the caller may proceed. Extracted so Chat and ChatStream
// share the gate verbatim — both must skip the check when no rate limiter
// is wired and when the request has no user id (cluster-internal calls).
func (r *Router) checkRateLimit(ctx context.Context, req ChatRequest) error {
	if r.rateLimiter == nil || req.UserID == uuid.Nil {
		return nil
	}
	allowed, err := r.rateLimiter.CheckLimit(ctx, req.UserID, req.BusinessID, tierFromRequest(req), 0)
	if err != nil {
		// Pass the sentinel through verbatim so callers can branch on
		// ErrDailySpendExceeded / ErrRateLimitUnavailable directly.
		return err
	}
	if !allowed {
		return ErrRateLimitExceeded
	}
	return nil
}

// Chat performs a blocking LLM chat request using the selected provider.
// Picking, outcome recording, and per-call prometheus emission all run
// inside Invoke; Router owns only the rate-limit gate and the async
// billing log.
func (r *Router) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := r.checkRateLimit(ctx, req); err != nil {
		return nil, err
	}

	entry, resp, err := Invoke(r.selector, req.Model, req.Strategy,
		func(p Provider) (*ChatResponse, time.Duration, error) {
			out, callErr := p.Chat(ctx, req)
			if callErr != nil {
				return nil, 0, callErr
			}
			return out, out.Latency, nil
		})
	if err != nil {
		return nil, err
	}

	resp.Provider = entry.Provider
	// Skip billing for system-level callers (titler, review_drafter) that
	// pass uuid.Nil BusinessID. The usage_logs.business_id column is NOT NULL
	// and the repository rejects nil-BusinessID rows; system callers are
	// retro-fitted to pass real BusinessIDs at the wiring layer.
	if r.billing != nil && req.BusinessID != uuid.Nil {
		go r.logBilling(context.Background(), req, entry, resp)
	}
	return resp, nil
}

// ChatStream performs a streaming LLM chat request using the selected
// provider. The closure returns a zero providerLatency because the
// channel-open instant is not user-perceived; defaultSelector skips
// zero-latency samples so the rolling window stays untouched for
// streaming starts.
func (r *Router) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if err := r.checkRateLimit(ctx, req); err != nil {
		return nil, err
	}

	_, ch, err := Invoke(r.selector, req.Model, req.Strategy,
		func(p Provider) (<-chan StreamChunk, time.Duration, error) {
			out, callErr := p.ChatStream(ctx, req)
			if callErr != nil {
				return nil, 0, callErr
			}
			return out, 0, nil
		})
	// ChatStream does not bill; the terminal turn (non-streaming) accounts the cost.
	return ch, err
}

// cachePricingMultiplierRead is Anthropic's cache-read rate as a fraction of
// the model's input rate. A cache hit is billed at 0.1× the input list price.
const cachePricingMultiplierRead = 0.1

// cachePricingMultiplierCreation is Anthropic's cache-write rate as a fraction
// of the model's input rate. A cache write is billed at 1.25× the input list
// price (the 5-minute ephemeral cache tier).
const cachePricingMultiplierCreation = 1.25

// billingPostTimeout bounds the per-call deadline applied inside logBilling so
// that a hung downstream billing endpoint cannot accumulate goroutines forever.
const billingPostTimeout = 5 * time.Second

// logBilling computes the cache-aware provider cost and forwards a UsageLog
// entry to the configured Writer. The billable-input formula reflects the
// Anthropic cache-pricing model (see TokenUsage doc):
//
//	billable_input = InputTokens*1.0 + CacheReadTokens*0.1 + CacheCreationTokens*1.25
//	provider_cost  = billable_input * InputCostPer1MTok / 1_000_000
//	             + OutputTokens   * OutputCostPer1MTok / 1_000_000
//
// Providers that do not surface cache breakdowns (OpenAI, OpenRouter,
// SelfHosted) leave CacheReadTokens / CacheCreationTokens at zero, so the
// formula collapses to InputTokens × InputCostPer1MTok / 1_000_000 — the
// pre-Phase-25a behavior.
func (r *Router) logBilling(ctx context.Context, req ChatRequest, entry *ModelProviderEntry, resp *ChatResponse) {
	ctx, cancel := context.WithTimeout(ctx, billingPostTimeout)
	defer cancel()

	billableInput := float64(resp.Usage.InputTokens) +
		float64(resp.Usage.CacheReadTokens)*cachePricingMultiplierRead +
		float64(resp.Usage.CacheCreationTokens)*cachePricingMultiplierCreation
	inputCostUSD := billableInput * entry.InputCostPer1MTok / tokensPerMillion
	outputCostUSD := float64(resp.Usage.OutputTokens) * entry.OutputCostPer1MTok / tokensPerMillion
	providerCost := inputCostUSD + outputCostUSD

	tier := tierFromRequest(req)
	commission := CalculateCommission(providerCost, r.commission.Mode, tier)

	_ = r.billing.LogUsage(ctx, &UsageLog{
		ID:                  uuid.New(),
		BusinessID:          req.BusinessID,
		UserID:              req.UserID,
		ConversationID:      req.ConversationID,
		RequestID:           req.RequestID,
		Model:               req.Model,
		Provider:            entry.Provider,
		InputTokens:         resp.Usage.InputTokens,
		OutputTokens:        resp.Usage.OutputTokens,
		CacheReadTokens:     resp.Usage.CacheReadTokens,
		CacheCreationTokens: resp.Usage.CacheCreationTokens,
		ProviderCostUSD:     providerCost,
		CommissionUSD:       commission,
		UserCostUSD:         providerCost + commission,
		UserTier:            tier,
		CreatedAt:           time.Now(),
	})
}
