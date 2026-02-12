package llm

// Limits defines rate limits for a subscription tier
type Limits struct {
	RequestsPerMin int     `json:"requests_per_min"` // Max requests per minute (-1 = unlimited)
	TokensPerMin   int     `json:"tokens_per_min"`   // Max tokens per minute (-1 = unlimited)
	TokensPerMonth int     `json:"tokens_per_month"` // Max tokens per month (-1 = unlimited)
	DailySpendUSD  float64 `json:"daily_spend_usd"`  // Max daily spend in USD (-1 = unlimited)
}

// IsUnlimited returns true if all limits are unlimited
func (l Limits) IsUnlimited() bool {
	return l.RequestsPerMin == -1 &&
		l.TokensPerMin == -1 &&
		l.TokensPerMonth == -1 &&
		l.DailySpendUSD == -1
}

// TierLimits maps subscription tier to limits
type TierLimits map[string]Limits

// DefaultTierLimits defines standard subscription tiers
var DefaultTierLimits = TierLimits{
	"free": {
		RequestsPerMin:  10,
		TokensPerMin:    5000,
		TokensPerMonth:  100000,
		DailySpendUSD:   1.0,
	},
	"basic": {
		RequestsPerMin:  60,
		TokensPerMin:    50000,
		TokensPerMonth:  1000000,
		DailySpendUSD:   10.0,
	},
	"pro": {
		RequestsPerMin:  120,
		TokensPerMin:    100000,
		TokensPerMonth:  -1, // unlimited
		DailySpendUSD:   50.0,
	},
	"enterprise": {
		RequestsPerMin:  -1, // unlimited
		TokensPerMin:    -1,
		TokensPerMonth:  -1,
		DailySpendUSD:   -1,
	},
}
