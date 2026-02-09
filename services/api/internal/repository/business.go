package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BusinessRepository struct {
	pool *pgxpool.Pool
	sb   squirrel.StatementBuilderType
}

func NewBusinessRepository(pool *pgxpool.Pool) *BusinessRepository {
	return &BusinessRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

func (r *BusinessRepository) Create(ctx context.Context, business *domain.Business) error {
	if business.ID == uuid.Nil {
		business.ID = uuid.New()
	}
	now := time.Now()
	business.CreatedAt = now
	business.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("businesses").
		Columns("id", "user_id", "name", "category", "address", "phone", "description", "logo_url", "settings", "created_at", "updated_at").
		Values(business.ID, business.UserID, business.Name, business.Category, business.Address, business.Phone, business.Description, business.LogoURL, business.Settings, business.CreatedAt, business.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build insert query: %w", err)
	}

	_, err = r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to insert business: %w", err)
	}

	return nil
}

func (r *BusinessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.sb.
		Select("id", "user_id", "name", "category", "address", "phone", "description", "logo_url", "settings", "created_at", "updated_at").
		From("businesses").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	var business domain.Business
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&business.ID,
		&business.UserID,
		&business.Name,
		&business.Category,
		&business.Address,
		&business.Phone,
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
		return nil, fmt.Errorf("failed to get business by id: %w", err)
	}

	return &business, nil
}

func (r *BusinessRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	sql, args, err := r.sb.
		Select("id", "user_id", "name", "category", "address", "phone", "description", "logo_url", "settings", "created_at", "updated_at").
		From("businesses").
		Where(squirrel.Eq{"user_id": userID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	var business domain.Business
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&business.ID,
		&business.UserID,
		&business.Name,
		&business.Category,
		&business.Address,
		&business.Phone,
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
		return nil, fmt.Errorf("failed to get business by user id: %w", err)
	}

	return &business, nil
}

func (r *BusinessRepository) Update(ctx context.Context, business *domain.Business) error {
	business.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("businesses").
		Set("name", business.Name).
		Set("category", business.Category).
		Set("address", business.Address).
		Set("phone", business.Phone).
		Set("description", business.Description).
		Set("logo_url", business.LogoURL).
		Set("settings", business.Settings).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": business.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update query: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to update business: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}

	return nil
}
