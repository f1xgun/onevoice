package llm

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

var (
	// ErrNoProvider is returned when no enabled+registered provider serves the model.
	ErrNoProvider = errors.New("no healthy provider available for model")
	// ErrRateLimitExceeded is returned when the per-user/tier rate limit gate rejects the call.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// tokensPerMillion is the divisor used when converting per-1M-token list prices into per-token cost.
const tokensPerMillion = 1_000_000

// RateLimitChecker is a testable interface for rate limit enforcement.
// businessID == uuid.Nil skips the per-business daily-spend gate.
type RateLimitChecker interface {
	CheckLimit(ctx context.Context, userID, businessID uuid.UUID, tier string, tokens int) (bool, error)
}

// Router brackets every LLM call with rate-limit, billing, metrics, and the
// one-shot sibling retry, delegating provider choice to a Selector.
// See docs/pkg/llm.md.
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

// WithBilling sets the billing writer. Accepts the narrow Writer interface so
// production can pass a Writer-only HTTP adapter (pkg/billingclient).
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

// WithSelector injects a Selector. Production callers don't need this;
// NewRouter auto-wraps a defaultSelector. Tests use this option to skip
// the registry+entries dance.
func WithSelector(s Selector) RouterOption {
	return func(r *Router) { r.selector = s }
}

// NewRouter creates a Router with the given registry and options.
// If no Selector is injected via WithSelector, a defaultSelector is constructed.
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

// tierFromRequest returns the effective tier for rate limiting (defaults to "free").
func tierFromRequest(req ChatRequest) string {
	if req.Tier == "" {
		return "free"
	}
	return req.Tier
}

// checkRateLimit enforces the per-user/tier limit before any provider work.
// Shared by Chat and ChatStream so they cannot drift.
func (r *Router) checkRateLimit(ctx context.Context, req ChatRequest) error {
	// Skip when no limiter is wired, and skip cluster-internal calls
	// (titler / review_drafter) that pass uuid.Nil UserID.
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

// retryLabel maps an attempt index to the llm_router_retry_total label value.
// Values past index 1 collapse to "unknown" so a future policy change to N>2
// attempts cannot silently mint new series.
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

// maxChatAttempts caps the candidate walk at primary + one sibling.
// Same-entry retry is intentionally skipped — without backoff it amplifies
// provider outages, and the second-most-preferred entry is a stronger
// fallback signal than a same-shot retry.
const maxChatAttempts = 2

// Chat performs a blocking LLM chat request with a one-shot sibling retry on
// transient errors. See docs/pkg/llm.md and docs/llm-router-retry.md.
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
		// Feed the health rollup on every attempt — both successful retries
		// and exhausted retries inform the next Pick.
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
			// Skip billing for system-level callers (titler, review_drafter)
			// that pass uuid.Nil BusinessID — the usage_logs.business_id
			// column is NOT NULL.
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
		// Transient. Retry only when this is attempt 0 and a sibling exists.
		if attempt == 0 && len(candidates) >= 2 {
			metrics.LLMRouterRetry.WithLabelValues("retrying", retryLabel(attempt)).Inc()
			continue
		}
		metrics.LLMRouterRetry.WithLabelValues("exhausted", retryLabel(attempt)).Inc()
		return nil, callErr
	}

	return nil, lastErr
}

// ChatStream performs a streaming LLM chat request using the selected provider.
// Intentionally NOT retried — mid-stream errors are not safely idempotent.
// See docs/pkg/llm.md and docs/llm-router-retry.md.
func (r *Router) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if err := r.checkRateLimit(ctx, req); err != nil {
		return nil, err
	}

	start := time.Now()
	entry, ch, err := Invoke(r.selector, req.Model, req.Strategy,
		func(p Provider) (<-chan StreamChunk, time.Duration, error) {
			out, callErr := p.ChatStream(ctx, req)
			if callErr != nil {
				return nil, 0, callErr
			}
			// Zero providerLatency: the channel-open instant is not user-perceived,
			// so defaultSelector skips this sample for the rolling window.
			return out, 0, nil
		})
	if err != nil || ch == nil {
		return ch, err
	}

	// Wrap the provider channel so the first chunk arrival records
	// llm_first_token_latency_seconds exactly once per ChatStream call.
	// sync.Once guards against multi-reader races even though production
	// code drains the channel from a single goroutine.
	out := make(chan StreamChunk, cap(ch))
	go func() {
		defer close(out)
		var once sync.Once
		for chunk := range ch {
			once.Do(func() {
				metrics.RecordLLMFirstToken(req.Model, entry.Provider, time.Since(start))
			})
			out <- chunk
		}
	}()
	// ChatStream does not bill; the terminal turn (non-streaming) accounts the cost.
	return out, nil
}

// cachePricingMultiplierRead is Anthropic's cache-read rate as a fraction of input rate (0.1×).
const cachePricingMultiplierRead = 0.1

// cachePricingMultiplierCreation is Anthropic's cache-write rate as a fraction of input rate (1.25×).
const cachePricingMultiplierCreation = 1.25

// billingPostTimeout bounds the per-call deadline applied inside logBilling so
// a hung downstream billing endpoint cannot accumulate goroutines forever.
const billingPostTimeout = 5 * time.Second

// logBilling computes the cache-aware provider cost and forwards a UsageLog
// entry to the configured Writer. See docs/pkg/llm.md.
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
