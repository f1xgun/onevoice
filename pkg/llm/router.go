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

// retryLabel maps an attempt index to the label value the
// llm_router_retry_total counter expects. The label vocabulary is fixed at
// {first, second}; values past index 1 collapse to "unknown" so a future
// policy change to N>2 attempts cannot silently mint new series.
func retryLabel(attempt int) string {
	switch attempt {
	case 0:
		return "first"
	case 1:
		return "second"
	default:
		return "unknown"
	}
}

// maxChatAttempts caps the candidate walk at two attempts — the primary
// pick plus one sibling. Same-entry retry is intentionally skipped: without
// backoff it amplifies provider outages, and the second-most-preferred
// registry entry is a stronger fallback signal than a same-shot retry.
const maxChatAttempts = 2

// Chat performs a blocking LLM chat request, walking up to two registered
// candidates with a one-shot retry on transient provider errors. The retry
// targets a sibling registry entry (different provider for the same model);
// the LLM POST is naturally idempotent at the API layer, so a sibling can
// safely replay the request.
//
// Failure semantics:
//
//   - Non-transient error on attempt 0: returned immediately. No sibling is
//     attempted because the failure (4xx, malformed payload, etc.) will
//     reproduce on any provider.
//   - Transient error on attempt 0 with a sibling available: a second
//     attempt against the sibling fires. If it succeeds, its response is
//     returned and billed. If it fails (transient or otherwise), that
//     second error is returned.
//   - Single-candidate registries return the original error without a
//     second attempt — there is no sibling to retry against.
//
// Billing fires exactly once and only for the successful attempt's
// response — the failed first attempt is never billed even if its partial
// response surfaced Usage tokens.
func (r *Router) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := r.checkRateLimit(ctx, req); err != nil {
		return nil, err
	}

	candidates := r.selector.Candidates(req.Model, req.Strategy)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	var lastErr error
	for attempt, cand := range candidates {
		if attempt >= maxChatAttempts {
			break
		}

		start := time.Now()
		resp, callErr := cand.Provider.Chat(ctx, req)
		// Feed the health rollup on every attempt — both successful
		// retries and exhausted retries inform the next Pick.
		providerLatency := time.Duration(0)
		if resp != nil {
			providerLatency = resp.Latency
		}
		r.selector.Record(cand.Entry, Outcome{
			Success: callErr == nil,
			Latency: providerLatency,
			Model:   req.Model,
			Wall:    time.Since(start),
		})

		if callErr == nil {
			metrics.LLMRouterRetry.WithLabelValues("success", retryLabel(attempt)).Inc()
			resp.Provider = cand.Entry.Provider
			// Skip billing for system-level callers (titler,
			// review_drafter) that pass uuid.Nil BusinessID. The
			// usage_logs.business_id column is NOT NULL and the
			// repository rejects nil-BusinessID rows.
			if r.billing != nil && req.BusinessID != uuid.Nil {
				go r.logBilling(context.Background(), req, cand.Entry, resp)
			}
			return resp, nil
		}

		lastErr = callErr
		if !isTransientLLMError(callErr) {
			metrics.LLMRouterRetry.WithLabelValues("nonretryable", retryLabel(attempt)).Inc()
			return nil, callErr
		}
		// Transient. Decide whether a retry is even possible.
		if attempt == 0 && len(candidates) >= 2 {
			metrics.LLMRouterRetry.WithLabelValues("retrying", retryLabel(attempt)).Inc()
			continue
		}
		metrics.LLMRouterRetry.WithLabelValues("exhausted", retryLabel(attempt)).Inc()
		return nil, callErr
	}

	return nil, lastErr
}

// ChatStream performs a streaming LLM chat request using the selected
// provider. The closure returns a zero providerLatency because the
// channel-open instant is not user-perceived; defaultSelector skips
// zero-latency samples so the rolling window stays untouched for
// streaming starts.
//
// ChatStream is intentionally NOT retried. Mid-stream errors are not
// safely idempotent: the channel may have already emitted partial chunks
// to the caller, and replaying the request against a sibling would
// surface duplicate content. Callers that need fault tolerance on the
// streaming path should fall back to non-streaming Chat (which is the
// retried path) or re-issue the prompt on a fresh connection.
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
