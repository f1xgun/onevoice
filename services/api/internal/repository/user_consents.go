// Package repository — user_consents.go
//
// introduced the table (000013) with columns (id, user_id, purpose,
// policy_version, policy_sha256, accepted_at) + unique index (user_id, purpose).
// (000016) adds (withdrawn_at, ip, user_agent) — forensic columns for
// 152-ФЗ Art. 21 (proof of withdrawal) and (forensic record at consent moment).
//
// All mutating methods take a caller-controlled pgx.Tx so consent writes
// stay inside the Register / ReConsent / Withdraw transaction.
package repository

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserConsentsRepository owns SQL for user_consents.
type UserConsentsRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewUserConsentsRepository constructs the user_consents repo.
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
// against duplicates. calls this exactly once per Register; the
// RecordRegistrationConsents path uses UpsertConsent instead.
//
// Kept for backward compatibility with the legacy single-INSERT
// Register fallback path (tests / pre-Phase-21 deploys).
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

// UpsertConsentInput carries the fields needed to record / re-record a
// single (user, purpose) consent row. IP / UserAgent may be empty when
// the caller is a background worker (e.g. policy version bump emits no
// per-user UPSERT, only an audit row).
type UpsertConsentInput struct {
	UserID        uuid.UUID
	Purpose       string // legalconfig.PolicySlug as string.
	PolicyVersion string
	PolicySHA256  string
	IP            string // may be empty.
	UserAgent     string
}

// UpsertConsent records or re-records a consent row inside the caller-
// supplied tx. ON CONFLICT (user_id, purpose) DO UPDATE bumps
// policy_version, policy_sha256, accepted_at, ip, user_agent and clears
// withdrawn_at. The row id is preserved across re-consent — forensic
// invariant per (re-consent does NOT create a new row; withdrawal
// does NOT delete the row).
//
// NULLIF + ::INET handle the case where the caller passes IP="" (e.g.
// a system path with no HTTP request context).
func (r *UserConsentsRepository) UpsertConsent(ctx context.Context, tx pgx.Tx, in UpsertConsentInput) error {
	const q = `
		INSERT INTO user_consents (user_id, purpose, policy_version, policy_sha256, accepted_at, ip, user_agent, withdrawn_at)
		VALUES ($1, $2, $3, $4, NOW(), NULLIF($5, '')::INET, NULLIF($6, ''), NULL)
		ON CONFLICT (user_id, purpose) DO UPDATE
		   SET policy_version = EXCLUDED.policy_version,
		       policy_sha256  = EXCLUDED.policy_sha256,
		       accepted_at    = NOW(),
		       ip             = EXCLUDED.ip,
		       user_agent     = EXCLUDED.user_agent,
		       withdrawn_at   = NULL`
	if _, err := tx.Exec(ctx, q, in.UserID, in.Purpose, in.PolicyVersion, in.PolicySHA256, in.IP, in.UserAgent); err != nil {
		return fmt.Errorf("user_consents upsert: %w", err)
	}
	return nil
}

// Consent is the read-side projection returned by ListByUser.
type Consent struct {
	UserID        uuid.UUID
	Purpose       string
	PolicyVersion string
	PolicySHA256  string
	AcceptedAt    time.Time
	WithdrawnAt   *time.Time
	IP            string
	UserAgent     string
}

// ListByUser returns every consent row for the user, sorted by purpose
// (deterministic order for the frontend). Pool-based (no tx) — read-only.
//
// Returns an empty slice (not nil) when the user has no rows.
func (r *UserConsentsRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]Consent, error) {
	const q = `SELECT user_id,
	                  purpose,
	                  policy_version,
	                  COALESCE(policy_sha256, ''),
	                  accepted_at,
	                  withdrawn_at,
	                  COALESCE(host(ip), ''),
	                  COALESCE(user_agent, '')
	             FROM user_consents
	            WHERE user_id = $1
	         ORDER BY purpose`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("user_consents list: %w", err)
	}
	defer rows.Close()
	out := make([]Consent, 0, 3)
	for rows.Next() {
		var c Consent
		if err := rows.Scan(&c.UserID, &c.Purpose, &c.PolicyVersion, &c.PolicySHA256, &c.AcceptedAt, &c.WithdrawnAt, &c.IP, &c.UserAgent); err != nil {
			return nil, fmt.Errorf("user_consents scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user_consents rows: %w", err)
	}
	return out, nil
}

// MarkWithdrawn sets withdrawn_at=NOW on the (user, purpose) row if
// it isn't already withdrawn. The IS NULL guard makes the operation
// idempotent — a duplicate withdrawal call is a no-op (zero rows
// updated, no error). Per / 152-ФЗ Art. 21 forensic invariant, the
// row is NEVER deleted.
func (r *UserConsentsRepository) MarkWithdrawn(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose string) error {
	const q = `UPDATE user_consents
	              SET withdrawn_at = NOW()
	            WHERE user_id = $1
	              AND purpose = $2
	              AND withdrawn_at IS NULL`
	if _, err := tx.Exec(ctx, q, userID, purpose); err != nil {
		return fmt.Errorf("user_consents mark_withdrawn: %w", err)
	}
	return nil
}
