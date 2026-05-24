// Package repository — user_consents.go
//
// UserConsentsRepository owns SQL for user_consents (Phase 21 / D-40).
// This is a STUB for Phase 22 to extend — Phase 21 writes exactly one
// row per Register with purpose='service_operation' policy_version='pre-v22'.
// Phase 22 will add Query / List methods + proper semver policy_version +
// non-null policy_sha256 + cross-border consent rows. The TABLE shape is
// already final; only new ROWS get added (not new columns).
//
// Caller controls tx lifecycle — Insert runs inside the same tx as the
// user-create in UserService.Register so a half-registered user (consent
// row missing) is impossible.
package repository

import (
	"context"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserConsentsRepository owns SQL for user_consents.
type UserConsentsRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewUserConsentsRepository constructs the Phase 21 user_consents repo.
// Returns the concrete pointer (matches password_reset_token,
// email_verification_token).
func NewUserConsentsRepository(pool pgxPool) *UserConsentsRepository {
	return &UserConsentsRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Insert records consent for a (user, purpose) pair inside the
// caller-supplied tx. The UNIQUE index on (user_id, purpose) protects
// against duplicates — Phase 21 calls this exactly once per Register so
// the conflict path is unreachable today; Phase 22 may add ON CONFLICT
// DO NOTHING semantics when consent collection moves to a dedicated UI.
//
// policyVersion is "pre-v22" for all Phase 21 inserts (D-40). Phase 22
// will pass real semver versions and populate policy_sha256.
func (r *UserConsentsRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	purpose, policyVersion string,
) error {
	sqlStr, args, err := r.psql.
		Insert("user_consents").
		Columns("user_id", "purpose", "policy_version").
		Values(userID, purpose, policyVersion).
		ToSql()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, sqlStr, args...)
	return err
}
