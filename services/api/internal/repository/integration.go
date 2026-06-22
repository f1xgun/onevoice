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
// wrapped_dek MUST be selected here — DecryptToken needs WrappedDEK from the
// row, so a read path that omits it leaves WrappedDEK nil, silently falls back
// to the legacy key, and fails to decrypt envelope-encrypted tokens.
// key_version / encryption_key_fingerprint are intentionally NOT read here:
// the decrypt path doesn't use them, and the rekey job reads them through its
// own SelectForRekey query.
var integrationColumns = []string{
	"id", "business_id", "platform", "status",
	"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
	"external_id", "metadata", "token_expires_at", "user_token_expires_at",
	"created_at", "updated_at",
	"wrapped_dek",
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
		&integration.WrappedDEK,
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
		Columns("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token", "external_id", "metadata", "token_expires_at", "user_token_expires_at", "created_at", "updated_at", "wrapped_dek", "key_version", "encryption_key_fingerprint").
		Values(integration.ID, integration.BusinessID, integration.Platform, integration.Status, integration.EncryptedAccessToken, integration.EncryptedRefreshToken, integration.EncryptedUserToken, integration.ExternalID, integration.Metadata, integration.TokenExpiresAt, integration.UserTokenExpiresAt, integration.CreatedAt, integration.UpdatedAt, integration.WrappedDEK, integration.KeyVersion, integration.EncryptionKeyFingerprint).
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
		Set("wrapped_dek", integration.WrappedDEK).
		Set("key_version", integration.KeyVersion).
		Set("encryption_key_fingerprint", integration.EncryptionKeyFingerprint).
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

// MarkTokenExpired flips matching active, non-deleted integrations to
// token_expired and returns the rows affected. When externalID is non-empty the
// flip is scoped to that one integration; an empty externalID flips every active
// integration for (businessID, platform). Zero rows is a benign no-op (nothing
// active to flip, or already flipped).
//
// The externalID is derived from raw LLM-supplied tool arguments, which the
// agents normalize before use (Telegram resolves an empty/business-name id to
// the integration's stored id; VK rewrites "123" → "-123"). When the supplied
// form differs from the stored external_id the scoped flip matches zero rows,
// so a non-empty externalID that affects nothing AND matches no stored
// integration is treated as an id-form mismatch and falls back to the
// platform-wide flip, ensuring the genuinely-broken integration is still
// flipped. The existence check guards that fallback: if the supplied id DOES
// match a stored integration that simply was not active (already
// expired/disconnected), zero rows is returned WITHOUT falling back, otherwise
// the fallback would collateral-flip a healthy sibling integration.
func (r *integrationRepository) MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) (int64, error) {
	affected, err := r.markTokenExpiredWhere(ctx, businessID, platform, externalID)
	if err != nil {
		return 0, err
	}
	if affected > 0 || externalID == "" {
		return affected, nil
	}

	exists, err := r.integrationExists(ctx, businessID, platform, externalID)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, nil
	}

	return r.markTokenExpiredWhere(ctx, businessID, platform, "")
}

// markTokenExpiredWhere runs the token_expired UPDATE scoped to (businessID,
// platform), narrowing to external_id when non-empty, and returns the rows
// affected. It is the single source of the flip SQL shared by both the scoped
// attempt and the platform-wide fallback.
func (r *integrationRepository) markTokenExpiredWhere(ctx context.Context, businessID uuid.UUID, platform, externalID string) (int64, error) {
	where := squirrel.Eq{
		"business_id": businessID,
		"platform":    platform,
		"status":      domain.IntegrationStatusActive,
		"deleted_at":  nil,
	}
	if externalID != "" {
		where["external_id"] = externalID
	}

	sql, args, err := r.sb.
		Update("integrations").
		Set("status", domain.IntegrationStatusTokenExpired).
		Set("updated_at", time.Now()).
		Where(where).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build mark-token-expired: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("mark token expired: %w", err)
	}

	return cmdTag.RowsAffected(), nil
}

// integrationExists reports whether any non-deleted integration row exists for
// (businessID, platform, externalID), regardless of status. It distinguishes an
// id that maps to a present-but-inactive integration (no fallback) from an id
// that maps to nothing (fall back to the platform-wide flip).
func (r *integrationRepository) integrationExists(ctx context.Context, businessID uuid.UUID, platform, externalID string) (bool, error) {
	inner := r.sb.
		Select("1").
		From("integrations").
		Where(squirrel.Eq{
			"business_id": businessID,
			"platform":    platform,
			"external_id": externalID,
			"deleted_at":  nil,
		})
	innerSQL, innerArgs, err := inner.ToSql()
	if err != nil {
		return false, fmt.Errorf("build integration-exists inner: %w", err)
	}

	sql, args, err := r.sb.
		Select(fmt.Sprintf("EXISTS(%s)", innerSQL)).
		ToSql()
	if err != nil {
		return false, fmt.Errorf("build integration-exists: %w", err)
	}

	var exists bool
	if err := r.pool.QueryRow(ctx, sql, append(args, innerArgs...)...).Scan(&exists); err != nil {
		return false, fmt.Errorf("integration exists: %w", err)
	}
	return exists, nil
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

func (r *integrationRepository) CountIntegrationsWithDifferentFingerprint(ctx context.Context, currentFP string) (int, error) {
	const q = `SELECT count(*) FROM integrations
	           WHERE encryption_key_fingerprint IS NOT NULL
	             AND encryption_key_fingerprint <> $1
	             AND deleted_at IS NULL`
	var n int
	if err := r.pool.QueryRow(ctx, q, currentFP).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: count fingerprint mismatch: %w", err)
	}
	return n, nil
}

func (r *integrationRepository) SelectForRekey(ctx context.Context, tx pgx.Tx, targetVersion int16, limit int) ([]domain.Integration, error) {
	const q = `
		SELECT id, business_id, platform, external_id,
		       encrypted_access_token, encrypted_refresh_token, encrypted_user_token,
		       wrapped_dek, key_version, encryption_key_fingerprint
		FROM integrations
		WHERE (wrapped_dek IS NULL OR key_version < $1)
		  AND deleted_at IS NULL
		ORDER BY id
		LIMIT $2
		FOR UPDATE SKIP LOCKED`
	rows, err := tx.Query(ctx, q, targetVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: select for rekey: %w", err)
	}
	defer rows.Close()

	var out []domain.Integration
	for rows.Next() {
		var i domain.Integration
		var keyVersion *int16
		var fp *string
		if err := rows.Scan(
			&i.ID, &i.BusinessID, &i.Platform, &i.ExternalID,
			&i.EncryptedAccessToken, &i.EncryptedRefreshToken, &i.EncryptedUserToken,
			&i.WrappedDEK, &keyVersion, &fp,
		); err != nil {
			return nil, fmt.Errorf("repo: scan for rekey: %w", err)
		}
		if keyVersion != nil {
			i.KeyVersion = *keyVersion
		}
		if fp != nil {
			i.EncryptionKeyFingerprint = *fp
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repo: rows err for rekey: %w", err)
	}
	return out, nil
}

func (r *integrationRepository) UpdateEnvelopeFieldsTx(ctx context.Context, tx pgx.Tx, i domain.Integration) error {
	const q = `
		UPDATE integrations
		SET encrypted_access_token = $1,
		    encrypted_refresh_token = $2,
		    encrypted_user_token = $3,
		    wrapped_dek = $4,
		    key_version = $5,
		    encryption_key_fingerprint = $6,
		    updated_at = NOW()
		WHERE id = $7`
	ct, err := tx.Exec(ctx, q,
		i.EncryptedAccessToken,
		i.EncryptedRefreshToken,
		i.EncryptedUserToken,
		i.WrappedDEK,
		i.KeyVersion,
		i.EncryptionKeyFingerprint,
		i.ID,
	)
	if err != nil {
		return fmt.Errorf("repo: update envelope: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("repo: update envelope: no rows affected for id=%s", i.ID)
	}
	return nil
}

func (r *integrationRepository) CountRekeyRemaining(ctx context.Context, targetVersion int16) (int, error) {
	const q = `SELECT count(*) FROM integrations
	           WHERE (wrapped_dek IS NULL OR key_version < $1)
	             AND deleted_at IS NULL`
	var n int
	if err := r.pool.QueryRow(ctx, q, targetVersion).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: count rekey remaining: %w", err)
	}
	return n, nil
}
