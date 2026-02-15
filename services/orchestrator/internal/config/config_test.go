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

func TestLoad_ActiveIntegrations(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("ACTIVE_INTEGRATIONS", "telegram, vk , yandex_business")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"telegram", "vk", "yandex_business"}, cfg.ActiveIntegrations)
}

func TestLoad_ActiveIntegrations_Empty(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("ACTIVE_INTEGRATIONS", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Nil(t, cfg.ActiveIntegrations)
}

func TestLoad_ProviderAPIKeys(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("OPENAI_API_KEY", "sk-test")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "sk-or-test", cfg.OpenRouterAPIKey)
	assert.Equal(t, "sk-test", cfg.OpenAIAPIKey)
	assert.Empty(t, cfg.AnthropicAPIKey)
}
