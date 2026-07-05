package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

type fakeSummaryResolver struct{ plan planresolver.Plan }

func (f fakeSummaryResolver) Resolve(_ context.Context, _ uuid.UUID) planresolver.Plan {
	return f.plan
}

type fakeSummaryBilling struct {
	balance    int
	balanceErr error
	summary    llm.MonthlyUsageSummary
	daily      float64
}

func (f fakeSummaryBilling) GetCreditBalance(_ context.Context, _ uuid.UUID) (int, error) {
	return f.balance, f.balanceErr
}

func (f fakeSummaryBilling) GetMonthlyUsageSummary(_ context.Context, _ uuid.UUID, _, _ int) (llm.MonthlyUsageSummary, error) {
	return f.summary, nil
}

func (f fakeSummaryBilling) GetDailySpend(_ context.Context, _ uuid.UUID, _ time.Time) (float64, error) {
	return f.daily, nil
}

func TestBillingSummaryService_AssemblesAllBlocks(t *testing.T) {
	svc := NewBillingSummaryService(
		fakeSummaryResolver{plan: planresolver.Plan{Code: "pro", Name: "Pro", MonthlyCredits: 2000, DailyLLMUSDCap: 50}},
		fakeSummaryBilling{balance: 1500, summary: llm.MonthlyUsageSummary{Actions: 12, SpendUSD: 3.75, Images: 2}, daily: 4.2},
	)

	got, err := svc.Summary(context.Background(), uuid.New())
	require.NoError(t, err)

	require.Equal(t, "pro", got.Plan.Code)
	require.Equal(t, "Pro", got.Plan.Name)
	require.Equal(t, 2000, got.Plan.MonthlyCredits)

	require.Equal(t, 2000, got.Credits.Granted)
	require.Equal(t, 1500, got.Credits.Remaining)
	require.Equal(t, 500, got.Credits.Used)
	require.Equal(t, 0, got.Credits.Overage)

	require.Equal(t, 12, got.UsageThisMonth.Actions)
	require.InDelta(t, 3.75, got.UsageThisMonth.SpendUSD, 1e-9)
	require.Equal(t, 2, got.UsageThisMonth.Images)

	require.InDelta(t, 4.2, got.DailySpend.TodayUSD, 1e-9)
	require.InDelta(t, 50.0, got.DailySpend.CapUSD, 1e-9)
}

// A balance greater than the grant clamps Used to 0 (never negative).
func TestBillingSummaryService_UsedClampedAtZero(t *testing.T) {
	svc := NewBillingSummaryService(
		fakeSummaryResolver{plan: planresolver.Plan{Code: "free", MonthlyCredits: 100}},
		fakeSummaryBilling{balance: 150},
	)
	got, err := svc.Summary(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, 0, got.Credits.Used, "Used must never go negative")
	require.Equal(t, 150, got.Credits.Remaining)
}

// After the monthly grant lands (credit_ledger balance = monthly_credits), a
// freshly granted business with no consumption reports remaining = the full
// allowance and used = 0 — the visible fix for the "remaining=0 for everyone"
// bug the grant job closes.
func TestBillingSummaryService_GrantedBusinessShowsFullRemaining(t *testing.T) {
	svc := NewBillingSummaryService(
		fakeSummaryResolver{plan: planresolver.Plan{Code: "free", Name: "Free", MonthlyCredits: 100}},
		fakeSummaryBilling{balance: 100},
	)
	got, err := svc.Summary(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, 100, got.Credits.Granted)
	require.Equal(t, 100, got.Credits.Remaining, "a just-granted business must not read remaining=0")
	require.Equal(t, 0, got.Credits.Used)
}

func TestBillingSummaryService_PropagatesBalanceError(t *testing.T) {
	svc := NewBillingSummaryService(
		fakeSummaryResolver{plan: planresolver.Plan{Code: "free"}},
		fakeSummaryBilling{balanceErr: errors.New("db down")},
	)
	_, err := svc.Summary(context.Background(), uuid.New())
	require.Error(t, err)
	require.Contains(t, err.Error(), "credit balance")
}
