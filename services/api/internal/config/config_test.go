package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/config"
)

// minTestEnv configures the env vars required by Config.Load()'s existing
// fail-fast validation (JWT_SECRET ≥32 chars, ENCRYPTION_KEY exactly 32
// bytes). Each Phase 18 test must call this so the validators pass and we
// can exercise the new auto-titler fields. Uses the testing helper t.Setenv
// per repo convention so env state restores automatically on test cleanup.
func minTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-jwt-secret-with-at-least-32-chars")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef") // exactly 32 bytes
}

func TestLoad_TitlerModel_Fallback(t *testing.T) {
	cases := []struct {
		name        string
		titlerModel string // "" means unset
		llmModel    string // "" means unset
		want        string
	}{
		{
			name:        "TITLER_MODEL set wins over LLM_MODEL",
			titlerModel: "gpt-4o-mini",
			llmModel:    "gpt-4o",
			want:        "gpt-4o-mini",
		},
		{
			name:        "TITLER_MODEL unset falls back to LLM_MODEL",
			titlerModel: "",
			llmModel:    "gpt-4o",
			want:        "gpt-4o",
		},
		{
			name:        "both unset → empty (graceful disable per Pitfall 1 / A6)",
			titlerModel: "",
			llmModel:    "",
			want:        "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			t.Setenv("TITLER_MODEL", c.titlerModel)
			t.Setenv("LLM_MODEL", c.llmModel)

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, c.want, cfg.TitlerModel)
			assert.Equal(t, c.llmModel, cfg.LLMModel)
		})
	}
}

func TestLoad_GracefulDisable_NoLLMEnv(t *testing.T) {
	// Pitfall 1 / Assumption A6: API must boot cleanly when neither
	// TITLER_MODEL nor LLM_MODEL is set, AND when no provider key is set.
	minTestEnv(t)
	t.Setenv("TITLER_MODEL", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfg, err := config.Load()
	require.NoError(t, err, "Load must succeed even with no LLM env (graceful disable)")
	assert.Empty(t, cfg.TitlerModel)
	assert.Empty(t, cfg.LLMModel)
	assert.Empty(t, cfg.OpenRouterAPIKey)
	assert.Empty(t, cfg.OpenAIAPIKey)
	assert.Empty(t, cfg.AnthropicAPIKey)
	assert.Empty(t, cfg.SelfHostedEndpoints)
}

func TestLoad_ProviderKeys(t *testing.T) {
	minTestEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("OPENAI_API_KEY", "sk-oai-test")
	// ANTHROPIC_API_KEY left unset → empty string

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "sk-or-test", cfg.OpenRouterAPIKey)
	assert.Equal(t, "sk-oai-test", cfg.OpenAIAPIKey)
	assert.Empty(t, cfg.AnthropicAPIKey)
}

func TestLoad_LLMTier_Default(t *testing.T) {
	cases := []struct {
		name string
		tier string // "" means unset
		want string
	}{
		{name: "unset → free default", tier: "", want: "free"},
		{name: "explicit value retained", tier: "premium", want: "premium"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			t.Setenv("LLM_TIER", c.tier)

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, c.want, cfg.LLMTier)
		})
	}
}

func TestLoad_SelfHostedEndpoints(t *testing.T) {
	// Mirrors orchestrator's TestLoad_SelfHostedEndpoints — the parser is
	// lifted verbatim so the same expectations apply.
	minTestEnv(t)
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	t.Setenv("SELF_HOSTED_0_MODEL", "llama3.1")
	t.Setenv("SELF_HOSTED_0_API_KEY", "sk-local")
	t.Setenv("SELF_HOSTED_1_URL", "http://vm2:8080/v1")
	t.Setenv("SELF_HOSTED_1_MODEL", "mistral")
	// SELF_HOSTED_1_API_KEY left unset

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
	minTestEnv(t)
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	// no SELF_HOSTED_0_MODEL → entry skipped, but scan continues to N=1
	// which is also unset, ending the loop.

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.SelfHostedEndpoints)
}

func TestLoad_SelfHostedEndpoints_StopsAtGap(t *testing.T) {
	minTestEnv(t)
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
