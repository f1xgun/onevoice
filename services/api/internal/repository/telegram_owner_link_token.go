// Package repository — telegram_owner_link_token.go
//
// TelegramOwnerLinkTokenRepository owns SQL for telegram_owner_link_tokens, the
// single-use short-TTL tokens that back the /start deep-link owner-id handshake.
//
// The cornerstone is ConsumeAtomic — a single statement that atomically marks a
// token consumed and returns the owning business_id. Zero rows returned ⇒
// ErrLinkTokenInvalid regardless of whether the token was unknown, expired, or
// already consumed (never distinguish the failure mode to the caller — no
// enumeration).
//
// Token storage discipline:
//   - Only SHA-256 hashes are persisted in token_hash (BYTEA UNIQUE). The
//     plaintext token never touches the DB, so a store leak cannot replay a link.
//   - The atomic-consume statement's WHERE clause IS the comparison — pgx encodes
//     the BYTEA argument and Postgres compares byte-by-byte server-side, so the
//     lookup needs no constant-time compare of its own.
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

// TelegramOwnerLinkTokenRepository owns SQL for telegram_owner_link_tokens.
// Constructor takes the pgxPool interface (pool.go) so unit tests can pass
// pgxmock.PgxPoolIface. The mint and consume operations are self-contained
// single statements, so no caller-supplied tx is needed.
type TelegramOwnerLinkTokenRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewTelegramOwnerLinkTokenRepository constructs the repository.
func NewTelegramOwnerLinkTokenRepository(pool pgxPool) *TelegramOwnerLinkTokenRepository {
	return &TelegramOwnerLinkTokenRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Insert writes a new token row bound to businessID. tokenHash must be the raw
// 32-byte SHA-256 digest of the crypto-random plaintext. business_id is
// COLUMN-bound here at mint time, so a token can only ever bind an owner for the
// business it was minted for (a leaked token cannot name a different business).
//
// Returns ErrLinkTokenCollision on the astronomically improbable UNIQUE
// constraint violation so the mint path may retry with a fresh token.
func (r *TelegramOwnerLinkTokenRepository) Insert(
	ctx context.Context,
	businessID uuid.UUID,
	tokenHash []byte,
	expiresAt time.Time,
) error {
	sqlStr, args, err := r.psql.
		Insert("telegram_owner_link_tokens").
		Columns("business_id", "token_hash", "expires_at").
		Values(businessID, tokenHash, expiresAt).
		ToSql()
	if err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, sqlStr, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrLinkTokenCollision
		}
		return err
	}
	return nil
}

// ConsumeAtomic is the canonical single-statement atomic consume. Zero rows
// returned = either expired, already consumed, or never existed — all surface as
// ErrLinkTokenInvalid (callers MUST NOT branch on which).
//
// The UPDATE acquires a row-level lock on the matching row before the
// SET / RETURNING evaluation, so two concurrent /start taps on the same hash see
// exactly one winner — the loser's WHERE clause re-evaluates after the winner's
// COMMIT, where `consumed_at IS NULL` no longer matches, and it returns
// ErrLinkTokenInvalid (single-use).
func (r *TelegramOwnerLinkTokenRepository) ConsumeAtomic(
	ctx context.Context,
	tokenHash []byte,
) (uuid.UUID, error) {
	const q = `
		UPDATE telegram_owner_link_tokens
		   SET consumed_at = NOW()
		 WHERE token_hash = $1
		   AND consumed_at IS NULL
		   AND expires_at > NOW()
		RETURNING business_id`
	var businessID uuid.UUID
	if err := r.pool.QueryRow(ctx, q, tokenHash).Scan(&businessID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, domain.ErrLinkTokenInvalid
		}
		return uuid.Nil, err
	}
	return businessID, nil
}

// InvalidateAllForBusiness marks every unconsumed token for a business as
// consumed. The mint path uses it to revoke older outstanding links before
// issuing a fresh one, so at most one live link exists per business at a time.
func (r *TelegramOwnerLinkTokenRepository) InvalidateAllForBusiness(
	ctx context.Context,
	businessID uuid.UUID,
) error {
	const q = `
		UPDATE telegram_owner_link_tokens
		   SET consumed_at = NOW()
		 WHERE business_id = $1
		   AND consumed_at IS NULL`
	_, err := r.pool.Exec(ctx, q, businessID)
	return err
}
