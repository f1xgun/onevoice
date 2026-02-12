package llm_test

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/stretchr/testify/assert"
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
