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

// GetByID delegates to the inner concrete repo. Phase 21-03: needed by
// EmailVerificationService.RequestResend + ChangeEmailBeforeVerify which
// must load the user state (email_verified + email) before mutating.
func (a *UserResetExtAdapter) GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return a.inner.GetByID(ctx, userID)
}

// GetByIDIncludingDeleted delegates to the inner concrete repo. Phase 21-04:
// AccountDeletionService.RequestDeletion calls this so it can detect a
// soft-deleted user and return ErrDeletionAlreadyPending instead of
// ErrUserNotFound on the retry path.
func (a *UserResetExtAdapter) GetByIDIncludingDeleted(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return a.inner.GetByIDIncludingDeleted(ctx, userID)
}

// Phase 21-04 delegates: account deletion lifecycle on the same adapter
// so AccountDeletionService consumes a single tx-aware user-repo seam.

func (a *UserResetExtAdapter) RequestDeletionInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return a.inner.RequestDeletionInTx(ctx, tx, userID)
}

func (a *UserResetExtAdapter) CancelDeletion(ctx context.Context, userID uuid.UUID, graceDays int) (bool, error) {
	return a.inner.CancelDeletion(ctx, userID, graceDays)
}

func (a *UserResetExtAdapter) EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error) {
	return a.inner.EnumeratePendingDeletionsInTx(ctx, tx, before, limit)
}

func (a *UserResetExtAdapter) EnumerateUpcomingDeletions(ctx context.Context, fromTime, toTime time.Time, limit int) ([]*domain.User, error) {
	return a.inner.EnumerateUpcomingDeletions(ctx, fromTime, toTime, limit)
}

func (a *UserResetExtAdapter) HardDeleteInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return a.inner.HardDeleteInTx(ctx, tx, userID)
}

// CreateInTx delegates to the inner concrete repo. Phase 21-03:
// UserService.Register uses this so the user row commits atomically with
// the user_consents + email_verification_tokens + email_outbox INSERTs.
func (a *UserResetExtAdapter) CreateInTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	return a.inner.CreateInTx(ctx, tx, user)
}

// UpdatePasswordHashInTx delegates to the inner concrete repo.
func (a *UserResetExtAdapter) UpdatePasswordHashInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bcryptHash []byte) error {
	return a.inner.UpdatePasswordHashInTx(ctx, tx, userID, bcryptHash)
}

// UpdateEmailInTx delegates to the inner concrete repo. Phase 21-03 D-21:
// PATCH /auth/email-before-verify mutates users.email inside the same tx
// as token invalidation + fresh-token issuance.
func (a *UserResetExtAdapter) UpdateEmailInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, newEmail string) error {
	return a.inner.UpdateEmailInTx(ctx, tx, userID, newEmail)
}

// MarkEmailVerifiedInTx delegates to the inner concrete repo. Phase 21-03
// D-22: POST /auth/verify-email/confirm flips email_verified + sets
// email_verified_at inside the same tx as token consume.
func (a *UserResetExtAdapter) MarkEmailVerifiedInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return a.inner.MarkEmailVerifiedInTx(ctx, tx, userID)
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

// CreateInTx inserts a user inside the caller-supplied tx. Phase 21-03:
// UserService.Register opens a tx so the user_consents + email_verification_tokens
// + email_outbox INSERTs commit atomically with the user row (no half-registered
// user with no verification email).
//
// Maps Postgres unique-violation on email to domain.ErrUserExists, same
// behavior as Create.
func (r *userRepository) CreateInTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
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
		return fmt.Errorf("build insert tx: %w", err)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrUserExists
		}
		// pgconn may not classify before pgx attempts the bind — fall back
		// to the string check (matches Create above).
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrUserExists
		}
		return fmt.Errorf("insert user tx: %w", err)
	}
	return nil
}

// GetByID — Phase 21-04 D-41: filters `deleted_at IS NULL` so a soft-deleted
// user (deletion requested, inside the 30-day grace window) looks like
// ErrUserNotFound to every read path. Deletion-aware code paths
// (AccountDeletionService, BlockWritesDuringGrace middleware, /auth/me)
// call GetByIDIncludingDeleted instead.
func (r *userRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	sql, args, err := r.sb.
		Select("id", "email", "password_hash", "preferred_locale",
			"COALESCE(email_verified, FALSE) AS email_verified",
			"email_verified_at",
			"deleted_at", "deletion_requested_at", "deletion_canceled_at",
			"created_at", "updated_at").
		From("users").
		Where(squirrel.Eq{"id": id}).
		Where("deleted_at IS NULL").
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
		&user.EmailVerified,
		&user.EmailVerifiedAt,
		&user.DeletedAt,
		&user.DeletionRequestedAt,
		&user.DeletionCanceledAt,
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

// GetByIDIncludingDeleted — Phase 21-04. Same SELECT as GetByID minus the
// `deleted_at IS NULL` filter. Used by handlers that need to read the
// accountDeletion state of a soft-deleted user (e.g. /auth/me must
// surface the grace banner; POST /users/me/restore must find the row
// to cancel).
func (r *userRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	sql, args, err := r.sb.
		Select("id", "email", "password_hash", "preferred_locale",
			"COALESCE(email_verified, FALSE) AS email_verified",
			"email_verified_at",
			"deleted_at", "deletion_requested_at", "deletion_canceled_at",
			"created_at", "updated_at").
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
		&user.EmailVerified,
		&user.EmailVerifiedAt,
		&user.DeletedAt,
		&user.DeletionRequestedAt,
		&user.DeletionCanceledAt,
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

// GetByEmail — Phase 21-04 D-41: filters `deleted_at IS NULL` so the same
// email can be re-registered post-purge without colliding with a
// soft-deleted account during the grace window (the legacy UNIQUE
// constraint on users.email still applies, so during the 30-day grace the
// email is genuinely unavailable — but at least no read confuses "soft-
// deleted" with "active").
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	sql, args, err := r.sb.
		Select("id", "email", "password_hash", "preferred_locale",
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

	var user domain.User
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&user.ID,
		&user.Email,
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

// UpdateEmailInTx sets users.email for the given userID inside the
// caller-supplied tx. Phase 21-03 D-21: PATCH /auth/email-before-verify
// runs this alongside InvalidateAllForUser + a fresh token issue + outbox
// enqueue, all in one tx.
//
// Maps pgconn UNIQUE-violation (sqlstate 23505 on users.email) to
// domain.ErrEmailTaken so the caller doesn't have to re-check after a race.
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
		// Postgres unique-violation maps to ErrEmailTaken so a race between
		// two concurrent ChangeEmailBeforeVerify calls (or against a fresh
		// Register) surfaces the friendly error code, not a raw pg error.
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

// MarkEmailVerifiedInTx flips users.email_verified=TRUE and stamps
// email_verified_at=NOW() for the given userID inside the caller-supplied
// tx. Phase 21-03 D-22: POST /auth/verify-email/confirm runs this in the
// same tx as the token consume so a partial state (token consumed but
// flag not flipped) cannot occur on a connection drop.
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

// RequestDeletionInTx — Phase 21-04. Sets deleted_at + deletion_requested_at
// + updated_at on the user row inside the caller-supplied tx. The
// `deletion_requested_at IS NULL` guard makes this idempotent: a second
// concurrent call surfaces ErrDeletionAlreadyPending so the handler can
// return 423 instead of double-scheduling.
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
		// Either user doesn't exist OR deletion_requested_at is already set.
		// Distinguish via a follow-up read so the service can map to the
		// right sentinel.
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

// CancelDeletion — Phase 21-04. Atomic UPDATE...RETURNING that clears
// deleted_at and stamps deletion_canceled_at iff the user is currently
// inside the 30-day grace window. Returns:
//
//	(true, nil)  — restored.
//	(false, ErrAlreadyPurged) — past 30d boundary OR row already gone.
//	(false, ErrNoDeletionPending) — row exists but had no pending deletion.
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
	// 0 rows matched — distinguish "no pending deletion" from "too late".
	var requestedAt *time.Time
	var canceledAt *time.Time
	err2 := r.pool.QueryRow(ctx, `SELECT deletion_requested_at, deletion_canceled_at FROM users WHERE id = $1`, userID).
		Scan(&requestedAt, &canceledAt)
	if err2 != nil {
		if errors.Is(err2, pgx.ErrNoRows) {
			// Row gone — must have been hard-deleted.
			return false, domain.ErrAlreadyPurged
		}
		return false, fmt.Errorf("classify cancel state: %w", err2)
	}
	if requestedAt == nil {
		return false, domain.ErrNoDeletionPending
	}
	// requestedAt is set AND we matched 0 rows → either canceledAt is already
	// set (idempotent re-cancel) OR past the grace boundary.
	if canceledAt != nil {
		// Already canceled — treat as success-noop.
		return true, nil
	}
	return false, domain.ErrAlreadyPurged
}

// EnumeratePendingDeletionsInTx — Phase 21-04 hard-delete sweeper helper.
// Claims a batch of soft-deleted users whose 30-day grace has elapsed via
// `FOR UPDATE SKIP LOCKED` so concurrent sweepers + the cancel endpoint
// don't deadlock or race-clobber. Returns up to `limit` IDs ordered oldest-
// first so the queue progresses deterministically.
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
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending deletion: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending deletions rows: %w", err)
	}
	return ids, nil
}

// EnumerateUpcomingDeletions — Phase 21-04 warning sweeper helper.
// Returns users whose deletion_requested_at falls between `fromTime`
// (exclusive) and `toTime` (inclusive) — the T-7 window the
// AccountDeletionService.WarningSweeper covers. Pool-based (no tx
// needed; the warning sweeper has no business transaction). Returns full
// user records so the caller can read the email + deletion_requested_at
// without a second round-trip.
func (r *userRepository) EnumerateUpcomingDeletions(ctx context.Context, fromTime, toTime time.Time, limit int) ([]*domain.User, error) {
	const q = `SELECT id, email, password_hash, preferred_locale,
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
	defer rows.Close()
	var out []*domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.PreferredLocale,
			&u.EmailVerified, &u.EmailVerifiedAt,
			&u.DeletedAt, &u.DeletionRequestedAt, &u.DeletionCanceledAt,
			&u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan upcoming deletion: %w", err)
		}
		out = append(out, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("upcoming deletions rows: %w", err)
	}
	return out, nil
}

// HardDeleteInTx — Phase 21-04 hard-delete sweeper. Issues `DELETE FROM
// users WHERE id = $1` inside the caller-supplied tx. The caller is
// responsible for writing the user_self_deleted audit row BEFORE the
// DELETE (in the same tx) so the FK SET NULL behavior (audit_logs.user_id
// from 21-03 ACCT-06) has somewhere to land. After DELETE, the audit
// row's FK becomes NULL but user_email_at_event preserves the email for
// 152-ФЗ forensic queries.
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
