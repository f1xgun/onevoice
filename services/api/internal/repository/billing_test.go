// Package repository — billing_test.go
//
// pgxmock unit tests for PostgresBillingRepository. Mirror the
// audit_log_test.go pattern (newAuditLogRepoMock) for setup; each test
// returns a fresh pgxmock pool wired to NewBillingRepository so
// expectations don't bleed across cases.
//
// LogUsage is transactional post-v1.6: it wraps the usage_logs INSERT and the
// credit-metering ledger write in one tx, so the happy-path tests queue
// ExpectBegin → usage_logs Exec → metering (advisory lock, balance read,
// credit_ledger insert) → ExpectCommit.

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// newBillingRepoMock returns the domain-typed BillingRepository plus the
// underlying pgxmock pool for argument-shape assertions.
func newBillingRepoMock(t *testing.T) (pgxmock.PgxPoolIface, llm.BillingRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewBillingRepository(mock)
}

// usageLogsColumnCount / creditLedgerColumnCount are the INSERT arities the
// metering expectations below match against via anyArgs (defined in
// telemetry_event_test.go). pgxmock treats a missing WithArgs as "expect zero
// arguments", so error-path expectations still need one matcher per column.
const (
	usageLogsColumnCount    = 15
	creditLedgerColumnCount = 9
)

// expectMetering queues the credit-metering sub-operations LogUsage runs inside
// its transaction after the usage_logs INSERT: per-business advisory lock,
// running-balance read (returning prevBalance), and the credit_ledger insert.
func expectMetering(mock pgxmock.PgxPoolIface, prevBalance int) {
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`SELECT balance_after FROM credit_ledger`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"balance_after"}).AddRow(prevBalance))
	mock.ExpectExec(`INSERT INTO credit_ledger`).
		WithArgs(anyArgs(creditLedgerColumnCount)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

// fullLog builds a populated UsageLog for the happy-path test. Fields not
// load-bearing for a given test can be overridden after construction.
func fullLog() *llm.UsageLog {
	return &llm.UsageLog{
		ID:                  uuid.New(),
		BusinessID:          uuid.New(),
		UserID:              uuid.New(),
		ConversationID:      "67f4a8b27a9ad15d4f8a1c00",
		RequestID:           "req-abc",
		Model:               "anthropic/claude-sonnet-4-6",
		Provider:            "anthropic",
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
		ProviderCostUSD:     0.0123,
		CommissionUSD:       0.00246,
		UserCostUSD:         0.01476,
		UserTier:            "basic",
		CreatedAt:           time.Now().UTC(),
	}
}

// Test 1 — Happy path: full log → transactional usage_logs INSERT + metering.
// All positional args on the usage_logs INSERT match struct fields in
// declaration order.
func TestBillingRepository_LogUsage_Success(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			log.ID, log.BusinessID, log.UserID,
			log.ConversationID, log.RequestID,
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectMetering(mock, 100)
	mock.ExpectCommit()

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 2 — Repository rejects uuid.Nil BusinessID at the application layer
// before opening a transaction. NO Begin/Exec called.
func TestBillingRepository_LogUsage_RejectsNilBusinessID(t *testing.T) {
	mock, repo := newBillingRepoMock(t)

	log := fullLog()
	log.BusinessID = uuid.Nil

	err := repo.LogUsage(context.Background(), log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "business_id is required")
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 3 — When the caller leaves UsageLog.ID == uuid.Nil, the repository
// assigns a fresh UUID before the INSERT (visible as the first positional arg).
func TestBillingRepository_LogUsage_AssignsIDWhenNil(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()
	log.ID = uuid.Nil

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			pgxmock.AnyArg(),
			log.BusinessID, log.UserID,
			log.ConversationID, log.RequestID,
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectMetering(mock, 100)
	mock.ExpectCommit()

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, log.ID,
		"LogUsage must mutate log.ID from uuid.Nil to a freshly generated UUID")
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 4 — UserID == uuid.Nil → translated to SQL NULL (nil interface).
func TestBillingRepository_LogUsage_NullableUserID(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()
	log.UserID = uuid.Nil

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			log.ID, log.BusinessID,
			nil,
			log.ConversationID, log.RequestID,
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectMetering(mock, 100)
	mock.ExpectCommit()

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 5 — Empty ConversationID and empty RequestID → SQL NULL via
// nullableString helper.
func TestBillingRepository_LogUsage_NullableConversationID(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()
	log.ConversationID = ""
	log.RequestID = ""

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			log.ID, log.BusinessID, log.UserID,
			nil,
			nil,
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectMetering(mock, 100)
	mock.ExpectCommit()

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 6 — GetDailySpend happy path. Mock returns 12.50; method returns
// 12.50, nil. Verifies the SELECT shape pins business_id + UTC range boundaries.
func TestBillingRepository_GetDailySpend_Success(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	businessID := uuid.New()
	day := time.Date(2026, 5, 30, 14, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(provider_cost_usd \+ commission_usd\), 0\) FROM usage_logs WHERE`).
		WithArgs(businessID.String(), dayStart, dayEnd).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(12.50))

	got, err := repo.GetDailySpend(context.Background(), businessID, day)
	require.NoError(t, err)
	require.InDelta(t, 12.50, got, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 7 — Empty result set: COALESCE collapses to 0 server-side.
func TestBillingRepository_GetDailySpend_ZeroWhenNoRows(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	businessID := uuid.New()
	day := time.Now().UTC()

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(provider_cost_usd \+ commission_usd\), 0\) FROM usage_logs WHERE`).
		WithArgs(businessID.String(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(0.0))

	got, err := repo.GetDailySpend(context.Background(), businessID, day)
	require.NoError(t, err)
	require.Equal(t, 0.0, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 8 — Range filter math. UTC+3 input must produce a UTC calendar-day window.
func TestBillingRepository_GetDailySpend_FiltersByUTCDay(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	businessID := uuid.New()

	loc, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	day := time.Date(2026, 5, 30, 14, 0, 0, 0, loc)

	expectedLower := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	expectedUpper := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(provider_cost_usd \+ commission_usd\), 0\) FROM usage_logs WHERE`).
		WithArgs(businessID.String(), expectedLower, expectedUpper).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(0.0))

	_, err = repo.GetDailySpend(context.Background(), businessID, day)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 9 — GetCreditBalance delegates to the credit_ledger balance read.
func TestBillingRepository_GetCreditBalance_LatestBalance(t *testing.T) {
	mock, repo := newBillingRepoMock(t)

	mock.ExpectQuery(`SELECT COALESCE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(1999))

	got, err := repo.GetCreditBalance(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, 1999, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 10 — GetMonthlyUsageSummary aggregates over the UTC month window and
// counts image-provider rows via FILTER. The image-provider FILTER placeholder
// binds first ($1), then the WHERE args.
func TestBillingRepository_GetMonthlyUsageSummary_Success(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	businessID := uuid.New()
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	next := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`COUNT\(\*\) FILTER \(WHERE provider = \$1\) FROM usage_logs WHERE`).
		WithArgs(llm.ImageProvider, businessID.String(), start, next).
		WillReturnRows(pgxmock.NewRows([]string{"count", "sum", "images"}).AddRow(12, 3.75, 2))

	got, err := repo.GetMonthlyUsageSummary(context.Background(), businessID, 2026, 7)
	require.NoError(t, err)
	require.Equal(t, 12, got.Actions)
	require.InDelta(t, 3.75, got.SpendUSD, 1e-9)
	require.Equal(t, 2, got.Images)
	require.NoError(t, mock.ExpectationsWereMet())
}

// GetUserBalance is a legacy user-keyed stub retained for the interface
// assertion; it returns (0, nil).
func TestBillingRepository_GetUserBalance_StubReturnsZero(t *testing.T) {
	_, repo := newBillingRepoMock(t)

	balance, err := repo.GetUserBalance(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, 0.0, balance)
}

// GetMonthlyUsage is a legacy user-keyed stub; it returns (nil, nil).
func TestBillingRepository_GetMonthlyUsage_StubReturnsEmpty(t *testing.T) {
	_, repo := newBillingRepoMock(t)

	rows, err := repo.GetMonthlyUsage(context.Background(), uuid.New(), 2026, 5)
	require.NoError(t, err)
	require.Nil(t, rows)
}

// --- DB error paths ---

// When the usage_logs INSERT fails, the transaction rolls back and LogUsage
// wraps with a clear prefix.
func TestBillingRepository_LogUsage_DBError(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(anyArgs(usageLogsColumnCount)...).
		WillReturnError(errors.New("pg connection lost"))
	mock.ExpectRollback()

	err := repo.LogUsage(context.Background(), log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LogUsage: exec")
	require.NoError(t, mock.ExpectationsWereMet())
}

// When metering fails (after a successful usage_logs INSERT), the whole tx
// rolls back — usage row and credit charge are atomic.
func TestBillingRepository_LogUsage_MeteringErrorRollsBack(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(anyArgs(usageLogsColumnCount)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("lock timeout"))
	mock.ExpectRollback()

	err := repo.LogUsage(context.Background(), log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LogUsage: meter")
	require.NoError(t, mock.ExpectationsWereMet())
}

// Nil log → application-level guard before any DB work.
func TestBillingRepository_LogUsage_NilLog(t *testing.T) {
	_, repo := newBillingRepoMock(t)

	err := repo.LogUsage(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "log is required")
}
