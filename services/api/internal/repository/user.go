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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// userRepository persists users in PostgreSQL. pool is the package-local
// pgxPool interface (pool.go) so unit tests can pass a pgxmock pool.
// See docs/api/repositories/user.md.
type userRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// NewUserRepository constructs the Postgres-backed user repository
// exposing the tx-free domain.UserRepository surface.
func NewUserRepository(pool *pgxpool.Pool) domain.UserRepository {
	return &userRepository{
		pool: pool,
		sb:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// UserResetExtAdapter exposes the tx-aware slice of userRepository that
// PasswordResetService, EmailVerificationService and AccountDeletionService
// consume. Concrete type so wire/repositories.go does not need to import
// the service package.
// See docs/api/repositories/user.md.
type UserResetExtAdapter struct {
	inner *userRepository
}

// NewUserResetExtAdapter constructs the extension repo sharing the pool
// with NewUserRepository via the pgxpool connection multiplex.
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

// GetByID delegates to the inner concrete repo; consumed by callers that
// must load user state (email_verified, email) before mutating.
func (a *UserResetExtAdapter) GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return a.inner.GetByID(ctx, userID)
}

// GetByIDIncludingDeleted delegates to the inner concrete repo; lets
// AccountDeletionService.RequestDeletion detect a soft-deleted user and
// return ErrDeletionAlreadyPending instead of ErrUserNotFound.
func (a *UserResetExtAdapter) GetByIDIncludingDeleted(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return a.inner.GetByIDIncludingDeleted(ctx, userID)
}

// GetByIDIncludingDeletedInTx delegates to the inner concrete repo so the
// hard-delete sweeper can re-read deletion state inside its held tx.
func (a *UserResetExtAdapter) GetByIDIncludingDeletedInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*domain.User, error) {
	return a.inner.GetByIDIncludingDeletedInTx(ctx, tx, userID)
}

// RequestDeletionInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) RequestDeletionInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return a.inner.RequestDeletionInTx(ctx, tx, userID)
}

// CancelDeletion delegates to the inner concrete repo.
func (a *UserResetExtAdapter) CancelDeletion(ctx context.Context, userID uuid.UUID, graceDays int) (bool, error) {
	return a.inner.CancelDeletion(ctx, userID, graceDays)
}

// EnumeratePendingDeletionsInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error) {
	return a.inner.EnumeratePendingDeletionsInTx(ctx, tx, before, limit)
}

// EnumerateUpcomingDeletions delegates to the inner concrete repo.
func (a *UserResetExtAdapter) EnumerateUpcomingDeletions(ctx context.Context, fromTime, toTime time.Time, limit int) ([]*domain.User, error) {
	return a.inner.EnumerateUpcomingDeletions(ctx, fromTime, toTime, limit)
}

// HardDeleteInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) HardDeleteInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return a.inner.HardDeleteInTx(ctx, tx, userID)
}

// CreateInTx delegates to the inner concrete repo so the user row commits
// atomically with consents/verification/outbox INSERTs.
func (a *UserResetExtAdapter) CreateInTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	return a.inner.CreateInTx(ctx, tx, user)
}

// UpdatePasswordHashInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) UpdatePasswordHashInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bcryptHash []byte) error {
	return a.inner.UpdatePasswordHashInTx(ctx, tx, userID, bcryptHash)
}

// UpdateEmailInTx delegates to the inner concrete repo so users.email
// mutates inside the same tx as token invalidation + fresh-token issuance.
func (a *UserResetExtAdapter) UpdateEmailInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, newEmail string) error {
	return a.inner.UpdateEmailInTx(ctx, tx, userID, newEmail)
}

// MarkEmailVerifiedInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) MarkEmailVerifiedInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return a.inner.MarkEmailVerifiedInTx(ctx, tx, userID)
}

// Create inserts a user via the pool; used by test paths that don't compose
// a surrounding transaction. Production registration uses CreateInTx.
func (r *userRepository) Create(ctx context.Context, user *domain.User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("users").
		Columns("id", "email", "name", "password_hash", "created_at", "updated_at").
		Values(user.ID, user.Email, user.Name, user.PasswordHash, user.CreatedAt, user.UpdatedAt).
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

// CreateInTx inserts a user inside the caller-supplied tx so the user row
// commits atomically with user_consents + email_verification_tokens +
// email_outbox INSERTs (no half-registered user without verification email).
func (r *userRepository) CreateInTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	sql, args, err := r.sb.
		Insert("users").
		Columns("id", "email", "name", "password_hash", "created_at", "updated_at").
		Values(user.ID, user.Email, user.Name, user.PasswordHash, user.CreatedAt, user.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert tx: %w", err)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrUserExists
		}
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrUserExists
		}
		return fmt.Errorf("insert user tx: %w", err)
	}
	return nil
}

// userColumns is the canonical select order shared by the Get paths and
// EnumerateUpcomingDeletions. `email_verified` is COALESCE-wrapped so a NULL
// column reads as FALSE.
var userColumns = []string{
	"id", "email", "name", "password_hash", "preferred_locale",
	"COALESCE(email_verified, FALSE) AS email_verified",
	"email_verified_at",
	"deleted_at", "deletion_requested_at", "deletion_canceled_at",
	"created_at", "updated_at",
}

// scanUser maps one users row into a domain.User in the canonical select
// order shared by the Get paths and EnumerateUpcomingDeletions.
func scanUser(row scanner) (domain.User, error) {
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.PasswordHash,
		&user.PreferredLocale,
		&user.EmailVerified,
		&user.EmailVerifiedAt,
		&user.DeletedAt,
		&user.DeletionRequestedAt,
		&user.DeletionCanceledAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	return user, err
}

// GetByID returns the active user row matching id. Soft-deleted rows are
// filtered out via `deleted_at IS NULL` and surface as ErrUserNotFound;
// deletion-aware callers use GetByIDIncludingDeleted.
func (r *userRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	sql, args, err := r.sb.
		Select(userColumns...).
		From("users").
		Where(squirrel.Eq{"id": id}).
		Where("deleted_at IS NULL").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	user, err := scanUser(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// includingDeletedSQL builds the SELECT-by-id query (no `deleted_at IS NULL`
// filter) shared by the pool-based and tx-based deletion-aware reads so the
// column list never drifts between them.
func (r *userRepository) includingDeletedSQL(id uuid.UUID) (sql string, args []any, err error) {
	return r.sb.
		Select(userColumns...).
		From("users").
		Where(squirrel.Eq{"id": id}).
		ToSql()
}

// GetByIDIncludingDeleted is the same SELECT as GetByID minus the
// `deleted_at IS NULL` filter; lets callers read the account-deletion state
// of a soft-deleted user (grace banner, restore endpoint).
func (r *userRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	sql, args, err := r.includingDeletedSQL(id)
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	user, err := scanUser(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// GetByIDIncludingDeletedInTx is GetByIDIncludingDeleted reading through the
// caller-supplied tx instead of the pool, so the hard-delete sweeper's
// deletion_canceled_at re-check runs on the same connection/snapshot as the
// subsequent delete (consistency-by-construction). Defense-in-depth: the
// re-check no longer depends on the enumeration query continuing to hold its
// FOR UPDATE locks to be correct.
func (r *userRepository) GetByIDIncludingDeletedInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.User, error) {
	sql, args, err := r.includingDeletedSQL(id)
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	user, err := scanUser(tx.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// GetByEmail returns the active user row matching email; filters
// `deleted_at IS NULL` so soft-deleted accounts don't bleed into reads.
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	sql, args, err := r.sb.
		Select("id", "email", "name", "password_hash", "preferred_locale",
			"COALESCE(email_verified, FALSE) AS email_verified",
			"email_verified_at",
			"deleted_at", "deletion_requested_at", "deletion_canceled_at",
			"created_at", "updated_at").
		From("users").
		Where(squirrel.Eq{"email": email}).
		Where("deleted_at IS NULL").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	user, err := scanUser(r.pool.QueryRow(ctx, sql, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// Update mutates email + password_hash + updated_at via the pool.
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

// UpdatePasswordHashInTx sets users.password_hash + updated_at inside the
// caller-supplied tx so the password update commits with the token consume.
// Not on domain.UserRepository so the interface stays tx-free.
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

// UpdateEmailInTx sets users.email inside the caller-supplied tx (PATCH
// /auth/email-before-verify). Maps unique-violation → ErrEmailTaken so
// concurrent change-email/register races surface the friendly sentinel.
func (r *userRepository) UpdateEmailInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, newEmail string) error {
	sqlStr, args, err := r.sb.
		Update("users").
		Set("email", newEmail).
		Set("updated_at", time.Now()).
		Where(squirrel.Eq{"id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update email: %w", err)
	}
	cmdTag, err := tx.Exec(ctx, sqlStr, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrEmailTaken
		}
		return fmt.Errorf("update email: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// MarkEmailVerifiedInTx flips email_verified=TRUE and stamps
// email_verified_at=NOW inside the caller-supplied tx so a connection drop
// cannot leave "token consumed but flag not flipped".
func (r *userRepository) MarkEmailVerifiedInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	const q = `UPDATE users
	              SET email_verified = TRUE,
	                  email_verified_at = NOW(),
	                  updated_at = NOW()
	            WHERE id = $1`
	cmdTag, err := tx.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// RequestDeletionInTx flips active → pending. The
// `deletion_requested_at IS NULL` guard makes the write idempotent; the
// follow-up classify-read distinguishes ErrUserNotFound from
// ErrDeletionAlreadyPending.
func (r *userRepository) RequestDeletionInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	const q = `UPDATE users
	              SET deletion_requested_at = NOW(),
	                  deleted_at = NOW(),
	                  deletion_canceled_at = NULL,
	                  updated_at = NOW()
	            WHERE id = $1
	              AND deletion_requested_at IS NULL`
	cmdTag, err := tx.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("request deletion: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		var requestedAt *time.Time
		var deletedAt *time.Time
		err2 := r.pool.QueryRow(ctx, `SELECT deletion_requested_at, deleted_at FROM users WHERE id = $1`, userID).
			Scan(&requestedAt, &deletedAt)
		if err2 != nil {
			if errors.Is(err2, pgx.ErrNoRows) {
				return domain.ErrUserNotFound
			}
			return fmt.Errorf("classify deletion state: %w", err2)
		}
		if requestedAt != nil {
			return domain.ErrDeletionAlreadyPending
		}
		return domain.ErrUserNotFound
	}
	return nil
}

// CancelDeletion flips pending → restored via UPDATE..RETURNING gated by the
// 30-day grace boundary. Distinguishes ErrAlreadyPurged from
// ErrNoDeletionPending via a follow-up classify-read on zero matches.
func (r *userRepository) CancelDeletion(ctx context.Context, userID uuid.UUID, graceDays int) (bool, error) {
	sql := fmt.Sprintf(`UPDATE users
	                       SET deletion_canceled_at = NOW(),
	                           deleted_at = NULL,
	                           updated_at = NOW()
	                     WHERE id = $1
	                       AND deletion_requested_at IS NOT NULL
	                       AND deletion_canceled_at IS NULL
	                       AND deletion_requested_at > NOW() - INTERVAL '%d days'
	                     RETURNING id`, graceDays)
	var returnedID uuid.UUID
	err := r.pool.QueryRow(ctx, sql, userID).Scan(&returnedID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("cancel deletion: %w", err)
	}
	var requestedAt *time.Time
	var canceledAt *time.Time
	err2 := r.pool.QueryRow(ctx, `SELECT deletion_requested_at, deletion_canceled_at FROM users WHERE id = $1`, userID).
		Scan(&requestedAt, &canceledAt)
	if err2 != nil {
		if errors.Is(err2, pgx.ErrNoRows) {
			return false, domain.ErrAlreadyPurged
		}
		return false, fmt.Errorf("classify cancel state: %w", err2)
	}
	if requestedAt == nil {
		return false, domain.ErrNoDeletionPending
	}
	if canceledAt != nil {
		return true, nil
	}
	return false, domain.ErrAlreadyPurged
}

// EnumeratePendingDeletionsInTx claims a batch for the hard-delete sweeper
// using FOR UPDATE SKIP LOCKED so concurrent sweepers + the cancel endpoint
// don't deadlock or race-clobber. Oldest-first for deterministic progress.
func (r *userRepository) EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error) {
	const q = `SELECT id FROM users
	            WHERE deletion_requested_at IS NOT NULL
	              AND deletion_canceled_at IS NULL
	              AND deletion_requested_at < $1
	            ORDER BY deletion_requested_at ASC
	            FOR UPDATE SKIP LOCKED
	            LIMIT $2`
	rows, err := tx.Query(ctx, q, before, limit)
	if err != nil {
		return nil, fmt.Errorf("enumerate pending deletions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	})
}

// EnumerateUpcomingDeletions returns users whose deletion_requested_at falls
// inside (fromTime, toTime] — the T-7 warning window. Pool-based because the
// warning sweeper has no surrounding business transaction.
func (r *userRepository) EnumerateUpcomingDeletions(ctx context.Context, fromTime, toTime time.Time, limit int) ([]*domain.User, error) {
	const q = `SELECT id, email, name, password_hash, preferred_locale,
	                  COALESCE(email_verified, FALSE) AS email_verified,
	                  email_verified_at,
	                  deleted_at, deletion_requested_at, deletion_canceled_at,
	                  created_at, updated_at
	             FROM users
	            WHERE deletion_requested_at IS NOT NULL
	              AND deletion_canceled_at IS NULL
	              AND deletion_requested_at > $1
	              AND deletion_requested_at <= $2
	            ORDER BY deletion_requested_at ASC
	            LIMIT $3`
	rows, err := r.pool.Query(ctx, q, fromTime, toTime, limit)
	if err != nil {
		return nil, fmt.Errorf("enumerate upcoming deletions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.User, error) {
		u, err := scanUser(row)
		if err != nil {
			return nil, err
		}
		return &u, nil
	})
}

// HardDeleteInTx issues DELETE FROM users inside the caller-supplied tx.
// The caller must write the user_self_deleted audit row BEFORE the DELETE in
// the same tx so the FK SET NULL on audit_logs.user_id has somewhere to land.
func (r *userRepository) HardDeleteInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	cmdTag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("hard delete user: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// UpdatePreferredLocale sets users.preferred_locale + updated_at. Locale
// validation ('ru' | 'en') happens at the handler boundary; the DB CHECK
// is the defense-in-depth floor.
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

// UpdateName sets users.name + updated_at. Length validation ('2..100') happens
// at the handler boundary; the repo just persists the trimmed value.
func (r *userRepository) UpdateName(ctx context.Context, userID uuid.UUID, name string) error {
	sql, args, err := r.sb.
		Update("users").
		Set("name", name).
		Set("updated_at", time.Now()).
		Where(squirrel.Eq{"id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update name: %w", err)
	}

	cmdTag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update name: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}
