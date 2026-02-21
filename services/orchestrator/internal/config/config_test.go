package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

func TestLoad_RequiredFields(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", cfg.LLMModel)
	assert.Equal(t, "8090", cfg.Port)      // default
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

func TestLoad_SelfHostedEndpoints(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	t.Setenv("SELF_HOSTED_0_MODEL", "llama3.1")
	t.Setenv("SELF_HOSTED_0_API_KEY", "sk-local")
	t.Setenv("SELF_HOSTED_1_URL", "http://vm2:8080/v1")
	t.Setenv("SELF_HOSTED_1_MODEL", "mistral")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.SelfHostedEndpoints, 2)
	assert.Equal(t, "http://vm1:11434/v1", cfg.SelfHostedEndpoints[0].URL)
	assert.Equal(t, "llama3.1", cfg.SelfHostedEndpoints[0].Model)
	assert.Equal(t, "sk-local", cfg.SelfHostedEndpoints[0].APIKey)
	assert.Equal(t, "http://vm2:8080/v1", cfg.SelfHostedEndpoints[1].URL)
	assert.Equal(t, "mistral", cfg.SelfHostedEndpoints[1].Model)
	assert.Empty(t, cfg.SelfHostedEndpoints[1].APIKey)
}

func TestLoad_SelfHostedEndpoints_MissingModel_Skipped(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	// no SELF_HOSTED_0_MODEL

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.SelfHostedEndpoints)
}

func TestLoad_SelfHostedEndpoints_StopsAtGap(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	t.Setenv("SELF_HOSTED_0_MODEL", "llama3.1")
	// index 1 missing — scan stops here
	t.Setenv("SELF_HOSTED_2_URL", "http://vm3:11434/v1")
	t.Setenv("SELF_HOSTED_2_MODEL", "gemma")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.SelfHostedEndpoints, 1)
	assert.Equal(t, "llama3.1", cfg.SelfHostedEndpoints[0].Model)
}
