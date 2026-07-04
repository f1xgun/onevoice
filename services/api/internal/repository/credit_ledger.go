// Package repository — credit_ledger.go
//
// creditLedgerRepository implements domain.CreditLedgerRepository over the
// append-only credit_ledger table. It also exposes MeterUsage, the tx-scoped
// "charge one credit for one usage_logs row" step that billingRepository.LogUsage
// runs inside its own transaction so the usage row and its credit consumption
// commit atomically.

package repository

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/creditmeter"
)

// creditsPerUsage is how many credits one chargeable usage_logs row consumes.
// 1 usage row (LLM turn or image generation) = 1 credit. A future "N credits
// per model" policy would compute this from the usage_logs.model at meter time;
// the schema already supports an arbitrary delta.
const creditsPerUsage = 1

type creditLedgerRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time assertion.
var _ domain.CreditLedgerRepository = (*creditLedgerRepository)(nil)

// newCreditLedgerRepository returns the concrete repo (exposes MeterUsage).
func newCreditLedgerRepository(pool pgxPool) *creditLedgerRepository {
	return &creditLedgerRepository{pool: pool, sb: newStatementBuilder()}
}

// NewCreditLedgerRepository returns the Postgres-backed CreditLedgerRepository.
func NewCreditLedgerRepository(pool pgxPool) domain.CreditLedgerRepository {
	return newCreditLedgerRepository(pool)
}

// currentBalanceSQL reads the running balance as the balance_after of the
// most-recent ledger row, collapsing "no rows yet" to 0 via COALESCE.
const currentBalanceSQL = `SELECT COALESCE(` +
	`(SELECT balance_after FROM credit_ledger WHERE business_id = $1 ORDER BY created_at DESC LIMIT 1), 0)`

// CurrentBalance returns the latest balance_after for a business, or 0 when the
// business has no ledger rows yet.
func (r *creditLedgerRepository) CurrentBalance(ctx context.Context, businessID uuid.UUID) (int, error) {
	var balance int
	if err := r.pool.QueryRow(ctx, currentBalanceSQL, businessID).Scan(&balance); err != nil {
		return 0, fmt.Errorf("CurrentBalance: exec: %w", err)
	}
	return balance, nil
}

// Append inserts one ledger row inside the caller's transaction. Idempotent on
// entry.IdempotencyKey: when the key is already present the partial unique index
// uq_credit_ledger_idem makes the insert a no-op via ON CONFLICT DO NOTHING.
func (r *creditLedgerRepository) Append(ctx context.Context, tx pgx.Tx, entry *domain.CreditLedgerEntry) error {
	if entry == nil {
		return fmt.Errorf("Append: entry is required")
	}
	if entry.BusinessID == uuid.Nil {
		return fmt.Errorf("Append: business_id is required")
	}
	id := entry.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	sql, args, err := r.sb.
		Insert("credit_ledger").
		Columns(
			"id", "business_id", "delta_credits", "balance_after",
			"overage_credits", "reason", "usage_log_id",
			"subscription_period", "idempotency_key", "created_at",
		).
		Values(
			id, entry.BusinessID, entry.DeltaCredits, entry.BalanceAfter,
			entry.OverageCredits, entry.Reason, entry.UsageLogID,
			entry.SubscriptionPeriod, entry.IdempotencyKey, squirrel.Expr("now()"),
		).
		Suffix("ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING").
		ToSql()
	if err != nil {
		return fmt.Errorf("Append: build sql: %w", err)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("Append: exec: %w", err)
	}
	return nil
}

// MeterUsage charges one credit for a single usage_logs row inside the caller's
// transaction. It serializes concurrent meters for the same business with a
// per-business transaction-scoped advisory lock (so the empty-ledger read is not
// racy), reads the running balance, splits the charge into consume/overage via
// creditmeter.Compute, and appends the row. idempotency_key is the usage-log id,
// so a retried billing POST re-meters to exactly one ledger row.
func (r *creditLedgerRepository) MeterUsage(ctx context.Context, tx pgx.Tx, businessID, usageLogID uuid.UUID) error {
	if businessID == uuid.Nil {
		return fmt.Errorf("MeterUsage: business_id is required")
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey(businessID)); err != nil {
		return fmt.Errorf("MeterUsage: advisory lock: %w", err)
	}

	var prevBalance int
	err := tx.QueryRow(ctx,
		"SELECT balance_after FROM credit_ledger WHERE business_id = $1 ORDER BY created_at DESC LIMIT 1",
		businessID,
	).Scan(&prevBalance)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("MeterUsage: read balance: %w", err)
		}
		prevBalance = 0
	}

	res := creditmeter.Compute(prevBalance, creditsPerUsage)
	logID := usageLogID
	idemKey := usageLogID.String()
	entry := &domain.CreditLedgerEntry{
		BusinessID:     businessID,
		DeltaCredits:   res.Delta,
		BalanceAfter:   res.BalanceAfter,
		OverageCredits: res.OverageCredits,
		Reason:         res.Reason,
		UsageLogID:     &logID,
		IdempotencyKey: &idemKey,
	}
	return r.Append(ctx, tx, entry)
}

// advisoryLockKey derives a deterministic int8 lock key from a business UUID so
// pg_advisory_xact_lock can serialize per-business metering. The uint64→int64
// reinterpretation is intentional: the value is only a lock identifier, so
// sign/overflow carry no meaning (collisions are astronomically unlikely and
// would only over-serialize two businesses, never corrupt data).
//
//nolint:gosec // G115: reinterpret-cast to a lock key; overflow is irrelevant.
func advisoryLockKey(id uuid.UUID) int64 {
	return int64(binary.BigEndian.Uint64(id[:8]))
}
