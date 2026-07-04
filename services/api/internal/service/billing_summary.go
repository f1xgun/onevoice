package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// PlanResolver is the narrow plan-resolution seam the billing summary needs.
// *planresolver.Resolver satisfies it.
type PlanResolver interface {
	Resolve(ctx context.Context, businessID uuid.UUID) planresolver.Plan
}

// BillingReader is the narrow read surface the billing summary needs from the
// billing repository.
type BillingReader interface {
	GetCreditBalance(ctx context.Context, businessID uuid.UUID) (int, error)
	GetMonthlyUsageSummary(ctx context.Context, businessID uuid.UUID, year, month int) (llm.MonthlyUsageSummary, error)
	GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error)
}

// BillingSummary is the read-only usage-transparency payload for
// GET /businesses/{id}/billing/summary. No payment fields — Track-A surfaces
// plan + credits + usage only.
type BillingSummary struct {
	Plan struct {
		Code           string `json:"code"`
		Name           string `json:"name"`
		MonthlyCredits int    `json:"monthly_credits"`
	} `json:"plan"`
	Credits struct {
		Granted   int `json:"granted"`
		Used      int `json:"used"`
		Remaining int `json:"remaining"`
		Overage   int `json:"overage"`
	} `json:"credits"`
	UsageThisMonth struct {
		Actions  int     `json:"actions"`
		SpendUSD float64 `json:"spend_usd"`
		Images   int     `json:"images"`
	} `json:"usage_this_month"`
	DailySpend struct {
		TodayUSD float64 `json:"today_usd"`
		CapUSD   float64 `json:"cap_usd"`
	} `json:"daily_spend"`
}

// BillingSummaryService assembles the billing summary from the plan resolver
// and the billing repository.
type BillingSummaryService struct {
	resolver PlanResolver
	billing  BillingReader
	now      func() time.Time
}

// NewBillingSummaryService constructs the service.
func NewBillingSummaryService(resolver PlanResolver, billing BillingReader) *BillingSummaryService {
	return &BillingSummaryService{resolver: resolver, billing: billing, now: time.Now}
}

// Summary builds the billing summary for a business.
//
// Track-A note on the credits block: without the Track-B grant/expire job the
// credit_ledger carries no `grant` rows, so GetCreditBalance reads 0 and
// Used = Granted-Remaining is not yet a meaningful "consumed vs allocation"
// figure. The truthful Track-A activity signal is UsageThisMonth (sourced from
// usage_logs). Overage is reported as 0 until grant rows exist. The credits
// block becomes fully meaningful once the grant job lands.
func (s *BillingSummaryService) Summary(ctx context.Context, businessID uuid.UUID) (BillingSummary, error) {
	plan := s.resolver.Resolve(ctx, businessID)

	remaining, err := s.billing.GetCreditBalance(ctx, businessID)
	if err != nil {
		return BillingSummary{}, fmt.Errorf("billing summary: credit balance: %w", err)
	}

	now := s.now().UTC()
	usage, err := s.billing.GetMonthlyUsageSummary(ctx, businessID, now.Year(), int(now.Month()))
	if err != nil {
		return BillingSummary{}, fmt.Errorf("billing summary: monthly usage: %w", err)
	}

	todaySpend, err := s.billing.GetDailySpend(ctx, businessID, now)
	if err != nil {
		return BillingSummary{}, fmt.Errorf("billing summary: daily spend: %w", err)
	}

	var out BillingSummary
	out.Plan.Code = plan.Code
	out.Plan.Name = plan.Name
	out.Plan.MonthlyCredits = plan.MonthlyCredits

	out.Credits.Granted = plan.MonthlyCredits
	out.Credits.Remaining = remaining
	out.Credits.Used = max(0, plan.MonthlyCredits-remaining)
	out.Credits.Overage = 0

	out.UsageThisMonth.Actions = usage.Actions
	out.UsageThisMonth.SpendUSD = usage.SpendUSD
	out.UsageThisMonth.Images = usage.Images

	out.DailySpend.TodayUSD = todaySpend
	out.DailySpend.CapUSD = plan.DailyLLMUSDCap

	return out, nil
}
