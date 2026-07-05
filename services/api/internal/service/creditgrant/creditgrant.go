// Package creditgrant grants each active business its plan's monthly credit
// allowance, idempotently per (business, period). It is the write side of the
// v1.6 billing credit model: without it credit_ledger carries no `grant` rows,
// so every business reads a 0 balance and the billing summary reports
// remaining=0 for everyone (see services/api/internal/service/billing_summary.go).
//
// It is DECOUPLED from the subscriptions table: Track-A never creates a
// subscription row per business, so a per-subscription grant loop would miss
// every business. Instead the service iterates active businesses and resolves
// each one's plan through the fail-safe BusinessPlanResolver (DB error / no
// subscription → Free), then grants that plan's monthly_credits for the current
// UTC billing period.
package creditgrant

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// periodFormat is the UTC 'YYYY-MM' billing-period key stored on
// credit_ledger.subscription_period and embedded in the idempotency key. It
// matches the UTC-month window the billing summary aggregates usage over, so a
// granted period and its usage line up on the same calendar month.
const periodFormat = "2006-01"

// Enumerator lists the businesses to grant. repository.CreditGrantExtAdapter satisfies it.
type Enumerator interface {
	EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error)
}

// Granter appends the idempotent monthly grant for one business.
// repository.CreditGrantExtAdapter satisfies it.
type Granter interface {
	GrantMonthly(ctx context.Context, businessID uuid.UUID, monthlyCredits int, period string) (bool, error)
}

// PlanResolver resolves a business's plan (fail-safe Free).
// *planresolver.Resolver satisfies it.
type PlanResolver interface {
	Resolve(ctx context.Context, businessID uuid.UUID) planresolver.Plan
}

// CacheInvalidator drops a business's cached plan after a grant so a subsequent
// summary read re-resolves it. *planresolver.Resolver satisfies it.
type CacheInvalidator interface {
	Invalidate(businessID uuid.UUID)
}

// Service grants monthly credits across the active-business fleet.
type Service struct {
	enum        Enumerator
	granter     Granter
	resolver    PlanResolver
	invalidator CacheInvalidator
	log         *slog.Logger
	now         func() time.Time
}

// New constructs the grant service. A nil logger falls back to slog.Default().
func New(enum Enumerator, granter Granter, resolver PlanResolver, invalidator CacheInvalidator, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		enum:        enum,
		granter:     granter,
		resolver:    resolver,
		invalidator: invalidator,
		log:         log,
		now:         time.Now,
	}
}

// GrantAll runs one grant pass over every active business for the current UTC
// billing period and returns how many businesses were newly granted (0 on a
// re-run in the same period — the grant is idempotent). Businesses whose plan
// has monthly_credits <= 0 (unlimited/none, or the hard Free fallback when the
// catalog is unreadable) are skipped. A single business's grant write failing is
// logged and skipped so one bad row never aborts the fleet-wide pass; only an
// enumeration failure aborts.
func (s *Service) GrantAll(ctx context.Context) (int, error) {
	period := s.now().UTC().Format(periodFormat)

	ids, err := s.enum.EnumerateActiveBusinessIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("creditgrant: enumerate active businesses: %w", err)
	}

	granted := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return granted, err
		}
		plan := s.resolver.Resolve(ctx, id)
		if plan.MonthlyCredits <= 0 {
			continue
		}
		did, err := s.granter.GrantMonthly(ctx, id, plan.MonthlyCredits, period)
		if err != nil {
			s.log.WarnContext(ctx, "creditgrant: grant failed",
				"business_id", id, "period", period, "err", err)
			continue
		}
		if did {
			granted++
			s.invalidator.Invalidate(id)
		}
	}
	return granted, nil
}
