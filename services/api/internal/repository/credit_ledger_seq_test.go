// Package repository — credit_ledger_seq_test.go
//
// Real-Postgres fail-on-revert guard for the balance-derivation ORDER BY. The
// balance is the balance_after of the most-recent ledger row, and "most recent"
// MUST be the monotonic seq, not created_at. created_at defaults to now(), which
// is transaction-START time in Postgres, so a meter whose tx started earlier but
// committed later (after acquiring the per-business advisory lock) lands a row
// with an EARLIER created_at yet is the true latest. Ordering by created_at
// would return that stale row and corrupt the derived balance once balances are
// non-zero. This test reproduces the created_at inversion and asserts both
// balance queries (CurrentBalance's currentBalanceSQL and MeterUsage's inline
// read) return the seq-latest row.

package repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// creditLedgerSeqPool opens a pgxpool against TEST_POSTGRES_URL, creating an
// isolated per-test schema that owns a self-contained credit_ledger table. The
// table mirrors the migration (crucially, `seq BIGINT GENERATED ALWAYS AS
// IDENTITY` plus the (business_id, seq DESC) and partial idempotency indexes)
// but drops the businesses/usage_logs foreign keys so the schema does not depend
// on the shared DB's migration state. The pool's search_path is pinned to the
// test schema so the repository's unqualified `credit_ledger` references resolve
// there; the schema is dropped on cleanup.
func creditLedgerSeqPool(t *testing.T) (*pgxpool.Pool, *creditLedgerRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping credit_ledger seq ordering test")
	}

	ctx := context.Background()
	schema := "credit_ledger_seq_" + uuid.NewString()[:8]

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		pool.Close()
	})

	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s.credit_ledger (
		id                  uuid PRIMARY KEY,
		seq                 bigint GENERATED ALWAYS AS IDENTITY,
		business_id         uuid NOT NULL,
		delta_credits       integer NOT NULL,
		balance_after       integer NOT NULL CHECK (balance_after >= 0),
		overage_credits     integer NOT NULL DEFAULT 0 CHECK (overage_credits >= 0),
		reason              text NOT NULL
		                        CHECK (reason IN ('grant', 'consume', 'overage', 'refund', 'expire')),
		usage_log_id        uuid NULL,
		subscription_period text NULL,
		idempotency_key     text NULL,
		created_at          timestamptz NOT NULL DEFAULT now()
	)`, schema))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf(
		`CREATE INDEX ON %s.credit_ledger(business_id, seq DESC)`, schema))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, fmt.Sprintf(
		`CREATE UNIQUE INDEX ON %s.credit_ledger(idempotency_key) WHERE idempotency_key IS NOT NULL`, schema))
	require.NoError(t, err)

	return pool, newCreditLedgerRepository(pool)
}

// TestCreditLedger_BalanceOrdersBySeqNotCreatedAt is the fail-on-revert guard
// for the tx-start-time skew. It seeds two same-business rows where the row with
// the LATER seq carries the EARLIER created_at, then asserts both balance reads
// return the seq-latest row's balance_after. If either query reverts to
// `ORDER BY created_at DESC` it returns the stale created_at-latest row instead
// and this test fails.
func TestCreditLedger_BalanceOrdersBySeqNotCreatedAt(t *testing.T) {
	ctx := context.Background()
	pool, repo := creditLedgerSeqPool(t)
	bizID := uuid.New()

	// seq is assigned in INSERT order, so the first row gets seq=1 and the second
	// seq=2. We give seq=1 the LATER wall-clock created_at and seq=2 the EARLIER
	// one — exactly the inversion a tx that started earlier but committed later
	// produces (created_at = transaction-START time). The seq-latest row (seq=2,
	// balance 50) is the TRUE latest; the created_at-latest row (seq=1, balance
	// 100) is stale.
	seed := func(balanceAfter int, createdAt string) {
		_, err := pool.Exec(ctx,
			`INSERT INTO credit_ledger (id, business_id, delta_credits, balance_after, reason, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), bizID, balanceAfter, balanceAfter, domain.CreditReasonGrant, createdAt)
		require.NoError(t, err)
	}
	seed(100, "2026-01-01T12:00:00Z") // seq 1: later created_at, STALE balance
	seed(50, "2026-01-01T11:00:00Z")  // seq 2: earlier created_at, TRUE latest balance

	// currentBalanceSQL path.
	got, err := repo.CurrentBalance(ctx, bizID)
	require.NoError(t, err)
	require.Equalf(t, 50, got,
		"CurrentBalance must return the seq-latest row's balance_after (50); got %d means the query still orders by created_at DESC and returned the stale row",
		got)

	// MeterUsage inline-read path: a fresh charge must consume from the seq-latest
	// balance (50 → 49), not the created_at-latest balance (100 → 99). The new
	// row is both seq-latest and created_at-latest, so the follow-up CurrentBalance
	// reads it regardless of ordering — the discriminator is which prev balance
	// MeterUsage read.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.MeterUsage(ctx, tx, bizID, uuid.New()))
	require.NoError(t, tx.Commit(ctx))

	after, err := repo.CurrentBalance(ctx, bizID)
	require.NoError(t, err)
	require.Equalf(t, 49, after,
		"MeterUsage must charge against the seq-latest balance (50-1=49); got %d means its inline read still orders by created_at DESC (100-1=99)",
		after)
}
