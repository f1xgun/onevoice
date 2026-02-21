package llm_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/llm"
)

func TestRegistry_RegisterModelProvider(t *testing.T) {
	registry := llm.NewRegistry()

	entry := &llm.ModelProviderEntry{
		Model:              "claude-3.5-sonnet",
		Provider:           "openrouter",
		InputCostPer1MTok:  3.00,
		OutputCostPer1MTok: 15.00,
		AvgLatencyMs:       1200,
		HealthStatus:       "healthy",
		Enabled:            true,
		Priority:           1,
		LastCheckedAt:      time.Now(),
	}

	registry.RegisterModelProvider(entry)

	providers := registry.GetModelProviders("claude-3.5-sonnet")
	assert.Len(t, providers, 1)
	assert.Equal(t, "openrouter", providers[0].Provider)
	assert.Equal(t, 3.0, providers[0].InputCostPer1MTok)
}

func TestRegistry_ModelExists(t *testing.T) {
	registry := llm.NewRegistry()

	assert.False(t, registry.ModelExists("nonexistent"))

	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:    "gpt-4",
		Provider: "openai",
	})

	assert.True(t, registry.ModelExists("gpt-4"))
}

func TestRegistry_RecordSuccess(t *testing.T) {
	registry := llm.NewRegistry()

	entry := &llm.ModelProviderEntry{
		Model:        "test-model",
		Provider:     "test-provider",
		AvgLatencyMs: 0,
	}
	registry.RegisterModelProvider(entry)

	// Record success with 1000ms latency
	registry.RecordSuccess("test-provider", "test-model", 1000*time.Millisecond)

	// Verify metrics updated
	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, 1000, providers[0].AvgLatencyMs)
	assert.Equal(t, "healthy", providers[0].HealthStatus)
}

func TestRegistry_RecordFailure(t *testing.T) {
	registry := llm.NewRegistry()

	entry := &llm.ModelProviderEntry{
		Model:        "test-model",
		Provider:     "test-provider",
		HealthStatus: "healthy",
	}
	registry.RegisterModelProvider(entry)

	// Record 6 failures (>50% failure rate)
	for i := 0; i < 6; i++ {
		registry.RecordFailure("test-provider", "test-model")
	}

	// Verify health status degraded
	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, "down", providers[0].HealthStatus)
}
