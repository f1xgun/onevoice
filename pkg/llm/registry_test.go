package llm_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// Runtime-policy tests (RecordSuccess / RecordFailure) live alongside
// the implementation in selector_test.go. Registry is now the config
// layer only, so only the entry CRUD surface gets exercised here.

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

func TestRegistry_RegisterModelProvider_OverwritesByProvider(t *testing.T) {
	// Same (model, provider) pair must overwrite in place — preserves the
	// pointer identity so a Selector holding a reference from a prior Pick
	// keeps seeing the latest config.
	registry := llm.NewRegistry()

	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:    "gpt-4",
		Provider: "openai",
		Priority: 1,
	})
	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:    "gpt-4",
		Provider: "openai",
		Priority: 9,
	})

	providers := registry.GetModelProviders("gpt-4")
	assert.Len(t, providers, 1)
	assert.Equal(t, 9, providers[0].Priority)
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
