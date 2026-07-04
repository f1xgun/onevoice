// Package repository — billing.go
//
// billingRepository implements pkg/llm.BillingRepository for Postgres
// (production wiring). The orchestrator's Router consumes only the narrow
// Writer slice (LogUsage) — read methods exist here for services/api
// consumers (daily-spend rate limiter via GetDailySpend; v1.5 billing UI via
// GetUserBalance / GetMonthlyUsage).
//
// Live methods:
//   - LogUsage      — write path; rejects uuid.Nil BusinessID defense-in-depth
//     (the DB column is also NOT NULL).
//   - GetDailySpend — read path for the daily-spend rate limiter; filters
//     strictly by UTC calendar day so the limiter's "did this business
//     exceed today's cap" check is wall-clock deterministic.
//
// Stubbed (TODO v1.5): GetUserBalance + GetMonthlyUsage. Both return zero
// values + nil so callers compile against the interface today; the billing
// UI ships the implementations.

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
)

type billingRepository struct {
	pool   pgxPool
	sb     squirrel.StatementBuilderType
	ledger *creditLedgerRepository
}

// Compile-time assertion: PostgresBillingRepository must satisfy
// llm.BillingRepository (and by embedding, llm.Writer).
var _ llm.BillingRepository = (*billingRepository)(nil)

// NewBillingRepository returns the Postgres-backed BillingRepository. Both
// *pgxpool.Pool (production) and pgxmock.PgxPoolIface (tests) satisfy the
// package-local pgxPool interface defined in pool.go.
func NewBillingRepository(pool pgxPool) llm.BillingRepository {
	return &billingRepository{
		pool:   pool,
		sb:     newStatementBuilder(),
		ledger: newCreditLedgerRepository(pool),
	}
}

// LogUsage inserts one usage_logs row. The call site is the fire-and-forget
// goroutine in pkg/llm/router.go (logBilling); the Router skips it entirely
// when BusinessID is uuid.Nil, so reaching this method with a nil BusinessID
// indicates a wiring bug — we reject it instead of silently producing a row
// the DB CHECK / NOT NULL would also reject (defense in depth).
//
// ID is assigned a fresh UUID when uuid.Nil, mirroring how audit_log.Insert
// lets the DB DEFAULT fill the column — explicit ID assignment here lets
// tests assert the generated value via the captured INSERT args.
//
// user_id, conversation_id, and request_id are translated to SQL NULL when
// empty via nullableUUID / nullableString — system-level callers (titler,
// review_drafter, future cron paths) pass uuid.Nil / "" for those fields.
//
// Credit metering (v1.6): the usage_logs INSERT and the credit_ledger charge
// run in ONE transaction so a usage row and its credit consumption commit
// atomically — a metering failure rolls back the usage row too. The whole call
// is fire-and-forget from pkg/llm/router.go (logBilling discards the returned
// error), so a rolled-back tx never blocks the chat turn.
func (r *billingRepository) LogUsage(ctx context.Context, log *llm.UsageLog) error {
	if log == nil {
		return fmt.Errorf("LogUsage: log is required")
	}
	if log.BusinessID == uuid.Nil {
		return fmt.Errorf("LogUsage: business_id is required")
	}
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}

	sql, args, err := r.sb.
		Insert("usage_logs").
		Columns(
			"id", "business_id", "user_id", "conversation_id", "request_id",
			"model", "provider",
			"input_tokens", "output_tokens",
			"cache_read_tokens", "cache_creation_tokens",
			"provider_cost_usd", "commission_usd",
			"user_tier", "created_at",
		).
		Values(
			log.ID, log.BusinessID, nullableUUID(log.UserID),
			nullableString(log.ConversationID), nullableString(log.RequestID),
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("LogUsage: build sql: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("LogUsage: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("LogUsage: exec: %w", err)
	}
	if err = r.ledger.MeterUsage(ctx, tx, log.BusinessID, log.ID); err != nil {
		return fmt.Errorf("LogUsage: meter: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("LogUsage: commit: %w", err)
	}
	return nil
}

// GetDailySpend returns the sum of (provider_cost_usd + commission_usd) for
// the supplied business over the UTC calendar day containing `day`. The
// daily-spend rate-limit policy reads this as its gate.
//
// Day boundaries:
//   - We normalize the supplied `day` to its UTC year/month/day. A caller in
//     UTC+3 passing 2026-05-30T14:00:00+03:00 still gets the 2026-05-30 UTC
//     window (00:00:00Z → next 00:00:00Z). The composite index
//     idx_usage_logs_business_created_at services the range scan.
//   - COALESCE(SUM(...), 0) collapses the empty-result case to 0, nil so
//     callers do not need to special-case "no rows today".
//
// dayWindow is the half-open UTC interval queried by GetDailySpend.
// Extracted into a named constant so the linter does not flag `24 * time.Hour`
// as a magic number — the value is the literal definition of "one day".
const dayWindow = 24 * time.Hour

func (r *billingRepository) GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	dayUTC := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	next := dayUTC.Add(dayWindow)

	sql, args, err := r.sb.
		Select("COALESCE(SUM(provider_cost_usd + commission_usd), 0)").
		From("usage_logs").
		Where(squirrel.Eq{"business_id": businessID}).
		Where(squirrel.GtOrEq{"created_at": dayUTC}).
		Where(squirrel.Lt{"created_at": next}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("GetDailySpend: build sql: %w", err)
	}

	var sum float64
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&sum); err != nil {
		return 0, fmt.Errorf("GetDailySpend: exec: %w", err)
	}
	return sum, nil
}

// GetCreditBalance returns the business's current credit balance — the latest
// credit_ledger.balance_after, or 0 when the business has no ledger rows yet.
// Delegates to the credit_ledger repository so the balance-read SQL lives in one
// place.
func (r *billingRepository) GetCreditBalance(ctx context.Context, businessID uuid.UUID) (int, error) {
	return r.ledger.CurrentBalance(ctx, businessID)
}

// GetMonthlyUsageSummary aggregates a business's usage_logs over the UTC
// calendar month (year, month) into action count, total spend, and image count.
// The window is half-open [firstOfMonth, firstOfNextMonth) in UTC — same
// wall-clock-deterministic boundary style as GetDailySpend so a caller in any
// time zone lands on the UTC month it asked for. The composite index
// idx_usage_logs_business_created_at services the range scan.
func (r *billingRepository) GetMonthlyUsageSummary(ctx context.Context, businessID uuid.UUID, year, month int) (llm.MonthlyUsageSummary, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	next := start.AddDate(0, 1, 0)

	sql, args, err := r.sb.
		Select(
			"COUNT(*)",
			"COALESCE(SUM(provider_cost_usd + commission_usd), 0)",
		).
		Column("COUNT(*) FILTER (WHERE provider = ?)", llm.ImageProvider).
		From("usage_logs").
		Where(squirrel.Eq{"business_id": businessID}).
		Where(squirrel.GtOrEq{"created_at": start}).
		Where(squirrel.Lt{"created_at": next}).
		ToSql()
	if err != nil {
		return llm.MonthlyUsageSummary{}, fmt.Errorf("GetMonthlyUsageSummary: build sql: %w", err)
	}

	var summary llm.MonthlyUsageSummary
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&summary.Actions, &summary.SpendUSD, &summary.Images); err != nil {
		return llm.MonthlyUsageSummary{}, fmt.Errorf("GetMonthlyUsageSummary: exec: %w", err)
	}
	return summary, nil
}

// GetUserBalance — legacy user-keyed stub retained to preserve the
// llm.BillingRepository interface assertion. The billing summary uses the
// business-keyed GetCreditBalance instead; the daily-spend rate limiter never
// calls this. Returns 0, nil.
func (r *billingRepository) GetUserBalance(_ context.Context, _ uuid.UUID) (float64, error) {
	return 0, nil
}

// GetMonthlyUsage — legacy user-keyed stub retained to preserve the
// llm.BillingRepository interface assertion. The billing summary uses the
// business-keyed GetMonthlyUsageSummary instead. Returns nil, nil.
func (r *billingRepository) GetMonthlyUsage(_ context.Context, _ uuid.UUID, _, _ int) ([]llm.UsageLog, error) {
	return nil, nil
}
