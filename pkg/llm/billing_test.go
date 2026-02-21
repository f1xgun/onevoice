package llm_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/llm"
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
		name         string
		mode         string
		providerCost float64
		tier         string
		expectedComm float64
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

func TestBillingRepository_Interface(t *testing.T) {
	// Verify interface is defined
	var _ llm.BillingRepository = (*MockBillingRepository)(nil)
}

// MockBillingRepository implements BillingRepository for testing
type MockBillingRepository struct {
	mu   sync.Mutex
	logs []llm.UsageLog
}

func (m *MockBillingRepository) LogUsage(_ context.Context, log *llm.UsageLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, *log)
	return nil
}

func (m *MockBillingRepository) GetUserBalance(_ context.Context, _ uuid.UUID) (float64, error) {
	return 10.0, nil // Mock balance
}

func (m *MockBillingRepository) GetDailySpend(_ context.Context, userID uuid.UUID) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0.0
	for _, log := range m.logs {
		if log.UserID == userID {
			total += log.UserCostUSD
		}
	}
	return total, nil
}

func (m *MockBillingRepository) GetMonthlyUsage(_ context.Context, userID uuid.UUID, year, month int) ([]llm.UsageLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []llm.UsageLog
	for _, log := range m.logs {
		if log.UserID == userID && log.CreatedAt.Year() == year && int(log.CreatedAt.Month()) == month {
			result = append(result, log)
		}
	}
	return result, nil
}

func TestMockBillingRepository(t *testing.T) {
	repo := &MockBillingRepository{}
	ctx := context.Background()
	userID := uuid.New()

	// Log usage
	log := &llm.UsageLog{
		ID:          uuid.New(),
		UserID:      userID,
		Model:       "gpt-4",
		Provider:    "openai",
		UserCostUSD: 0.05,
		CreatedAt:   time.Now(),
	}
	err := repo.LogUsage(ctx, log)
	assert.NoError(t, err)

	// Get daily spend
	spend, err := repo.GetDailySpend(ctx, userID)
	assert.NoError(t, err)
	assert.Equal(t, 0.05, spend)

	// Get balance
	balance, err := repo.GetUserBalance(ctx, userID)
	assert.NoError(t, err)
	assert.Equal(t, 10.0, balance)
}
