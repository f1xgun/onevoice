package llm

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UsageLog records LLM usage for billing.
//
// New (Phase 25a) fields:
//   - BusinessID: required for non-system callers — Router skips logging when nil.
//   - ConversationID: Mongo ObjectID hex; "" for system-level callers (titler,
//     review_drafter); column is TEXT NULL in usage_logs.
//   - RequestID: correlation/trace identifier copied from ChatRequest.RequestID.
//   - CacheReadTokens / CacheCreationTokens: populated from TokenUsage so the
//     cache-aware cost math (read=0.1×, write=1.25×) is reconstructable from the
//     row even if InputCostPer1MTok changes later.
type UsageLog struct {
	ID                  uuid.UUID `json:"id"`
	BusinessID          uuid.UUID `json:"business_id"`
	UserID              uuid.UUID `json:"user_id"`
	ConversationID      string    `json:"conversation_id,omitempty"`
	RequestID           string    `json:"request_id,omitempty"`
	Model               string    `json:"model"`
	Provider            string    `json:"provider"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CacheReadTokens     int       `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int       `json:"cache_creation_tokens,omitempty"`
	ProviderCostUSD     float64   `json:"provider_cost_usd"`
	CommissionUSD       float64   `json:"commission_usd"`
	UserCostUSD         float64   `json:"user_cost_usd"`
	UserTier            string    `json:"user_tier"`
	CreatedAt           time.Time `json:"created_at"`
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

// Writer is the orchestrator-facing slice of BillingRepository. The Router's
// fire-and-forget call site in router.go logBilling consumes only this single
// method, so production wiring can satisfy it with a thin HTTP adapter (Plan
// 25a-03 billingclient) without dragging the read methods into the
// orchestrator's dependency surface.
type Writer interface {
	LogUsage(ctx context.Context, log *UsageLog) error
}

// BillingRepository manages usage logging and billing queries. Embeds Writer
// so any BillingRepository satisfies the orchestrator-facing Writer interface.
type BillingRepository interface {
	Writer

	// GetUserBalance returns the user's current balance in USD. v1.5 — stubbed
	// in PostgresBillingRepository until the billing UI lands.
	GetUserBalance(ctx context.Context, userID uuid.UUID) (float64, error)

	// GetDailySpend returns the SUM(provider_cost_usd + commission_usd) for the
	// supplied business on the UTC calendar day containing `day`. Phase 25b's
	// rate limiter consumes this as the daily spend gate.
	GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error)

	// GetMonthlyUsage returns all usage logs for a given month. v1.5 — stubbed
	// in PostgresBillingRepository until the billing UI lands.
	GetMonthlyUsage(ctx context.Context, userID uuid.UUID, year, month int) ([]UsageLog, error)
}
