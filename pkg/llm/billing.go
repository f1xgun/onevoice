package llm

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UsageLog records LLM usage for billing
type UsageLog struct {
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
	Model           string    `json:"model"`
	Provider        string    `json:"provider"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	ProviderCostUSD float64   `json:"provider_cost_usd"`
	CommissionUSD   float64   `json:"commission_usd"`
	UserCostUSD     float64   `json:"user_cost_usd"`
	UserTier        string    `json:"user_tier"`
	CreatedAt       time.Time `json:"created_at"`
}

// Default commission rates per tier (fraction of provider cost).
// These are MVP defaults — override via CommissionConfig at the call site once
// rates become contractual.
const (
	commissionPercentageDefault  = 0.20  // generic % markup when no tier match
	commissionFlatPerRequestUSD  = 0.001 // flat-mode fee per LLM request
	commissionTierRateFree       = 0.30
	commissionTierRateBasic      = 0.20
	commissionTierRatePro        = 0.10
	commissionTierRateEnterprise = 0.05
)

// CalculateCommission calculates commission based on mode and tier
func CalculateCommission(providerCost float64, mode, tier string) float64 {
	switch mode {
	case "percentage":
		return providerCost * commissionPercentageDefault

	case "flat":
		return commissionFlatPerRequestUSD

	case "tiered":
		rates := map[string]float64{
			"free":       commissionTierRateFree,
			"basic":      commissionTierRateBasic,
			"pro":        commissionTierRatePro,
			"enterprise": commissionTierRateEnterprise,
		}
		rate, ok := rates[tier]
		if !ok {
			rate = commissionPercentageDefault
		}
		return providerCost * rate

	default:
		return providerCost * commissionPercentageDefault
	}
}

// BillingRepository manages usage logging and billing queries
type BillingRepository interface {
	// LogUsage records an LLM usage event
	LogUsage(ctx context.Context, log *UsageLog) error

	// GetUserBalance returns the user's current balance in USD
	GetUserBalance(ctx context.Context, userID uuid.UUID) (float64, error)

	// GetDailySpend returns total spend for today
	GetDailySpend(ctx context.Context, userID uuid.UUID) (float64, error)

	// GetMonthlyUsage returns all usage logs for a given month
	GetMonthlyUsage(ctx context.Context, userID uuid.UUID, year, month int) ([]UsageLog, error)
}
