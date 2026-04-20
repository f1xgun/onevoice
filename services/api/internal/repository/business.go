package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type businessRepository struct {
	pool *pgxpool.Pool
	sb   squirrel.StatementBuilderType
}

func NewBusinessRepository(pool *pgxpool.Pool) domain.BusinessRepository {
	return &businessRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

func (r *businessRepository) Create(ctx context.Context, business *domain.Business) error {
	if business.ID == uuid.Nil {
		business.ID = uuid.New()
	}
	now := time.Now()
	business.CreatedAt = now
	business.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("businesses").
		Columns("id", "user_id", "name", "category", "address", "phone", "website", "description", "logo_url", "settings", "created_at", "updated_at").
		Values(business.ID, business.UserID, business.Name, business.Category, business.Address, business.Phone, business.Website, business.Description, business.LogoURL, business.Settings, business.CreatedAt, business.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, sql, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBusinessExists
		}
		return fmt.Errorf("insert business: %w", err)
	}

	return nil
}

func (r *businessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.sb.
		Select("id", "user_id", "name", "category", "address", "phone", "website", "description", "logo_url", "settings", "created_at", "updated_at").
		From("businesses").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var business domain.Business
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&business.ID,
		&business.UserID,
		&business.Name,
		&business.Category,
		&business.Address,
		&business.Phone,
		&business.Website,
		&business.Description,
		&business.LogoURL,
		&business.Settings,
		&business.CreatedAt,
		&business.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}

	return &business, nil
}

func (r *businessRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.sb.
		Select("id", "user_id", "name", "category", "address", "phone", "website", "description", "logo_url", "settings", "created_at", "updated_at").
		From("businesses").
		Where(squirrel.Eq{"user_id": userID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var business domain.Business
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&business.ID,
		&business.UserID,
		&business.Name,
		&business.Category,
		&business.Address,
		&business.Phone,
		&business.Website,
		&business.Description,
		&business.LogoURL,
		&business.Settings,
		&business.CreatedAt,
		&business.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}

	return &business, nil
}

// UpdateToolApprovals replaces only the settings.tool_approvals sub-object on
// the given business. Other keys inside settings (e.g. schedule) are
// preserved — this is a MERGE on the top-level settings JSONB, but a REPLACE
// on the tool_approvals sub-object: a key removed from the PUT body becomes
// un-approved (no longer in the persisted map).
//
// Implementation: load current settings, mutate the tool_approvals key,
// write the merged settings back with Update. Done under a single pgx Exec
// call (no transaction) because this is standalone Postgres and a lost
// update only affects settings races which the frontend serializes via
// React Query's mutate() pattern.
//
// Phase 16 (POLICY-05): feeds the PUT /api/v1/business/{id}/tool-approvals
// endpoint.
func (r *businessRepository) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	// Load current business so we keep unrelated settings keys intact.
	business, err := r.GetByID(ctx, businessID)
	if err != nil {
		return err
	}
	if business.Settings == nil {
		business.Settings = make(map[string]interface{})
	}
	// Translate ToolFloor map into map[string]interface{} for the JSONB blob.
	raw := make(map[string]interface{}, len(approvals))
	for k, v := range approvals {
		raw[k] = string(v)
	}
	business.Settings["tool_approvals"] = raw
	business.UpdatedAt = time.Now()

	sql, args, buildErr := r.sb.
		Update("businesses").
		Set("settings", business.Settings).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": businessID}).
		ToSql()
	if buildErr != nil {
		return fmt.Errorf("build update: %w", buildErr)
	}

	tag, execErr := r.pool.Exec(ctx, sql, args...)
	if execErr != nil {
		return fmt.Errorf("update business settings: %w", execErr)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}
	return nil
}

func (r *businessRepository) Update(ctx context.Context, business *domain.Business) error {
	business.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("businesses").
		Set("name", business.Name).
		Set("category", business.Category).
		Set("address", business.Address).
		Set("phone", business.Phone).
		Set("website", business.Website).
		Set("description", business.Description).
		Set("logo_url", business.LogoURL).
		Set("settings", business.Settings).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": business.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update business: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}

	return nil
}
