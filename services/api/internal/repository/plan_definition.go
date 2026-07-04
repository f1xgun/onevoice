// Package repository — plan_definition.go
//
// planDefinitionRepository implements domain.PlanDefinitionRepository over the
// plan_definitions catalog (read-only in the API; rows are seeded by migration
// and edited by the founder via a follow-up migration).

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type planDefinitionRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

var _ domain.PlanDefinitionRepository = (*planDefinitionRepository)(nil)

// NewPlanDefinitionRepository returns the Postgres-backed catalog reader.
func NewPlanDefinitionRepository(pool pgxPool) domain.PlanDefinitionRepository {
	return &planDefinitionRepository{pool: pool, sb: newStatementBuilder()}
}

var planDefinitionColumns = []string{
	"code", "display_name", "price_rub", "monthly_credits",
	"overage_price_per_credit_rub", "daily_llm_usd_cap", "max_integrations",
	"max_members", "rate_limit_tier", "active", "sort_order",
	"created_at", "updated_at",
}

func scanPlanDefinition(row scanner, p *domain.PlanDefinition) error {
	return row.Scan(
		&p.Code, &p.DisplayName, &p.PriceRUB, &p.MonthlyCredits,
		&p.OveragePricePerCreditRUB, &p.DailyLLMUSDCap, &p.MaxIntegrations,
		&p.MaxMembers, &p.RateLimitTier, &p.Active, &p.SortOrder,
		&p.CreatedAt, &p.UpdatedAt,
	)
}

// GetByCode returns one plan, or domain.ErrPlanNotFound.
func (r *planDefinitionRepository) GetByCode(ctx context.Context, code string) (*domain.PlanDefinition, error) {
	sql, args, err := r.sb.
		Select(planDefinitionColumns...).
		From("plan_definitions").
		Where(squirrel.Eq{"code": code}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("GetByCode: build sql: %w", err)
	}

	var p domain.PlanDefinition
	if err := scanPlanDefinition(r.pool.QueryRow(ctx, sql, args...), &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPlanNotFound
		}
		return nil, fmt.Errorf("GetByCode: exec: %w", err)
	}
	return &p, nil
}

// ListActive returns active plans ordered by sort_order.
func (r *planDefinitionRepository) ListActive(ctx context.Context) ([]domain.PlanDefinition, error) {
	sql, args, err := r.sb.
		Select(planDefinitionColumns...).
		From("plan_definitions").
		Where(squirrel.Eq{"active": true}).
		OrderBy("sort_order ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("ListActive: build sql: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("ListActive: query: %w", err)
	}
	defer rows.Close()

	plans := make([]domain.PlanDefinition, 0)
	for rows.Next() {
		var p domain.PlanDefinition
		if err := scanPlanDefinition(rows, &p); err != nil {
			return nil, fmt.Errorf("ListActive: scan: %w", err)
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListActive: rows: %w", err)
	}
	return plans, nil
}
