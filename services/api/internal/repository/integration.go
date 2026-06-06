package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type integrationRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

func NewIntegrationRepository(pool pgxPool) domain.IntegrationRepository {
	return &integrationRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// integrationColumns is the canonical column order shared by every
// integrations SELECT so scanIntegration stays in lockstep with the query.
var integrationColumns = []string{
	"id", "business_id", "platform", "status",
	"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
	"external_id", "metadata", "token_expires_at", "user_token_expires_at",
	"created_at", "updated_at",
}

// scanIntegration maps one integrations row into a domain.Integration. Shared
// by the QueryRow Get paths and the CollectRows List paths.
func scanIntegration(row scanner) (domain.Integration, error) {
	var integration domain.Integration
	err := row.Scan(
		&integration.ID,
		&integration.BusinessID,
		&integration.Platform,
		&integration.Status,
		&integration.EncryptedAccessToken,
		&integration.EncryptedRefreshToken,
		&integration.EncryptedUserToken,
		&integration.ExternalID,
		&integration.Metadata,
		&integration.TokenExpiresAt,
		&integration.UserTokenExpiresAt,
		&integration.CreatedAt,
		&integration.UpdatedAt,
	)
	return integration, err
}

func (r *integrationRepository) Create(ctx context.Context, integration *domain.Integration) error {
	if integration.ID == uuid.Nil {
		integration.ID = uuid.New()
	}
	now := time.Now()
	integration.CreatedAt = now
	integration.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("integrations").
		Columns("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token", "external_id", "metadata", "token_expires_at", "user_token_expires_at", "created_at", "updated_at").
		Values(integration.ID, integration.BusinessID, integration.Platform, integration.Status, integration.EncryptedAccessToken, integration.EncryptedRefreshToken, integration.EncryptedUserToken, integration.ExternalID, integration.Metadata, integration.TokenExpiresAt, integration.UserTokenExpiresAt, integration.CreatedAt, integration.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, sql, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrIntegrationExists
		}
		return fmt.Errorf("insert integration: %w", err)
	}

	return nil
}

func (r *integrationRepository) activeQuery() squirrel.SelectBuilder {
	return r.sb.
		Select(integrationColumns...).
		From("integrations").
		Where(squirrel.Eq{"deleted_at": nil})
}

func (r *integrationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
	sql, args, err := r.activeQuery().
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	integration, err := scanIntegration(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("query integration: %w", err)
	}

	return &integration, nil
}

func (r *integrationRepository) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	sql, args, err := r.activeQuery().
		Where(squirrel.Eq{
			"business_id": businessID,
			"platform":    platform,
		}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	integration, err := scanIntegration(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("query integration: %w", err)
	}

	return &integration, nil
}

func (r *integrationRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	sql, args, err := r.activeQuery().
		Where(squirrel.Eq{"business_id": businessID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query integrations: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Integration, error) {
		return scanIntegration(row)
	})
}

func (r *integrationRepository) Update(ctx context.Context, integration *domain.Integration) error {
	integration.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("integrations").
		Set("status", integration.Status).
		Set("encrypted_access_token", integration.EncryptedAccessToken).
		Set("encrypted_refresh_token", integration.EncryptedRefreshToken).
		Set("encrypted_user_token", integration.EncryptedUserToken).
		Set("external_id", integration.ExternalID).
		Set("metadata", integration.Metadata).
		Set("token_expires_at", integration.TokenExpiresAt).
		Set("user_token_expires_at", integration.UserTokenExpiresAt).
		Set("updated_at", integration.UpdatedAt).
		Where(squirrel.Eq{"id": integration.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update integration: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}

func (r *integrationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	sql, args, err := r.sb.
		Delete("integrations").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}

// SoftDelete marks an integration as deleted by stamping deleted_at; reads
// through activeQuery() then treat it as not-found. Idempotent re-delete is a
// no-op that returns ErrIntegrationNotFound.
func (r *integrationRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	sql, args, err := r.sb.
		Update("integrations").
		Set("deleted_at", now).
		Set("updated_at", now).
		Where(squirrel.And{
			squirrel.Eq{"id": id},
			squirrel.Eq{"deleted_at": nil},
		}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build soft-delete: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("soft-delete integration: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}

// DeleteOlderThan hard-deletes soft-deleted integrations whose tombstone is
// older than cutoff; it is the retention purge path and intentionally bypasses
// activeQuery(). Returns the number of rows removed.
func (r *integrationRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	sql, args, err := r.sb.
		Delete("integrations").
		Where(squirrel.And{
			squirrel.NotEq{"deleted_at": nil},
			squirrel.Lt{"deleted_at": cutoff},
		}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build purge: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("purge integrations: %w", err)
	}

	return cmdTag.RowsAffected(), nil
}

func (r *integrationRepository) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	sql, args, err := r.activeQuery().
		Where(squirrel.Eq{"business_id": businessID, "platform": platform}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query integrations: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Integration, error) {
		return scanIntegration(row)
	})
}

func (r *integrationRepository) ListAllActiveByPlatforms(ctx context.Context, platforms []string) ([]domain.Integration, error) {
	sql, args, err := r.activeQuery().
		Where(ActiveIntegrationStatusEq()).
		Where(squirrel.Eq{"platform": platforms}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query integrations: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Integration, error) {
		return scanIntegration(row)
	})
}

func (r *integrationRepository) GetByBusinessPlatformExternal(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*domain.Integration, error) {
	sql, args, err := r.activeQuery().
		Where(squirrel.Eq{"business_id": businessID, "platform": platform, "external_id": externalID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	integration, err := scanIntegration(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("query integration: %w", err)
	}
	return &integration, nil
}
