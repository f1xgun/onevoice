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

// TestConfig_DraftReplyModel_DefaultsToLLMModel pins the graceful-fallback
// contract for DRAFT_REPLY_MODEL: when the operator only sets LLM_MODEL,
// the draft_reply handler MUST route at the main chat model rather than
// erroring or running with an empty model string.
func TestConfig_DraftReplyModel_DefaultsToLLMModel(t *testing.T) {
	t.Setenv("LLM_MODEL", "x")
	t.Setenv("DRAFT_REPLY_MODEL", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.DraftReplyModel,
		"DRAFT_REPLY_MODEL unset must fall back to LLM_MODEL — mirrors the TitlerModel pattern in services/api/internal/config")
}

// TestConfig_DraftReplyModel_RespectsEnv proves an explicit DRAFT_REPLY_MODEL
// overrides LLM_MODEL — the whole point of the two-env-var split (route
// draft_reply at cheap-tier independently of the main chat model).
func TestConfig_DraftReplyModel_RespectsEnv(t *testing.T) {
	t.Setenv("LLM_MODEL", "x")
	t.Setenv("DRAFT_REPLY_MODEL", "y")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "y", cfg.DraftReplyModel,
		"explicit DRAFT_REPLY_MODEL must win over LLM_MODEL fallback")
}

// TestConfig_DraftReplyModel_PropagatesEmptyWhenLLMModelMissing asserts that
// when LLM_MODEL itself is missing Load returns an error (matching the
// existing required-field behavior at TestLoad_MissingLLMModel) — so
// DraftReplyModel never needs to handle the empty-LLM-Model case at
// runtime; the boot fails fast first.
func TestConfig_DraftReplyModel_PropagatesEmptyWhenLLMModelMissing(t *testing.T) {
	t.Setenv("LLM_MODEL", "")
	t.Setenv("DRAFT_REPLY_MODEL", "")

	_, err := config.Load()
	assert.ErrorContains(t, err, "LLM_MODEL",
		"missing LLM_MODEL must still error — DraftReplyModel inherits the same fail-fast")
}

// TestConfig_APIInternalURL_DefaultHTTPS pins the default `https://api:8443`
// for the orchestrator → api billing hop. mTLS substrate from 25a-01 requires
// HTTPS on this endpoint; a plain-HTTP default would silently bypass mTLS at
// startup and surface only when the first billing POST hit a TLS-only listener.
func TestConfig_APIInternalURL_DefaultHTTPS(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("API_INTERNAL_URL", "") // explicitly clear

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api:8443", cfg.APIInternalURL,
		"default must be HTTPS — Plan 25a-01 mTLS listener requires it")
}

// TestConfig_APIInternalURL_RespectsEnv proves operator override wins.
func TestConfig_APIInternalURL_RespectsEnv(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("API_INTERNAL_URL", "https://api-staging.example.com:9443")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api-staging.example.com:9443", cfg.APIInternalURL)
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
