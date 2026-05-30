package llm_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// Phase 25a Test 7 — Marshal/Unmarshal UsageLog with all new fields populated;
// verifies the JSON tags on BusinessID, ConversationID, RequestID, and the cache
// token columns ship the wire format Plan 25a-03 (billingclient) will rely on.
func TestUsageLog_JSONRoundTrip(t *testing.T) {
	created := time.Date(2026, 5, 30, 14, 0, 0, 0, time.UTC)
	in := llm.UsageLog{
		ID:                  uuid.New(),
		BusinessID:          uuid.New(),
		UserID:              uuid.New(),
		ConversationID:      "67f4a8b27a9ad15d4f8a1c00",
		RequestID:           "req-abc-123",
		Model:               "anthropic/claude-sonnet-4-6",
		Provider:            "anthropic",
		InputTokens:         1000,
		OutputTokens:        500,
		CacheReadTokens:     2500,
		CacheCreationTokens: 750,
		ProviderCostUSD:     0.012345,
		CommissionUSD:       0.002469,
		UserCostUSD:         0.014814,
		UserTier:            "basic",
		CreatedAt:           created,
	}

	blob, err := json.Marshal(in)
	require.NoError(t, err)

	var out llm.UsageLog
	require.NoError(t, json.Unmarshal(blob, &out))
	assert.Equal(t, in, out)
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

// MockBillingRepository implements BillingRepository for testing. Phase 25a
// extends it with a Logs slice + LastLog helper so the new router tests can
// assert per-log fields populated by logBilling under the async goroutine.
type MockBillingRepository struct {
	mu   sync.Mutex
	logs []llm.UsageLog
	Logs []*llm.UsageLog // appended in order of LogUsage calls; read via LastLog()
	// DailySpendByBusiness lets tests preload return values for GetDailySpend;
	// unset entries default to 0.
	DailySpendByBusiness map[uuid.UUID]float64
}

func (m *MockBillingRepository) LogUsage(_ context.Context, log *llm.UsageLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, *log)
	// Store a copy on the new-shape slice so callers reading after the fact
	// see a stable pointer that won't be mutated by future LogUsage calls.
	cp := *log
	m.Logs = append(m.Logs, &cp)
	return nil
}

// LastLog returns the most recently captured UsageLog, or nil when LogUsage has
// not been called. Lockless callers (assert.Eventually polling) read the slice
// length atomically via the mutex.
func (m *MockBillingRepository) LastLog() *llm.UsageLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Logs) == 0 {
		return nil
	}
	return m.Logs[len(m.Logs)-1]
}

// CallCount reports how many times LogUsage has been invoked.
func (m *MockBillingRepository) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Logs)
}

func (m *MockBillingRepository) GetUserBalance(_ context.Context, _ uuid.UUID) (float64, error) {
	return 10.0, nil // Mock balance
}

// GetDailySpend (Phase 25a signature) — returns the preloaded value from
// DailySpendByBusiness, or sums the captured logs by business when the map is
// nil. Falls back to 0 when neither is populated.
func (m *MockBillingRepository) GetDailySpend(_ context.Context, businessID uuid.UUID, _ time.Time) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DailySpendByBusiness != nil {
		return m.DailySpendByBusiness[businessID], nil
	}
	total := 0.0
	for _, log := range m.logs {
		if log.BusinessID == businessID {
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
	businessID := uuid.New()

	// Log usage
	log := &llm.UsageLog{
		ID:          uuid.New(),
		BusinessID:  businessID,
		UserID:      userID,
		Model:       "gpt-4",
		Provider:    "openai",
		UserCostUSD: 0.05,
		CreatedAt:   time.Now(),
	}
	err := repo.LogUsage(ctx, log)
	assert.NoError(t, err)

	// Get daily spend — aggregate by business in the in-memory fallback.
	spend, err := repo.GetDailySpend(ctx, businessID, time.Now())
	assert.NoError(t, err)
	assert.Equal(t, 0.05, spend)

	// Get balance
	balance, err := repo.GetUserBalance(ctx, userID)
	assert.NoError(t, err)
	assert.Equal(t, 10.0, balance)
}
