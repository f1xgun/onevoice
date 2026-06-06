package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/config"
)

// canonicalACLJSON is the D-02 example ACL value used across config tests.
const canonicalACLJSON = `{"agent-telegram":["telegram"],"agent-vk":["vk"],"agent-yandex-business":["yandex_business"],"agent-google-business":["google_business"],"orchestrator":["telegram","vk","yandex_business","google_business"],"api":["*"]}`

// setValidLegal sets every LEGAL_* env var to a non-placeholder value that
// passes validateLegalProduction. Tests that gate on APP_ENV=production
// must call this so the legal validator does not trip on unrelated
// missing fields.
func setValidLegal(t *testing.T) {
	t.Helper()
	t.Setenv("LEGAL_ENTITY_NAME", "ООО Реальная компания")
	t.Setenv("LEGAL_INN", "7707083893")
	t.Setenv("LEGAL_ADDRESS", "123456 Москва ул. Пушкина д. 1 оф. 42")
	t.Setenv("LEGAL_EMAIL_PDN", "dpo@example.com")
}

// minTestEnv configures the env vars required by Config.Load()'s existing
// fail-fast validation (JWT_SECRET ≥32 chars, ENCRYPTION_KEY exactly 32
// bytes). Each test must call this so the validators pass and we
// can exercise the auto-titler fields. Uses the testing helper t.Setenv
// per repo convention so env state restores automatically on test cleanup.
func minTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "Tk8pZ3vXq2RmJ7wL4HdNcF9YbVgUaSx5KQePtBnCMrZyDoIxhEfWj1uvLA8")
	t.Setenv("ENCRYPTION_KEY", "uW4qX9pTzN3vM8yJ7sR2bL5kH1gD0fA6")
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", canonicalACLJSON)
}

func TestLoad_TitlerModel_Fallback(t *testing.T) {
	cases := []struct {
		name        string
		titlerModel string
		llmModel    string
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
			name:        "both unset → empty (graceful disable)",
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

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "sk-or-test", cfg.OpenRouterAPIKey)
	assert.Equal(t, "sk-oai-test", cfg.OpenAIAPIKey)
	assert.Empty(t, cfg.AnthropicAPIKey)
}

func TestLoad_LLMTier_Default(t *testing.T) {
	cases := []struct {
		name string
		tier string
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
	minTestEnv(t)
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
	minTestEnv(t)
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.SelfHostedEndpoints)
}

func TestLoad_RateLimits(t *testing.T) {
	t.Run("defaults when env unset", func(t *testing.T) {
		minTestEnv(t)

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 5, cfg.RateLimitRegister)
		assert.Equal(t, 10, cfg.RateLimitLogin)
		assert.Equal(t, 10, cfg.RateLimitChat)
		assert.Equal(t, 10, cfg.RateLimitHITL)
		assert.Equal(t, 10, cfg.RateLimitConsents)
	})

	t.Run("env override applied", func(t *testing.T) {
		minTestEnv(t)
		t.Setenv("RATE_LIMIT_REGISTER", "2")
		t.Setenv("RATE_LIMIT_LOGIN", "20")
		t.Setenv("RATE_LIMIT_CHAT", "30")
		t.Setenv("RATE_LIMIT_HITL", "40")
		t.Setenv("RATE_LIMIT_CONSENTS", "50")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.RateLimitRegister)
		assert.Equal(t, 20, cfg.RateLimitLogin)
		assert.Equal(t, 30, cfg.RateLimitChat)
		assert.Equal(t, 40, cfg.RateLimitHITL)
		assert.Equal(t, 50, cfg.RateLimitConsents)
	})
}

func TestLoad_HTTPTimeouts(t *testing.T) {
	t.Run("defaults when env unset", func(t *testing.T) {
		minTestEnv(t)

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, cfg.HTTPReadTimeout)
		assert.Equal(t, 10*time.Second, cfg.HTTPReadHeaderTimeout)
		assert.Equal(t, 60*time.Second, cfg.HTTPIdleTimeout)
		assert.Equal(t, 10*time.Second, cfg.OrchestratorFetchTimeout)
	})

	t.Run("env override applied", func(t *testing.T) {
		minTestEnv(t)
		t.Setenv("HTTP_READ_TIMEOUT", "45s")
		t.Setenv("HTTP_READ_HEADER_TIMEOUT", "20s")
		t.Setenv("HTTP_IDLE_TIMEOUT", "2m")
		t.Setenv("ORCHESTRATOR_FETCH_TIMEOUT", "30s")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 45*time.Second, cfg.HTTPReadTimeout)
		assert.Equal(t, 20*time.Second, cfg.HTTPReadHeaderTimeout)
		assert.Equal(t, 2*time.Minute, cfg.HTTPIdleTimeout)
		assert.Equal(t, 30*time.Second, cfg.OrchestratorFetchTimeout)
	})

	t.Run("invalid value falls back to default", func(t *testing.T) {
		minTestEnv(t)
		t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, cfg.HTTPReadTimeout)
	})
}

func TestLoad_CORSAllowedOrigins(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "unset → dev default localhost:3000",
			value: "",
			want:  []string{"http://localhost:3000"},
		},
		{
			name:  "single origin",
			value: "https://app.example.com",
			want:  []string{"https://app.example.com"},
		},
		{
			name:  "comma-separated list with spaces trimmed",
			value: "https://app.example.com, https://staging.example.com",
			want:  []string{"https://app.example.com", "https://staging.example.com"},
		},
		{
			name:  "empty entries dropped",
			value: "https://app.example.com,,",
			want:  []string{"https://app.example.com"},
		},
		{
			name:  "blank-only value falls back to default",
			value: "  ,  ,",
			want:  []string{"http://localhost:3000"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", c.value)

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, c.want, cfg.CORSAllowedOrigins)
		})
	}
}

func TestLoad_SelfHostedEndpoints_StopsAtGap(t *testing.T) {
	minTestEnv(t)
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	t.Setenv("SELF_HOSTED_0_MODEL", "llama3.1")
	t.Setenv("SELF_HOSTED_2_URL", "http://vm3:11434/v1")
	t.Setenv("SELF_HOSTED_2_MODEL", "gemma")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.SelfHostedEndpoints, 1)
	assert.Equal(t, "llama3.1", cfg.SelfHostedEndpoints[0].Model)
}

// TestLoad_LocalFallback_InvalidIntFailsLoud — regression for WR-03.
// Previously a non-integer LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR value was
// silently coerced to the 2000 default. With the fix the operator's typo is
// now surfaced as a boot error so misconfig fails loud rather than running
// the api on a value the operator never chose.
func TestLoad_LocalFallback_InvalidIntFailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("LLM_RATELIMIT_ON_REDIS_DOWN", "local_fallback")
	t.Setenv("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR", "foo")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR")
	assert.Contains(t, err.Error(), `"foo"`)
}

func TestConfig_PG_Defaults(t *testing.T) {
	minTestEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 25, cfg.PGMaxConns)
	assert.Equal(t, 2, cfg.PGMinConns)
	assert.Equal(t, 30*time.Minute, cfg.PGMaxConnLifetime)
	assert.Equal(t, 15*time.Minute, cfg.PGMaxConnIdleTime)
	assert.Equal(t, 1*time.Minute, cfg.PGHealthCheckPeriod)
	assert.Equal(t, 3*time.Minute, cfg.PGMaxConnLifetimeJitter)
}

func TestConfig_PG_EnvOverride_MaxConns(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MAX_CONNS", "50")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 50, cfg.PGMaxConns)
}

func TestConfig_PG_EnvOverride_MinConns(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MIN_CONNS", "5")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.PGMinConns)
}

func TestConfig_PG_EnvOverride_Duration(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MAX_CONN_LIFETIME", "1h")
	t.Setenv("PG_MAX_CONN_IDLE_TIME", "20m")
	t.Setenv("PG_HEALTH_CHECK_PERIOD", "2m")
	t.Setenv("PG_MAX_CONN_LIFETIME_JITTER", "5m")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, time.Hour, cfg.PGMaxConnLifetime)
	assert.Equal(t, 20*time.Minute, cfg.PGMaxConnIdleTime)
	assert.Equal(t, 2*time.Minute, cfg.PGHealthCheckPeriod)
	assert.Equal(t, 5*time.Minute, cfg.PGMaxConnLifetimeJitter)
}

func TestConfig_PG_InvalidInt_FailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MAX_CONNS", "abc")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_MAX_CONNS")
}

func TestConfig_PG_InvalidDuration_FailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MAX_CONN_LIFETIME", "10x")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_MAX_CONN_LIFETIME")
}

func TestConfig_PG_MaxConnsZero_FailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MAX_CONNS", "0")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "> 0")
}

func TestConfig_PG_MaxConnsExceedsInt32_FailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MAX_CONNS", "2147483648")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_MAX_CONNS")
}

func TestConfig_PG_MinConnsExceedsMax_FailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MIN_CONNS", "50")
	t.Setenv("PG_MAX_CONNS", "10")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_MIN_CONNS")
}

func TestConfig_PG_MinConnsNegative_FailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("PG_MIN_CONNS", "-1")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_MIN_CONNS")
}

func TestLoad_RejectDefaultEncryptionKey(t *testing.T) {
	t.Setenv("JWT_SECRET", "Tk8pZ3vXq2RmJ7wL4HdNcF9YbVgUaSx5KQePtBnCMrZyDoIxhEfWj1uvLA8")
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deny-list")
}

func TestLoad_RejectLowEntropyKey(t *testing.T) {
	t.Setenv("JWT_SECRET", "Tk8pZ3vXq2RmJ7wL4HdNcF9YbVgUaSx5KQePtBnCMrZyDoIxhEfWj1uvLA8")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("a", 32))
	_, err := config.Load()
	require.Error(t, err)
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "entropy") || strings.Contains(msg, "repeated"),
		"expected entropy or repeated mention, got %q", msg)
}

func TestLoad_AcceptStrongKey(t *testing.T) {
	minTestEnv(t)
	_, err := config.Load()
	require.NoError(t, err)
}

func TestLoad_RejectMissingLegalINN_Prod(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidLegal(t)
	t.Setenv("LEGAL_INN", "")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LEGAL_INN")
}

func TestLoad_RejectPlaceholderEntityName_Prod(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidLegal(t)
	t.Setenv("LEGAL_ENTITY_NAME", "[Юридическое лицо — будет обновлено]")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LEGAL_ENTITY_NAME")
}

func TestLoad_RejectInvalidINNChecksum_Prod(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidLegal(t)
	t.Setenv("LEGAL_INN", "1234567890")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
}

func TestLoad_AcceptValidLegal_Prod(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidLegal(t)
	_, err := config.Load()
	require.NoError(t, err)
}

func TestLoad_AllowPlaceholders_Dev(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("LEGAL_ENTITY_NAME", "[Юридическое лицо — будет обновлено]")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "[Юридическое лицо — будет обновлено]", cfg.LegalEntityName)
}

func TestConfigInternalACL_Loaded(t *testing.T) {
	minTestEnv(t)
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", canonicalACLJSON)
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.InternalACL, 6)
	assert.Equal(t, []string{"telegram"}, cfg.InternalACL["agent-telegram"])
}

func TestConfigInternalACL_APIWildcard(t *testing.T) {
	minTestEnv(t)
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", canonicalACLJSON)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, reflect.DeepEqual([]string{"*"}, cfg.InternalACL["api"]),
		"api CN must map to the wildcard value list")
}

func TestConfigInternalACL_MissingFailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", "")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ONEVOICE_INTERNAL_ACL_JSON")
}

func TestConfigInternalACL_InvalidJSONFailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", "{not-valid-json")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ONEVOICE_INTERNAL_ACL_JSON")
}

func TestConfigInternalACL_EmptyObjectFailsLoud(t *testing.T) {
	minTestEnv(t)
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", "{}")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ONEVOICE_INTERNAL_ACL_JSON")
	assert.Contains(t, err.Error(), "at least one")
}
