package llm_test

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUsageLog(t *testing.T) {
	userID := uuid.New()
	log := llm.UsageLog{
		ID:              uuid.New(),
		UserID:          userID,
		Model:           "claude-3.5-sonnet",
		Provider:        "openrouter",
		InputTokens:     100,
		OutputTokens:    200,
		ProviderCostUSD: 0.0015,
		CommissionUSD:   0.0003,
		UserCostUSD:     0.0018,
		UserTier:        "free",
		CreatedAt:       time.Now(),
	}

	assert.Equal(t, "claude-3.5-sonnet", log.Model)
	assert.Equal(t, "openrouter", log.Provider)
	assert.Equal(t, 100, log.InputTokens)
	assert.Equal(t, 200, log.OutputTokens)
	assert.Equal(t, 0.0018, log.UserCostUSD)
}

func TestCalculateCommission(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		providerCost  float64
		tier          string
		expectedComm  float64
	}{
		{
			name:         "percentage mode",
			mode:         "percentage",
			providerCost: 0.01,
			tier:         "free",
			expectedComm: 0.002, // 20% of 0.01
		},
		{
			name:         "flat mode",
			mode:         "flat",
			providerCost: 0.01,
			tier:         "free",
			expectedComm: 0.001, // Fixed $0.001
		},
		{
			name:         "tiered mode - free",
			mode:         "tiered",
			providerCost: 0.01,
			tier:         "free",
			expectedComm: 0.003, // 30% for free tier
		},
		{
			name:         "tiered mode - pro",
			mode:         "tiered",
			providerCost: 0.01,
			tier:         "pro",
			expectedComm: 0.001, // 10% for pro tier
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commission := llm.CalculateCommission(tt.providerCost, tt.mode, tt.tier)
			assert.InDelta(t, tt.expectedComm, commission, 0.0001)
		})
	}
}
