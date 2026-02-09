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

type IntegrationRepository struct {
	pool *pgxpool.Pool
	sb   squirrel.StatementBuilderType
}

func NewIntegrationRepository(pool *pgxpool.Pool) *IntegrationRepository {
	return &IntegrationRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

func (r *IntegrationRepository) Create(ctx context.Context, integration *domain.Integration) error {
	if integration.ID == uuid.Nil {
		integration.ID = uuid.New()
	}
	now := time.Now()
	integration.CreatedAt = now
	integration.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("integrations").
		Columns("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		Values(integration.ID, integration.BusinessID, integration.Platform, integration.Status, integration.EncryptedAccessToken, integration.EncryptedRefreshToken, integration.ExternalID, integration.Metadata, integration.TokenExpiresAt, integration.CreatedAt, integration.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build insert query: %w", err)
	}

	_, err = r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to insert integration: %w", err)
	}

	return nil
}

func (r *IntegrationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	var integration domain.Integration
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&integration.ID,
		&integration.BusinessID,
		&integration.Platform,
		&integration.Status,
		&integration.EncryptedAccessToken,
		&integration.EncryptedRefreshToken,
		&integration.ExternalID,
		&integration.Metadata,
		&integration.TokenExpiresAt,
		&integration.CreatedAt,
		&integration.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("failed to get integration by id: %w", err)
	}

	return &integration, nil
}

func (r *IntegrationRepository) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{
			"business_id": businessID,
			"platform":    platform,
		}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	var integration domain.Integration
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&integration.ID,
		&integration.BusinessID,
		&integration.Platform,
		&integration.Status,
		&integration.EncryptedAccessToken,
		&integration.EncryptedRefreshToken,
		&integration.ExternalID,
		&integration.Metadata,
		&integration.TokenExpiresAt,
		&integration.CreatedAt,
		&integration.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("failed to get integration by business and platform: %w", err)
	}

	return &integration, nil
}

func (r *IntegrationRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"business_id": businessID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list integrations: %w", err)
	}
	defer rows.Close()

	var integrations []domain.Integration
	for rows.Next() {
		var integration domain.Integration
		err := rows.Scan(
			&integration.ID,
			&integration.BusinessID,
			&integration.Platform,
			&integration.Status,
			&integration.EncryptedAccessToken,
			&integration.EncryptedRefreshToken,
			&integration.ExternalID,
			&integration.Metadata,
			&integration.TokenExpiresAt,
			&integration.CreatedAt,
			&integration.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan integration: %w", err)
		}
		integrations = append(integrations, integration)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return integrations, nil
}

func (r *IntegrationRepository) Update(ctx context.Context, integration *domain.Integration) error {
	integration.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("integrations").
		Set("status", integration.Status).
		Set("encrypted_access_token", integration.EncryptedAccessToken).
		Set("encrypted_refresh_token", integration.EncryptedRefreshToken).
		Set("external_id", integration.ExternalID).
		Set("metadata", integration.Metadata).
		Set("token_expires_at", integration.TokenExpiresAt).
		Set("updated_at", integration.UpdatedAt).
		Where(squirrel.Eq{"id": integration.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update query: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to update integration: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}

func (r *IntegrationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	sql, args, err := r.sb.
		Delete("integrations").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build delete query: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to delete integration: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}
