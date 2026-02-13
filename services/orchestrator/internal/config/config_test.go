package config_test

import (
	"os"
	"testing"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiredFields(t *testing.T) {
	os.Setenv("LLM_MODEL", "gpt-4o-mini")
	defer os.Unsetenv("LLM_MODEL")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", cfg.LLMModel)
	assert.Equal(t, "8090", cfg.Port) // default
}

func TestLoad_MissingLLMModel(t *testing.T) {
	os.Unsetenv("LLM_MODEL")
	_, err := config.Load()
	assert.ErrorContains(t, err, "LLM_MODEL")
}
