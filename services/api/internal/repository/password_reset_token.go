// Package repository — password_reset_token.go
//
// PasswordResetTokenRepository owns SQL for password_reset_tokens
// The cornerstone is ConsumeAtomic — a single
// statement that atomically marks a token as used and returns the
// owning user_id. Zero rows returned ⇒ ErrResetTokenInvalid regardless
// of whether the token was unknown, expired, or already consumed
// (PITFALLS §1.1: never distinguish failure modes to the caller).
//
// Token storage discipline:
// - Only SHA-256 hashes are persisted in token_hash (BYTEA UNIQUE).
// The plaintext token never touches the DB.
// - The atomic-consume statement's WHERE clause IS the comparison —
// pgx encodes the BYTEA argument and Postgres compares byte-by-byte
// server-side, removing the need for crypto/subtle in the lookup path.
//
// Caller controls tx lifecycle. Insert and InvalidateAllForUser run
// inside the caller-supplied tx so they commit atomically with the email
// outbox enqueue (guarantees no orphan emails when the tx
// rolls back). ConsumeAtomic also runs inside the caller's tx so the
// password update + refresh-token wipe in PasswordResetService commit
// alongside the consume.
package repository

import (
	"context"
	"errors"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// PasswordResetTokenRepository owns SQL for password_reset_tokens.
// Constructor takes the pgxPool interface (already in pool.go) so unit
// tests can pass pgxmock.PgxPoolIface — matches the EmailOutboxRepository
// pattern established in.
type PasswordResetTokenRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewPasswordResetTokenRepository constructs the password reset
// token repository. Returns the concrete pointer (matches
// EmailOutboxRepository) — the PasswordResetService consumes the methods
// directly with no need for a domain interface.
func NewPasswordResetTokenRepository(pool pgxPool) *PasswordResetTokenRepository {
	return &PasswordResetTokenRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Insert writes a new token row inside the caller-supplied tx. tokenHash
// must be the raw 32-byte SHA-256 digest (the service computes
// sha256.Sum256(plaintext)[:]).
//
// Returns ErrResetTokenCollision on the astronomically improbable UNIQUE
// constraint violation — at 256-bit entropy the odds of a duplicate are
// effectively zero, but the service may retry on this sentinel.
func (r *PasswordResetTokenRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	tokenHash []byte,
	expiresAt time.Time,
) error {
	sqlStr, args, err := r.psql.
		Insert("password_reset_tokens").
		Columns("user_id", "token_hash", "expires_at").
		Values(userID, tokenHash, expiresAt).
		ToSql()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlStr, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrResetTokenCollision
		}
		return err
	}
	return nil
}

// ConsumeAtomic is the canonical single-statement atomic consume. PITFALLS §1.3.
// Zero rows returned = either expired, already consumed, or never existed —
// all surface as ErrResetTokenInvalid (callers MUST NOT branch on which:
// PITFALLS §1.1).
//
// The UPDATE acquires a row-level lock on the matching row before the
// SET / RETURNING evaluation, so two concurrent calls on the same hash see
// exactly one winner — the loser's WHERE clause re-evaluates after the
// winner's COMMIT, where `consumed_at IS NULL` no longer matches.
func (r *PasswordResetTokenRepository) ConsumeAtomic(
	ctx context.Context,
	tx pgx.Tx,
	tokenHash []byte,
) (uuid.UUID, error) {
	const q = `
		UPDATE password_reset_tokens
		   SET consumed_at = NOW()
		 WHERE token_hash = $1
		   AND consumed_at IS NULL
		   AND expires_at > NOW()
		RETURNING user_id`
	var userID uuid.UUID
	if err := tx.QueryRow(ctx, q, tokenHash).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, domain.ErrResetTokenInvalid
		}
		return uuid.Nil, err
	}
	return userID, nil
}

// InvalidateAllForUser marks every unconsumed token for a user as consumed.
// Used by RequestReset to revoke older outstanding links before issuing a
// fresh one (FEATURES Flow 1 Edge case 5). Caller passes the tx; service
// composes this with the new Insert in the same transaction.
func (r *PasswordResetTokenRepository) InvalidateAllForUser(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	const q = `
		UPDATE password_reset_tokens
		   SET consumed_at = NOW()
		 WHERE user_id = $1
		   AND consumed_at IS NULL`
	_, err := tx.Exec(ctx, q, userID)
	return err
}
