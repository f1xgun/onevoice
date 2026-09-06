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

// canonicalACLJSON is a representative internal-ACL value used across config tests.
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
	t.Setenv("A2A_PAYLOAD_KEY", "pT9wX2qZ7vN4mJ8yR3sB6kL1hG5dF0aC")
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", canonicalACLJSON)
	t.Setenv("ORCHESTRATOR_INTERNAL_SECRET", "test-internal-secret-abcdef123")
	t.Setenv("TOKEN_ENCRYPTION_KMS_KEY_ID", "aes256-test-key-id")
	t.Setenv("YC_SA_JSON_CREDENTIALS", `{"id":"test","service_account_id":"test","created_at":"2024-01-01T00:00:00Z","key_algorithm":"RSA_2048","public_key":"-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0000000000000000000=\n-----END PUBLIC KEY-----\n","private_key":"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0000000000000000000=\n-----END RSA PRIVATE KEY-----\n"}`)
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
		assert.Equal(t, 60, cfg.RateLimitRefresh)
		assert.Equal(t, 10, cfg.RateLimitChat)
		assert.Equal(t, 10, cfg.RateLimitHITL)
		assert.Equal(t, 10, cfg.RateLimitConsents)
		assert.Equal(t, 30, cfg.RateLimitSearch)
	})

	t.Run("env override applied", func(t *testing.T) {
		minTestEnv(t)
		t.Setenv("RATE_LIMIT_REGISTER", "2")
		t.Setenv("RATE_LIMIT_LOGIN", "20")
		t.Setenv("RATE_LIMIT_REFRESH", "120")
		t.Setenv("RATE_LIMIT_CHAT", "30")
		t.Setenv("RATE_LIMIT_HITL", "40")
		t.Setenv("RATE_LIMIT_CONSENTS", "50")
		t.Setenv("RATE_LIMIT_SEARCH", "7")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.RateLimitRegister)
		assert.Equal(t, 20, cfg.RateLimitLogin)
		assert.Equal(t, 120, cfg.RateLimitRefresh)
		assert.Equal(t, 30, cfg.RateLimitChat)
		assert.Equal(t, 40, cfg.RateLimitHITL)
		assert.Equal(t, 50, cfg.RateLimitConsents)
		assert.Equal(t, 7, cfg.RateLimitSearch)
	})
}

func TestLoad_UserRateLimits_NonPositiveFailsLoud(t *testing.T) {
	keys := []string{
		"RATE_LIMIT_CHAT",
		"RATE_LIMIT_HITL",
		"RATE_LIMIT_CONSENTS",
		"RATE_LIMIT_TELEMETRY",
		"RATE_LIMIT_WRITES",
		"RATE_LIMIT_INVITATIONS",
		"RATE_LIMIT_SEARCH",
	}
	for _, key := range keys {
		for _, value := range []string{"0", "-1", "notanint"} {
			t.Run(key+"="+value, func(t *testing.T) {
				minTestEnv(t)
				t.Setenv(key, value)
				_, err := config.Load()
				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoad_AuthRateLimits_NonPositiveFailsLoud(t *testing.T) {
	keys := []string{
		"RATE_LIMIT_REGISTER",
		"RATE_LIMIT_LOGIN",
		"RATE_LIMIT_REFRESH",
	}
	for _, key := range keys {
		for _, value := range []string{"0", "-1", "notanint"} {
			t.Run(key+"="+value, func(t *testing.T) {
				minTestEnv(t)
				t.Setenv(key, value)
				_, err := config.Load()
				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoad_AuthRateLimits_PositiveLoadsCleanly(t *testing.T) {
	minTestEnv(t)
	t.Setenv("RATE_LIMIT_REGISTER", "3")
	t.Setenv("RATE_LIMIT_LOGIN", "11")
	t.Setenv("RATE_LIMIT_REFRESH", "90")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.RateLimitRegister)
	assert.Equal(t, 11, cfg.RateLimitLogin)
	assert.Equal(t, 90, cfg.RateLimitRefresh)
}

func TestLoad_UserRateLimits_PositiveLoadsCleanly(t *testing.T) {
	minTestEnv(t)
	t.Setenv("RATE_LIMIT_SEARCH", "1")
	t.Setenv("RATE_LIMIT_WRITES", "120")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.RateLimitSearch)
	assert.Equal(t, 120, cfg.RateLimitWrites)
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
		name      string
		value     string
		publicURL string
		appEnv    string
		want      []string
	}{
		{
			name:  "unset → public origin plus the dev origin",
			value: "",
			want:  []string{"http://localhost", "http://localhost:3000"},
		},
		{
			name:      "unset behind a reverse proxy → that proxy's origin is allowed",
			value:     "",
			publicURL: "https://app.example.com",
			want:      []string{"https://app.example.com", "http://localhost:3000"},
		},
		{
			name:      "unset in production → public origin only",
			value:     "",
			publicURL: "https://app.example.com",
			appEnv:    "production",
			want:      []string{"https://app.example.com"},
		},
		{
			name:      "unset with PUBLIC_URL equal to the dev origin → no duplicate",
			value:     "",
			publicURL: "http://localhost:3000",
			want:      []string{"http://localhost:3000"},
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
			want:  []string{"http://localhost", "http://localhost:3000"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			if c.appEnv == "production" {
				setValidLegal(t)
				t.Setenv("LLM_MODEL", "")
				t.Setenv("TITLER_MODEL", "")
			}
			t.Setenv("APP_ENV", c.appEnv)
			t.Setenv("PUBLIC_URL", c.publicURL)
			t.Setenv("CORS_ALLOWED_ORIGINS", c.value)

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, c.want, cfg.CORSAllowedOrigins)
		})
	}
}

// TestLoad_SecureCookies pins the SECURE_COOKIES default to the PUBLIC_URL
// scheme: a Secure `__Host-` refresh cookie is discarded by browsers on a
// plain-http origin, which silently signs the user out on every full page load.
func TestLoad_SecureCookies(t *testing.T) {
	cases := []struct {
		name      string
		publicURL string
		value     string
		want      bool
		wantErr   bool
	}{
		{
			name: "unset with the default http origin → off",
			want: false,
		},
		{
			name:      "unset with an https origin → on",
			publicURL: "https://app.example.com",
			want:      true,
		},
		{
			name:      "unset with an uppercase http origin → off",
			publicURL: "HTTP://localhost",
			want:      false,
		},
		{
			name:      "explicit true wins over an http origin",
			publicURL: "http://localhost",
			value:     "true",
			want:      true,
		},
		{
			name:      "explicit false wins over an https origin",
			publicURL: "https://app.example.com",
			value:     "false",
			want:      false,
		},
		{
			name:    "unparsable value fails loud",
			value:   "yes-please",
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			t.Setenv("PUBLIC_URL", c.publicURL)
			t.Setenv("SECURE_COOKIES", c.value)

			cfg, err := config.Load()
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, cfg.SecureCookies)
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

func TestLoad_MessageHistoryLimit_Default(t *testing.T) {
	minTestEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 100, cfg.MessageHistoryLimit)
}

func TestLoad_MessageHistoryLimit_EnvOverride(t *testing.T) {
	minTestEnv(t)
	t.Setenv("MESSAGE_HISTORY_LIMIT", "250")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 250, cfg.MessageHistoryLimit)
}

func TestLoad_MessageHistoryLimit_InvalidFailsLoud(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"non_integer", "foo"},
		{"zero", "0"},
		{"negative", "-1"},
		{"over_max", "501"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			t.Setenv("MESSAGE_HISTORY_LIMIT", c.value)
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "MESSAGE_HISTORY_LIMIT")
		})
	}
}

func TestLoad_OutboxPollInterval_NonPositiveFailsLoud(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"zero", "0s"},
		{"negative", "-1s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			minTestEnv(t)
			t.Setenv("OUTBOX_POLL_INTERVAL", c.value)
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "OUTBOX_POLL_INTERVAL")
		})
	}
}

func TestLoad_OutboxPollInterval_Default(t *testing.T) {
	minTestEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, cfg.OutboxPollInterval)
}

func TestLoad_OutboxPollInterval_PositiveLoads(t *testing.T) {
	minTestEnv(t)
	t.Setenv("OUTBOX_POLL_INTERVAL", "10s")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, cfg.OutboxPollInterval)
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

func TestLoad_RejectMissingOrchestratorSecret_Prod(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidLegal(t)
	t.Setenv("ORCHESTRATOR_INTERNAL_SECRET", "")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ORCHESTRATOR_INTERNAL_SECRET")
}

func TestLoad_RejectShortOrchestratorSecret(t *testing.T) {
	minTestEnv(t)
	t.Setenv("ORCHESTRATOR_INTERNAL_SECRET", "short")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ORCHESTRATOR_INTERNAL_SECRET")
}

func TestLoad_AllowMissingOrchestratorSecret_Dev(t *testing.T) {
	minTestEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ORCHESTRATOR_INTERNAL_SECRET", "")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.OrchestratorInternalSecret)
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

func TestConfigIsProduction(t *testing.T) {
	tests := []struct {
		appEnv string
		want   bool
	}{
		{"production", true},
		{"Production", true},
		{"  production  ", true},
		{"staging", false},
		{"development", false},
		{"", false},
		{"prod", false},
	}
	for _, tt := range tests {
		t.Run(tt.appEnv, func(t *testing.T) {
			cfg := &config.Config{AppEnv: tt.appEnv}
			assert.Equal(t, tt.want, cfg.IsProduction())
		})
	}
}

func TestRequireInternalMTLS(t *testing.T) {
	tests := []struct {
		name          string
		appEnv        string
		kmsKeyID      string
		tlsConfigured bool
		wantErr       bool
	}{
		{name: "tls configured in prod is fine", appEnv: "production", tlsConfigured: true, wantErr: false},
		{name: "tls configured with kms is fine", kmsKeyID: "kms-key-1", tlsConfigured: true, wantErr: false},
		{name: "prod without tls is rejected", appEnv: "production", tlsConfigured: false, wantErr: true},
		{name: "kms without tls is rejected", kmsKeyID: "kms-key-1", tlsConfigured: false, wantErr: true},
		{name: "prod+kms without tls is rejected", appEnv: "production", kmsKeyID: "kms-key-1", tlsConfigured: false, wantErr: true},
		{name: "dev without tls or kms is allowed", appEnv: "development", tlsConfigured: false, wantErr: false},
		{name: "empty env without tls or kms is allowed", appEnv: "", tlsConfigured: false, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{AppEnv: tt.appEnv, TokenEncryptionKMSKeyID: tt.kmsKeyID}
			err := cfg.RequireInternalMTLS(tt.tlsConfigured)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigLoad_PopulatesAppEnv(t *testing.T) {
	minTestEnv(t)
	setValidLegal(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("ONEVOICE_INTERNAL_ACL_JSON", canonicalACLJSON)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "production", cfg.AppEnv)
	assert.True(t, cfg.IsProduction())
}

func TestConfigLoad_A2APayloadKeyRequired(t *testing.T) {
	// Full valid config passes; overriding only A2A_PAYLOAD_KEY isolates the
	// mandatory-key requirement (the seal for Yandex connect cookies over NATS).
	minTestEnv(t)
	setValidLegal(t)
	t.Setenv("A2A_PAYLOAD_KEY", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded without A2A_PAYLOAD_KEY; want error")
	}
	t.Setenv("A2A_PAYLOAD_KEY", "too-short")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with a short A2A_PAYLOAD_KEY; want error")
	}
}
