// Package repository — subscription.go
//
// subscriptionRepository implements domain.SubscriptionRepository over the
// business-keyed subscriptions table. ActiveByBusiness is the Track-A read path
// (consumed by BusinessPlanResolver). Upsert is the Track-B write path, defined
// now so the interface is stable but never called in Track-A.

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type subscriptionRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

var _ domain.SubscriptionRepository = (*subscriptionRepository)(nil)

// NewSubscriptionRepository returns the Postgres-backed SubscriptionRepository.
func NewSubscriptionRepository(pool pgxPool) domain.SubscriptionRepository {
	return &subscriptionRepository{pool: pool, sb: newStatementBuilder()}
}

// subscriptionColumns is the shared SELECT column list, kept in one place so the
// scan order cannot drift from the query.
var subscriptionColumns = []string{
	"id", "business_id", "parent_business_id", "plan_code", "status",
	"period_start", "period_end", "provider", "provider_sub_id",
	"cancel_at_period_end", "created_at", "updated_at",
}

// ActiveByBusiness returns the single active subscription for a business, or
// domain.ErrSubscriptionNotFound when none exists.
func (r *subscriptionRepository) ActiveByBusiness(ctx context.Context, businessID uuid.UUID) (*domain.Subscription, error) {
	sql, args, err := r.sb.
		Select(subscriptionColumns...).
		From("subscriptions").
		Where(squirrel.Eq{"business_id": businessID, "status": domain.SubscriptionStatusActive}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("ActiveByBusiness: build sql: %w", err)
	}

	var s domain.Subscription
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&s.ID, &s.BusinessID, &s.ParentBusinessID, &s.PlanCode, &s.Status,
		&s.PeriodStart, &s.PeriodEnd, &s.Provider, &s.ProviderSubID,
		&s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSubscriptionNotFound
		}
		return nil, fmt.Errorf("ActiveByBusiness: exec: %w", err)
	}
	return &s, nil
}

// Upsert is the Track-B write path (checkout / webhook). It inserts a
// subscription, or updates the business's active row on conflict with the
// partial unique index uq_subscriptions_business_active. Never called in
// Track-A.
func (r *subscriptionRepository) Upsert(ctx context.Context, sub *domain.Subscription) error {
	if sub == nil {
		return fmt.Errorf("Upsert: subscription is required")
	}
	if sub.BusinessID == uuid.Nil {
		return fmt.Errorf("Upsert: business_id is required")
	}
	if sub.ID == uuid.Nil {
		sub.ID = uuid.New()
	}
	if sub.Status == "" {
		sub.Status = domain.SubscriptionStatusActive
	}
	if sub.PlanCode == "" {
		sub.PlanCode = "free"
	}

	sql, args, err := r.sb.
		Insert("subscriptions").
		Columns(
			"id", "business_id", "parent_business_id", "plan_code", "status",
			"period_start", "period_end", "provider", "provider_sub_id",
			"cancel_at_period_end",
		).
		Values(
			sub.ID, sub.BusinessID, sub.ParentBusinessID, sub.PlanCode, sub.Status,
			sub.PeriodStart, sub.PeriodEnd, sub.Provider, sub.ProviderSubID,
			sub.CancelAtPeriodEnd,
		).
		Suffix(`ON CONFLICT (business_id) WHERE status = 'active' DO UPDATE SET ` +
			`parent_business_id = EXCLUDED.parent_business_id, ` +
			`plan_code = EXCLUDED.plan_code, ` +
			`status = EXCLUDED.status, ` +
			`period_start = EXCLUDED.period_start, ` +
			`period_end = EXCLUDED.period_end, ` +
			`provider = EXCLUDED.provider, ` +
			`provider_sub_id = EXCLUDED.provider_sub_id, ` +
			`cancel_at_period_end = EXCLUDED.cancel_at_period_end, ` +
			`updated_at = now()`).
		ToSql()
	if err != nil {
		return fmt.Errorf("Upsert: build sql: %w", err)
	}
	if _, err := r.pool.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("Upsert: exec: %w", err)
	}
	return nil
}
