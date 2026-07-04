// Package planresolver resolves a business's effective billing plan (and the
// rate-limit tier the orchestrator must apply) from its active subscription,
// with a short-TTL in-process cache.
//
// It is FAIL-SAFE by construction: a DB error, a missing subscription, or a
// missing plan row all resolve to Free — never to a higher tier. This is what
// makes wiring it into the chat-turn hot path (replacing the hardcoded
// "tier": "" in the orchestrator request) safe: the worst case is a business is
// briefly rate-limited as Free, never that it is accidentally granted
// Enterprise limits.
package planresolver

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// DefaultTTL bounds how long a resolved plan is cached per business. Short
// enough that a subscription change (Track-B webhook / grant job) becomes
// effective quickly, long enough to keep the chat hot path off the DB.
const DefaultTTL = 60 * time.Second

// Plan is the resolved, display-and-enforcement-ready view of a business's
// billing plan.
type Plan struct {
	Code           string
	Name           string
	RateLimitTier  string
	MonthlyCredits int
	DailyLLMUSDCap float64
}

// freePlan is the hard fallback used when even the Free plan_definitions row is
// unreadable (DB down / catalog missing). RateLimitTier "free" is the safe
// floor; the orchestrator maps it through pkg/llm.DefaultTierLimits.
var freePlan = Plan{
	Code:           "free",
	Name:           "Free",
	RateLimitTier:  "free",
	MonthlyCredits: 0,
	DailyLLMUSDCap: 1.0,
}

// Store is the data seam the resolver reads through. Tests inject a fake; prod
// uses RepoStore over the subscription + plan-definition repositories.
type Store interface {
	// ActivePlanForBusiness returns the plan of the business's active
	// subscription, or domain.ErrSubscriptionNotFound when there is none.
	ActivePlanForBusiness(ctx context.Context, businessID uuid.UUID) (Plan, error)
	// FreePlan returns the catalog's Free plan.
	FreePlan(ctx context.Context) (Plan, error)
}

type cachedPlan struct {
	plan      Plan
	expiresAt time.Time
}

// Resolver caches resolved plans per business for ttl.
type Resolver struct {
	store Store
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	cache map[uuid.UUID]cachedPlan
}

// New constructs a Resolver. A non-positive ttl falls back to DefaultTTL.
func New(store Store, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Resolver{
		store: store,
		ttl:   ttl,
		now:   time.Now,
		cache: make(map[uuid.UUID]cachedPlan),
	}
}

// Resolve returns the business's effective plan. It serves a live cache entry
// when present, otherwise fetches through the store and caches the result. Any
// failure (store error, no active subscription) resolves to Free and is cached
// so a persistent DB fault does not hammer the store on every chat turn.
func (r *Resolver) Resolve(ctx context.Context, businessID uuid.UUID) Plan {
	now := r.now()

	r.mu.Lock()
	if entry, ok := r.cache[businessID]; ok && now.Before(entry.expiresAt) {
		plan := entry.plan
		r.mu.Unlock()
		return plan
	}
	r.mu.Unlock()

	plan := r.fetch(ctx, businessID)

	r.mu.Lock()
	r.cache[businessID] = cachedPlan{plan: plan, expiresAt: now.Add(r.ttl)}
	r.mu.Unlock()
	return plan
}

// fetch resolves without touching the cache. Fail-safe: any error path yields
// Free.
func (r *Resolver) fetch(ctx context.Context, businessID uuid.UUID) Plan {
	plan, err := r.store.ActivePlanForBusiness(ctx, businessID)
	if err == nil {
		return plan
	}
	free, ferr := r.store.FreePlan(ctx)
	if ferr != nil {
		slog.WarnContext(ctx, "planresolver: falling back to hardcoded Free plan",
			"business_id", businessID, "active_err", err, "free_err", ferr)
		return freePlan
	}
	return free
}

// Invalidate drops the cached plan for a business so the next Resolve refetches.
// Call after a subscription mutation (Track-B webhook, grant job).
func (r *Resolver) Invalidate(businessID uuid.UUID) {
	r.mu.Lock()
	delete(r.cache, businessID)
	r.mu.Unlock()
}

// RepoStore adapts the subscription + plan-definition repositories to Store.
type RepoStore struct {
	subs  domain.SubscriptionRepository
	plans domain.PlanDefinitionRepository
}

// NewRepoStore builds the production Store.
func NewRepoStore(subs domain.SubscriptionRepository, plans domain.PlanDefinitionRepository) *RepoStore {
	return &RepoStore{subs: subs, plans: plans}
}

// ActivePlanForBusiness looks up the active subscription then its plan.
func (s *RepoStore) ActivePlanForBusiness(ctx context.Context, businessID uuid.UUID) (Plan, error) {
	sub, err := s.subs.ActiveByBusiness(ctx, businessID)
	if err != nil {
		return Plan{}, err
	}
	def, err := s.plans.GetByCode(ctx, sub.PlanCode)
	if err != nil {
		return Plan{}, err
	}
	return planFromDefinition(def), nil
}

// FreePlan reads the catalog's Free plan.
func (s *RepoStore) FreePlan(ctx context.Context) (Plan, error) {
	def, err := s.plans.GetByCode(ctx, "free")
	if err != nil {
		return Plan{}, err
	}
	return planFromDefinition(def), nil
}

func planFromDefinition(def *domain.PlanDefinition) Plan {
	return Plan{
		Code:           def.Code,
		Name:           def.DisplayName,
		RateLimitTier:  def.RateLimitTier,
		MonthlyCredits: def.MonthlyCredits,
		DailyLLMUSDCap: def.DailyLLMUSDCap,
	}
}
