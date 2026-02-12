package llm_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/stretchr/testify/assert"
)

func TestTierLimits(t *testing.T) {
	limits := llm.TierLimits{
		"free": llm.Limits{
			RequestsPerMin:  10,
			TokensPerMin:    5000,
			TokensPerMonth:  100000,
			DailySpendUSD:   1.0,
		},
		"basic": llm.Limits{
			RequestsPerMin:  60,
			TokensPerMin:    50000,
			TokensPerMonth:  1000000,
			DailySpendUSD:   10.0,
		},
	}

	freeLimits := limits["free"]
	assert.Equal(t, 10, freeLimits.RequestsPerMin)
	assert.Equal(t, 5000, freeLimits.TokensPerMin)
	assert.Equal(t, 100000, freeLimits.TokensPerMonth)
	assert.Equal(t, 1.0, freeLimits.DailySpendUSD)

	basicLimits := limits["basic"]
	assert.Equal(t, 60, basicLimits.RequestsPerMin)
}

func TestLimits_IsUnlimited(t *testing.T) {
	unlimited := llm.Limits{
		RequestsPerMin:  -1,
		TokensPerMin:    -1,
		TokensPerMonth:  -1,
		DailySpendUSD:   -1,
	}

	assert.True(t, unlimited.IsUnlimited())

	limited := llm.Limits{
		RequestsPerMin: 10,
		TokensPerMin:   5000,
	}

	assert.False(t, limited.IsUnlimited())
}
