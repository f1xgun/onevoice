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

type userRepository struct {
	pool *pgxpool.Pool
	sb   squirrel.StatementBuilderType
}

func NewUserRepository(pool *pgxpool.Pool) domain.UserRepository {
	return &userRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// UserResetExtAdapter is the public façade exposing GetByEmail +
// UpdatePasswordHashInTx — the slice of UserRepository the
// PasswordResetService (Phase 21b) consumes. Returned as a concrete type
// (not an interface) so wire/repositories.go can construct it without
// importing the service package (and avoid the type-assertion dance).
type UserResetExtAdapter struct {
	inner *userRepository
}

// NewUserResetExtAdapter constructs the Phase 21b extension repo. Re-uses
// the same connection pool as NewUserRepository — both struct values
// share state through the pgxpool's connection multiplex.
func NewUserResetExtAdapter(pool *pgxpool.Pool) *UserResetExtAdapter {
	return &UserResetExtAdapter{
		inner: &userRepository{
			pool: pool,
			sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
		},
	}
}

// GetByEmail delegates to the inner concrete repo.
func (a *UserResetExtAdapter) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return a.inner.GetByEmail(ctx, email)
}

// UpdatePasswordHashInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) UpdatePasswordHashInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bcryptHash []byte) error {
	return a.inner.UpdatePasswordHashInTx(ctx, tx, userID, bcryptHash)
}

func (r *userRepository) Create(ctx context.Context, user *domain.User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("users").
		Columns("id", "email", "password_hash", "created_at", "updated_at").
		Values(user.ID, user.Email, user.PasswordHash, user.CreatedAt, user.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, sql, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrUserExists
		}
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

func (r *userRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	sql, args, err := r.sb.
		Select("id", "email", "password_hash", "preferred_locale", "created_at", "updated_at").
		From("users").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var user domain.User
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.PreferredLocale,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	sql, args, err := r.sb.
		Select("id", "email", "password_hash", "preferred_locale", "created_at", "updated_at").
		From("users").
		Where(squirrel.Eq{"email": email}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var user domain.User
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.PreferredLocale,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

func (r *userRepository) Update(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now()

	sql, args, err := r.sb.
		Update("users").
		Set("email", user.Email).
		Set("password_hash", user.PasswordHash).
		Set("updated_at", user.UpdatedAt).
		Where(squirrel.Eq{"id": user.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// UpdatePasswordHashInTx sets users.password_hash + updated_at for the
// given userID inside the caller-supplied tx. Used by PasswordResetService
// to commit the password update in the same transaction as the token
// consume (Phase 21b D-12).
//
// NOT part of the domain.UserRepository interface — the interface stays
// tx-free for callers that don't compose transactions. Service callers
// type-assert against the concrete *userRepository to access this method
// (an adapter is registered in wire/services.go).
func (r *userRepository) UpdatePasswordHashInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bcryptHash []byte) error {
	sqlStr, args, err := r.sb.
		Update("users").
		Set("password_hash", string(bcryptHash)).
		Set("updated_at", time.Now()).
		Where(squirrel.Eq{"id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update password_hash: %w", err)
	}
	cmdTag, err := tx.Exec(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update password_hash: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// UpdatePreferredLocale sets users.preferred_locale for the row matching
// userID, also touching updated_at so audit-style queries notice the change.
// Returns domain.ErrUserNotFound when 0 rows matched (mirrors Update above).
//
// Validation of the locale value itself ('ru' | 'en') happens at the handler
// boundary. The DB CHECK constraint (migration 000010 prod / 000008 test — i18n Phase A3) is the defense-in-depth
// floor — passing an invalid value here surfaces as a pgx error, NOT
// ErrUserNotFound, because RowsAffected will be 0 only when id doesn't match.
func (r *userRepository) UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	sql, args, err := r.sb.
		Update("users").
		Set("preferred_locale", locale).
		Set("updated_at", time.Now()).
		Where(squirrel.Eq{"id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update preferred_locale: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update preferred_locale: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}
