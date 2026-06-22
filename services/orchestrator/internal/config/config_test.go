package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

func TestLoad_RequiredFields(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", cfg.LLMModel)
	assert.Equal(t, "8090", cfg.Port)
	assert.Equal(t, 10, cfg.MaxIterations)
}

func TestLoad_MissingLLMModel(t *testing.T) {
	t.Setenv("LLM_MODEL", "")
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
// for the orchestrator → api billing hop. The mTLS substrate requires HTTPS
// on this endpoint; a plain-HTTP default would silently bypass mTLS at startup
// and only surface when the first billing POST hit a TLS-only listener.
func TestConfig_APIInternalURL_DefaultHTTPS(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("API_INTERNAL_URL", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api:8443", cfg.APIInternalURL,
		"default must be HTTPS — the mTLS listener requires it")
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
	t.Setenv("SELF_HOSTED_2_URL", "http://vm3:11434/v1")
	t.Setenv("SELF_HOSTED_2_MODEL", "gemma")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.SelfHostedEndpoints, 1)
	assert.Equal(t, "llama3.1", cfg.SelfHostedEndpoints[0].Model)
}

// ---------------------------------------------------------------------
// Cost-guard config tests
// ---------------------------------------------------------------------

func requireLoad(t *testing.T) (*config.Config, error) {
	t.Helper()
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	return config.Load()
}

func TestConfig_ConversationCaps_Defaults(t *testing.T) {
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, 50000, cfg.ConversationInputCap)
	assert.Equal(t, 10000, cfg.ConversationOutputCap)
}

func TestConfig_ConversationCaps_EnvOverride(t *testing.T) {
	t.Setenv("LLM_CONVERSATION_INPUT_CAP", "100000")
	t.Setenv("LLM_CONVERSATION_OUTPUT_CAP", "20000")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, 100000, cfg.ConversationInputCap)
	assert.Equal(t, 20000, cfg.ConversationOutputCap)
}

func TestConfig_ConversationCaps_Zero(t *testing.T) {
	t.Setenv("LLM_CONVERSATION_INPUT_CAP", "0")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.ConversationInputCap)
}

func TestConfig_FreeTierDailySpend_Default(t *testing.T) {
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, 0.0, cfg.FreeTierDailySpendUSD)
}

func TestConfig_FreeTierDailySpend_EnvOverride(t *testing.T) {
	t.Setenv("LLM_FREE_TIER_DAILY_SPEND_USD", "2.5")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.InDelta(t, 2.5, cfg.FreeTierDailySpendUSD, 1e-9)
}

func TestConfig_FreeTierDailySpend_Unlimited(t *testing.T) {
	t.Setenv("LLM_FREE_TIER_DAILY_SPEND_USD", "-1")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.InDelta(t, -1.0, cfg.FreeTierDailySpendUSD, 1e-9)
}

func TestConfig_RedisDownPolicy_Default(t *testing.T) {
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, "block", cfg.RedisDownPolicy)
}

func TestConfig_RedisDownPolicy_LocalFallback(t *testing.T) {
	t.Setenv("LLM_RATELIMIT_ON_REDIS_DOWN", "local_fallback")
	t.Setenv("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR", "3000")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, "local_fallback", cfg.RedisDownPolicy)
	assert.Equal(t, 3000, cfg.LocalFallbackRequestsPerHour)
}

func TestConfig_RedisDownPolicy_Invalid(t *testing.T) {
	t.Setenv("LLM_RATELIMIT_ON_REDIS_DOWN", "panic")
	_, err := requireLoad(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "block")
	assert.Contains(t, err.Error(), "local_fallback")
}

func TestConfig_LocalFallback_RequiresRate(t *testing.T) {
	t.Setenv("LLM_RATELIMIT_ON_REDIS_DOWN", "local_fallback")
	t.Setenv("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR", "0")
	_, err := requireLoad(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR")
}

func TestConfig_LocalFallback_DefaultRate(t *testing.T) {
	t.Setenv("LLM_RATELIMIT_ON_REDIS_DOWN", "local_fallback")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, 2000, cfg.LocalFallbackRequestsPerHour)
}

// TestConfig_LocalFallback_InvalidIntFailsLoud — regression for WR-03.
// Previously a non-integer env value was silently swallowed and the loader
// kept the 2000 default. With a non-integer value the operator's typo is now
// surfaced as a boot error instead of being coerced into the default rate.
func TestConfig_LocalFallback_InvalidIntFailsLoud(t *testing.T) {
	t.Setenv("LLM_RATELIMIT_ON_REDIS_DOWN", "local_fallback")
	t.Setenv("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR", "foo")
	_, err := requireLoad(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR")
	assert.Contains(t, err.Error(), `"foo"`)
}

func TestConfig_LocalFallbackWindow_Default(t *testing.T) {
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.LocalFallbackWindow)
}

// TestConfig_AllowTransborderLLM_DefaultFalse pins the compliant-safe default:
// unset means outbound personal-data redaction stays ON (the orchestrator wires
// RedactOutboundPDn from the inverse of this flag).
func TestConfig_AllowTransborderLLM_DefaultFalse(t *testing.T) {
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.False(t, cfg.AllowTransborderLLM)
}

func TestConfig_AllowTransborderLLM_EnvTrue(t *testing.T) {
	for _, v := range []string{"true", "1", "TRUE"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("ALLOW_TRANSBORDER_LLM", v)
			cfg, err := requireLoad(t)
			require.NoError(t, err)
			assert.True(t, cfg.AllowTransborderLLM)
		})
	}
}

func TestConfig_AllowTransborderLLM_EnvFalse(t *testing.T) {
	t.Setenv("ALLOW_TRANSBORDER_LLM", "false")
	cfg, err := requireLoad(t)
	require.NoError(t, err)
	assert.False(t, cfg.AllowTransborderLLM)
}

func TestConfig_AllowTransborderLLM_InvalidFailsLoud(t *testing.T) {
	t.Setenv("ALLOW_TRANSBORDER_LLM", "maybe")
	_, err := requireLoad(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ALLOW_TRANSBORDER_LLM")
	assert.Contains(t, err.Error(), `"maybe"`)
}
