// Package repository — billing_test.go
//
// pgxmock unit tests for PostgresBillingRepository. Mirror the
// audit_log_test.go pattern (newAuditLogRepoMock) for setup; each test
// returns a fresh pgxmock pool wired to NewBillingRepository so
// expectations don't bleed across cases.

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

// Test 1 — Happy path: full log → single Exec with the 15-column INSERT
// pattern; all positional args match struct fields in declaration order.
func TestBillingRepository_LogUsage_Success(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()

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

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 2 — Repository rejects uuid.Nil BusinessID at the application layer
// even though the DB NOT NULL constraint is the second wall. NO Exec called.
func TestBillingRepository_LogUsage_RejectsNilBusinessID(t *testing.T) {
	mock, repo := newBillingRepoMock(t)

	log := fullLog()
	log.BusinessID = uuid.Nil

	err := repo.LogUsage(context.Background(), log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "business_id is required")
	// No Exec was expected — ExpectationsWereMet verifies zero queries fired.
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 3 — When the caller leaves UsageLog.ID == uuid.Nil, the repository
// assigns a fresh UUID before the INSERT (visible as the first positional
// arg in the captured Exec). pgxmock.AnyArg() is the appropriate matcher
// for the generated UUID because the exact bytes are unpredictable; the
// caller-visible side effect (log.ID mutated to non-Nil) is the load-bearing
// assertion.
func TestBillingRepository_LogUsage_AssignsIDWhenNil(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()
	log.ID = uuid.Nil

	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			pgxmock.AnyArg(), // generated ID — caller asserts non-Nil below
			log.BusinessID, log.UserID,
			log.ConversationID, log.RequestID,
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, log.ID,
		"LogUsage must mutate log.ID from uuid.Nil to a freshly generated UUID")
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 4 — UserID == uuid.Nil → translated to SQL NULL (nil interface).
// Captures the args via a custom check: arg index 2 must be untyped nil.
func TestBillingRepository_LogUsage_NullableUserID(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()
	log.UserID = uuid.Nil

	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			log.ID, log.BusinessID,
			nil, // user_id → NULL
			log.ConversationID, log.RequestID,
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 5 — Empty ConversationID and empty RequestID → SQL NULL via
// nullableString helper. Verifies the args at indices 3 + 4.
func TestBillingRepository_LogUsage_NullableConversationID(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()
	log.ConversationID = ""
	log.RequestID = ""

	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(
			log.ID, log.BusinessID, log.UserID,
			nil, // conversation_id → NULL
			nil, // request_id      → NULL
			log.Model, log.Provider,
			log.InputTokens, log.OutputTokens,
			log.CacheReadTokens, log.CacheCreationTokens,
			log.ProviderCostUSD, log.CommissionUSD,
			log.UserTier, log.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.LogUsage(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 6 — GetDailySpend happy path. Mock returns 12.50; method returns
// 12.50, nil. Verifies the SELECT shape pins business_id + UTC range
// boundaries.
func TestBillingRepository_GetDailySpend_Success(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	businessID := uuid.New()
	day := time.Date(2026, 5, 30, 14, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	// pgx encodes uuid.UUID via its driver.Valuer Value() method as the
	// canonical 36-char hyphenated string, so pgxmock sees the arg as a
	// string. Asserting on businessID.String() pins the value while still
	// matching the wire shape pgx actually emits.
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(provider_cost_usd \+ commission_usd\), 0\) FROM usage_logs WHERE`).
		WithArgs(businessID.String(), dayStart, dayEnd).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(12.50))

	got, err := repo.GetDailySpend(context.Background(), businessID, day)
	require.NoError(t, err)
	require.InDelta(t, 12.50, got, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test 7 — Empty result set: COALESCE collapses to 0 server-side; method
// returns 0, nil. Verifies the caller does not need a special-case for
// "no rows today".
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

// Test 8 — Range filter math. UTC+3 input 2026-05-30T14:00:00+03:00 must
// produce a query range [2026-05-30T00:00:00Z, 2026-05-31T00:00:00Z). The
// repository normalizes to the UTC calendar day from the caller's Local
// year/month/day — non-UTC zones round to the supplied wall-clock day, NOT
// the UTC-shifted day. This matches the user-observable invariant: "today"
// in Russia means 2026-05-30 local, not 2026-05-29.
func TestBillingRepository_GetDailySpend_FiltersByUTCDay(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	businessID := uuid.New()

	loc, err := time.LoadLocation("Europe/Moscow") // UTC+3
	require.NoError(t, err)
	day := time.Date(2026, 5, 30, 14, 0, 0, 0, loc)

	// Repository builds bounds from day.Year()/Month()/Day() — Moscow's
	// 2026-05-30 14:00 has Year=2026, Month=5, Day=30 (the local fields are
	// what time.Date(... time.UTC) reads). The window is the UTC calendar
	// day with those fields.
	expectedLower := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	expectedUpper := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(provider_cost_usd \+ commission_usd\), 0\) FROM usage_logs WHERE`).
		WithArgs(businessID.String(), expectedLower, expectedUpper).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(0.0))

	_, err = repo.GetDailySpend(context.Background(), businessID, day)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// GetUserBalance is stubbed to (0, nil) today so the interface compiles for
// downstream consumers; v1.5 billing UI ships the real impl.
func TestBillingRepository_GetUserBalance_StubReturnsZero(t *testing.T) {
	_, repo := newBillingRepoMock(t)

	balance, err := repo.GetUserBalance(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, 0.0, balance)
}

// GetMonthlyUsage is stubbed to (nil, nil) today. v1.5 billing UI ships the
// real aggregation.
func TestBillingRepository_GetMonthlyUsage_StubReturnsEmpty(t *testing.T) {
	_, repo := newBillingRepoMock(t)

	rows, err := repo.GetMonthlyUsage(context.Background(), uuid.New(), 2026, 5)
	require.NoError(t, err)
	require.Nil(t, rows)
}

// --- DB error paths ---

// Defense in depth — when the underlying Exec fails, LogUsage wraps with a
// clear prefix so the goroutine's discarded error is debuggable from logs.
func TestBillingRepository_LogUsage_DBError(t *testing.T) {
	mock, repo := newBillingRepoMock(t)
	log := fullLog()

	mock.ExpectExec(`INSERT INTO usage_logs`).
		WillReturnError(errors.New("pg connection lost"))

	err := repo.LogUsage(context.Background(), log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LogUsage: exec")
}

// Nil log → application-level guard before any DB work.
func TestBillingRepository_LogUsage_NilLog(t *testing.T) {
	_, repo := newBillingRepoMock(t)

	err := repo.LogUsage(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "log is required")
}
