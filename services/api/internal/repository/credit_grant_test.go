// Package repository — credit_grant_test.go
//
// pgxmock unit tests for the monthly credit grant: the ledger-level GrantMonthly
// (reset-expire + grant, idempotency short-circuit, balance_after correctness)
// and the CreditGrantExtAdapter (active-business enumeration + tx wrapper).

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

const testGrantPeriod = "2026-07"

// expectGrantIdempotencyHit queues the lock + EXISTS(true) short-circuit.
func expectGrantIdempotencyHit(mock pgxmock.PgxPoolIface) {
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
}

// TestGrantMonthly_FreshBusiness_GrantsFullAllowance: no prior ledger rows →
// prevBalance 0 → no expire row, one grant row whose delta and balance_after
// both equal monthlyCredits.
func TestGrantMonthly_FreshBusiness_GrantsFullAllowance(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)
	const monthly = 100

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(
			pgxmock.AnyArg(), // id
			pgxmock.AnyArg(), // business_id
			monthly,          // delta_credits
			monthly,          // balance_after
			0,                // overage_credits
			domain.CreditReasonGrant,
			pgxmock.AnyArg(), // usage_log_id (nil)
			pgxmock.AnyArg(), // subscription_period
			pgxmock.AnyArg(), // idempotency_key
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	granted, err := repo.GrantMonthly(context.Background(), tx, uuid.New(), monthly, testGrantPeriod)
	require.NoError(t, err)
	require.True(t, granted)
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGrantMonthly_ResetsLeftover: a leftover balance from the prior period is
// zeroed with an expire row (delta -leftover, balance_after 0) BEFORE the grant
// lands the fresh allowance (balance_after = monthlyCredits), so the period
// opens at exactly the allowance and the ledger stays self-consistent.
func TestGrantMonthly_ResetsLeftover(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)
	const (
		monthly  = 100
		leftover = 40
	)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"balance_after"}).AddRow(leftover))
	// Expire the leftover first.
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(),
			-leftover, // delta_credits
			0,         // balance_after
			0,         // overage_credits
			domain.CreditReasonExpire,
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// Then grant the full allowance.
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(),
			monthly, // delta_credits
			monthly, // balance_after
			0,       // overage_credits
			domain.CreditReasonGrant,
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	granted, err := repo.GrantMonthly(context.Background(), tx, uuid.New(), monthly, testGrantPeriod)
	require.NoError(t, err)
	require.True(t, granted)
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGrantMonthly_IdempotentSecondPass: the (business, period) grant already
// exists → EXISTS short-circuits before any balance read or insert, and the
// method reports granted=false so the caller does not double-count.
func TestGrantMonthly_IdempotentSecondPass(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)

	mock.ExpectBegin()
	expectGrantIdempotencyHit(mock)
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	granted, err := repo.GrantMonthly(context.Background(), tx, uuid.New(), 100, testGrantPeriod)
	require.NoError(t, err)
	require.False(t, granted, "an already-granted period must not re-grant")
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGrantMonthly_NonPositiveIsNoOp: unlimited/none plans (monthly_credits <= 0)
// short-circuit with no DB work.
func TestGrantMonthly_NonPositiveIsNoOp(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)

	mock.ExpectBegin()
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	granted, err := repo.GrantMonthly(context.Background(), tx, uuid.New(), 0, testGrantPeriod)
	require.NoError(t, err)
	require.False(t, granted)
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditGrantExtAdapter_EnumerateActiveBusinessIDs filters soft-deleted rows
// at the query level and returns the active ids.
func TestCreditGrantExtAdapter_EnumerateActiveBusinessIDs(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	adapter := NewCreditGrantExtAdapter(mock)

	id1, id2 := uuid.New(), uuid.New()
	mock.ExpectQuery(`SELECT id FROM businesses WHERE deleted_at IS NULL`).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(id1).AddRow(id2))

	ids, err := adapter.EnumerateActiveBusinessIDs(context.Background())
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{id1, id2}, ids)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreditGrantExtAdapter_GrantMonthly_CommitsFreshGrant exercises the adapter
// tx wrapper end-to-end for a fresh business.
func TestCreditGrantExtAdapter_GrantMonthly_CommitsFreshGrant(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	adapter := NewCreditGrantExtAdapter(mock)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(anyArgs(9)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	granted, err := adapter.GrantMonthly(context.Background(), uuid.New(), 100, testGrantPeriod)
	require.NoError(t, err)
	require.True(t, granted)
	require.NoError(t, mock.ExpectationsWereMet())
}
