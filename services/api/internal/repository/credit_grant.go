// Package repository — credit_grant.go
//
// CreditGrantExtAdapter is the concrete monthly-credit-grant surface the
// background grant worker (wire.StartCreditGrant) consumes. Like
// BusinessDeletionExtAdapter, it is exposed as a concrete pointer rather than
// through a domain interface: the grant worker needs table-spanning operations
// (enumerate every active business + append grant/expire ledger rows in one tx)
// that do not belong on any single-aggregate domain repository. The append-only
// balance convention itself stays in credit_ledger.go — this adapter only owns
// enumeration and transaction management.

package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreditGrantExtAdapter grants monthly credits over the businesses + credit_ledger tables.
type CreditGrantExtAdapter struct {
	pool   pgxPool
	ledger *creditLedgerRepository
}

// NewCreditGrantExtAdapter constructs the adapter sharing the pool with the
// other Postgres repositories.
func NewCreditGrantExtAdapter(pool pgxPool) *CreditGrantExtAdapter {
	return &CreditGrantExtAdapter{pool: pool, ledger: newCreditLedgerRepository(pool)}
}

// EnumerateActiveBusinessIDs returns the id of every non-soft-deleted business.
// The grant worker resolves each one's plan and grants its monthly allowance,
// which is why enumeration is decoupled from the (Track-A-sparse) subscriptions
// table: a business with no subscription row is still granted its Free
// allowance, so no business is missed.
func (a *CreditGrantExtAdapter) EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := a.pool.Query(ctx, `SELECT id FROM businesses WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("EnumerateActiveBusinessIDs: query: %w", err)
	}
	ids, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	})
	if err != nil {
		return nil, fmt.Errorf("EnumerateActiveBusinessIDs: collect: %w", err)
	}
	return ids, nil
}

// GrantMonthly grants one business its monthly allowance for period, opening and
// committing its own transaction (so a single business's failure never rolls
// back another's). It returns true when a grant row was inserted (false when the
// period was already granted or monthlyCredits <= 0). The ledger row logic
// (advisory lock, reset-expire, grant) lives on the credit-ledger repository.
func (a *CreditGrantExtAdapter) GrantMonthly(ctx context.Context, businessID uuid.UUID, monthlyCredits int, period string) (bool, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("GrantMonthly: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	granted, err := a.ledger.GrantMonthly(ctx, tx, businessID, monthlyCredits, period)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("GrantMonthly: commit: %w", err)
	}
	return granted, nil
}
