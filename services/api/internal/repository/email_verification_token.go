// Package repository — email_verification_token.go
//
// EmailVerificationTokenRepository owns SQL for email_verification_tokens
// Mirrors PasswordResetTokenRepository's shape:
// single-statement atomic consume (PITFALLS §1.1 → ErrVerifyTokenInvalid
// for every failure mode), SHA-256 hash storage (BYTEA UNIQUE), no
// plaintext at rest.
//
// The `email` column captures the address the token was issued for so the
// historical row survives a user changing their pre-verification email
// (invalidates outstanding tokens but the snapshot stays).
//
// Caller controls tx lifecycle. Insert + InvalidateAllForUser run inside
// the caller-supplied tx so they commit atomically with the email outbox
// enqueue (guarantees no orphan emails when the tx rolls back).
// ConsumeAtomic also runs inside the caller's tx so the email_verified
// flag flip on users commits alongside the token consume.
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

// EmailVerificationTokenRepository owns SQL for email_verification_tokens.
// Constructor takes the pgxPool interface (pool.go) so unit tests can pass
// pgxmock.PgxPoolIface — matches the EmailOutboxRepository pattern.
type EmailVerificationTokenRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewEmailVerificationTokenRepository constructs the repo.
// Returns the concrete pointer (matches password_reset_token, email_outbox)
// — the EmailVerificationService consumes the methods directly with no
// need for a domain interface.
func NewEmailVerificationTokenRepository(pool pgxPool) *EmailVerificationTokenRepository {
	return &EmailVerificationTokenRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Insert persists a new verification token inside the caller-supplied tx.
// tokenHash MUST be the raw 32-byte SHA-256 digest (the service computes
// sha256.Sum256([]byte(plaintext))[:]).
//
// Returns a wrapped Postgres error on UNIQUE-violation — at 256-bit entropy
// the odds of a duplicate are effectively zero, so callers may bubble the
// error up rather than retry. The service does not currently retry.
func (r *EmailVerificationTokenRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	email string,
	tokenHash []byte,
	expiresAt time.Time,
) error {
	sqlStr, args, err := r.psql.
		Insert("email_verification_tokens").
		Columns("user_id", "email", "token_hash", "expires_at").
		Values(userID, email, tokenHash, expiresAt).
		ToSql()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlStr, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return err // hash collision — caller decides
		}
		return err
	}
	return nil
}

// ConsumeAtomic is the canonical single-statement atomic consume
// (PITFALLS §1.3). Zero rows returned = either expired, already consumed,
// or never existed — all surface as ErrVerifyTokenInvalid (callers MUST
// NOT branch on which: PITFALLS §1.1).
//
// The UPDATE acquires a row-level lock on the matching row before the
// SET / RETURNING evaluation, so two concurrent calls on the same hash see
// exactly one winner — the loser's WHERE clause re-evaluates after the
// winner's COMMIT, where `consumed_at IS NULL` no longer matches.
//
// Returns (userID, email, nil) on success — email is the address the token
// was issued for.
func (r *EmailVerificationTokenRepository) ConsumeAtomic(
	ctx context.Context,
	tx pgx.Tx,
	tokenHash []byte,
) (uuid.UUID, string, error) {
	const q = `
		UPDATE email_verification_tokens
		   SET consumed_at = NOW()
		 WHERE token_hash = $1
		   AND consumed_at IS NULL
		   AND expires_at > NOW()
		RETURNING user_id, email`
	var userID uuid.UUID
	var email string
	if err := tx.QueryRow(ctx, q, tokenHash).Scan(&userID, &email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", domain.ErrVerifyTokenInvalid
		}
		return uuid.Nil, "", err
	}
	return userID, email, nil
}

// LookupExpired returns (true, nil) if a row exists for tokenHash with
// expires_at <= NOW AND consumed_at IS NULL. Callers use it to distinguish
// "expired" from "unknown / consumed" purely for UX. This is a READ
// query, NOT another consume — the actual consume already failed with
// ErrVerifyTokenInvalid before we get here.
//
// Defense-in-depth: even if an attacker observed the verify_token_expired
// code, they still cannot recover the plaintext or replay the token —
// it was already consumed by the winner of the race, or never existed.
func (r *EmailVerificationTokenRepository) LookupExpired(
	ctx context.Context,
	tokenHash []byte,
) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1 FROM email_verification_tokens
			 WHERE token_hash = $1
			   AND consumed_at IS NULL
			   AND expires_at <= NOW()
		)`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, tokenHash).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// InvalidateAllForUser marks every unconsumed token for a user as consumed.
// Callers include ChangeEmailBeforeVerify (so an in-flight verification link
// cannot still verify the new address) and RequestResend (to revoke older
// outstanding links before issuing a fresh one).
func (r *EmailVerificationTokenRepository) InvalidateAllForUser(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
) error {
	const q = `
		UPDATE email_verification_tokens
		   SET consumed_at = NOW()
		 WHERE user_id = $1
		   AND consumed_at IS NULL`
	_, err := tx.Exec(ctx, q, userID)
	return err
}
