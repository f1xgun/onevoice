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

// CalculateCommission calculates commission based on mode and tier
func CalculateCommission(providerCost float64, mode string, tier string) float64 {
	switch mode {
	case "percentage":
		return providerCost * 0.20 // 20% default

	case "flat":
		return 0.001 // $0.001 per request

	case "tiered":
		rates := map[string]float64{
			"free":       0.30, // 30%
			"basic":      0.20, // 20%
			"pro":        0.10, // 10%
			"enterprise": 0.05, // 5%
		}
		rate, ok := rates[tier]
		if !ok {
			rate = 0.20 // Default to 20%
		}
		return providerCost * rate

	default:
		return providerCost * 0.20 // Default 20%
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
	GetMonthlyUsage(ctx context.Context, userID uuid.UUID, year int, month int) ([]UsageLog, error)
}
