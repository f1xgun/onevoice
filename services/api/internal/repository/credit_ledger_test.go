// Package repository — credit_ledger_test.go
//
// pgxmock unit tests for creditLedgerRepository: balance read, tx-scoped append
// (idempotency clause), and the MeterUsage charge (consume / overage / retry).

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

func newCreditLedgerMock(t *testing.T) (pgxmock.PgxPoolIface, *creditLedgerRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, newCreditLedgerRepository(mock)
}

func TestCreditLedger_CurrentBalance_LatestRow(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)
	bizID := uuid.New()

	mock.ExpectQuery(`SELECT COALESCE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(42))

	got, err := repo.CurrentBalance(context.Background(), bizID)
	require.NoError(t, err)
	require.Equal(t, 42, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreditLedger_CurrentBalance_EmptyIsZero(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)

	// COALESCE collapses the no-rows subquery to 0 server-side.
	mock.ExpectQuery(`SELECT COALESCE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(0))

	got, err := repo.CurrentBalance(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, 0, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreditLedger_Append_CarriesIdempotencyClause(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)
	bizID := uuid.New()
	key := uuid.NewString()

	mock.ExpectBegin()
	// The load-bearing assertion: the INSERT carries the partial-index
	// ON CONFLICT DO NOTHING clause that makes retries a no-op.
	mock.ExpectExec(`INSERT INTO credit_ledger .* ON CONFLICT \(idempotency_key\) WHERE idempotency_key IS NOT NULL DO NOTHING`).
		WithArgs(anyArgs(9)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	err = repo.Append(context.Background(), tx, &domain.CreditLedgerEntry{
		BusinessID:     bizID,
		DeltaCredits:   -1,
		BalanceAfter:   4,
		Reason:         domain.CreditReasonConsume,
		IdempotencyKey: &key,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// expectMeter sets the advisory-lock + balance-read + insert expectations for
// one MeterUsage call, returning after ExpectCommit is queued.
func expectMeterConsume(mock pgxmock.PgxPoolIface, prevBalance, wantDelta, wantBalance, wantOverage int, wantReason string) {
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"balance_after"}).AddRow(prevBalance))
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(
			pgxmock.AnyArg(), // id
			pgxmock.AnyArg(), // business_id
			wantDelta,
			wantBalance,
			wantOverage,
			wantReason,
			pgxmock.AnyArg(), // usage_log_id
			pgxmock.AnyArg(), // subscription_period
			pgxmock.AnyArg(), // idempotency_key
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestMeterUsage_NormalConsume(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)

	mock.ExpectBegin()
	expectMeterConsume(mock, 5, -1, 4, 0, domain.CreditReasonConsume)
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	require.NoError(t, repo.MeterUsage(context.Background(), tx, uuid.New(), uuid.New()))
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeterUsage_OverageWhenBalanceEmpty(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// No ledger rows yet → ErrNoRows → prevBalance 0 → overage.
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(),
			0, // delta: nothing drawn
			0, // balance_after: never negative
			1, // overage_credits
			domain.CreditReasonOverage,
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	require.NoError(t, repo.MeterUsage(context.Background(), tx, uuid.New(), uuid.New()))
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// A retried metering write (same usage_log_id) hits the unique idempotency_key
// and inserts 0 rows via ON CONFLICT DO NOTHING — MeterUsage must NOT error, so
// exactly one ledger row survives across retries.
func TestMeterUsage_IdempotentRetryIsNoError(t *testing.T) {
	mock, repo := newCreditLedgerMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"balance_after"}).AddRow(5))
	// Conflict → 0 rows affected.
	mock.ExpectExec(`INSERT INTO credit_ledger .* ON CONFLICT`).
		WithArgs(anyArgs(9)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))
	mock.ExpectCommit()

	tx, err := mock.Begin(context.Background())
	require.NoError(t, err)
	require.NoError(t, repo.MeterUsage(context.Background(), tx, uuid.New(), uuid.New()),
		"an ON CONFLICT no-op must not surface as an error")
	require.NoError(t, tx.Commit(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}
