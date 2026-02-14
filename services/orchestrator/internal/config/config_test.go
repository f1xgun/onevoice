package config_test

import (
	"testing"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiredFields(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", cfg.LLMModel)
	assert.Equal(t, "8090", cfg.Port) // default
	assert.Equal(t, 10, cfg.MaxIterations) // default
}

func TestLoad_MissingLLMModel(t *testing.T) {
	t.Setenv("LLM_MODEL", "") // explicitly clear
	_, err := config.Load()
	assert.ErrorContains(t, err, "LLM_MODEL")
}

func TestLoad_CustomMaxIterations(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("MAX_ITERATIONS", "5")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.MaxIterations)
}

func TestLoad_DefaultNATSUrl(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "nats://localhost:4222", cfg.NATSUrl)
}
