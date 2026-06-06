// Package config loads the API service's env-driven configuration.
//
// See docs/api/config.md for the full field reference, env var defaults,
// ranges, and fail-loud parsing policy.
package config

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/crypto"

	"github.com/f1xgun/onevoice/services/api/internal/auth"
)

// HTTP server + orchestrator timeout defaults.
const (
	defaultHTTPReadTimeout       = 15 * time.Second
	defaultHTTPReadHeaderTimeout = 10 * time.Second
	defaultHTTPIdleTimeout       = 60 * time.Second
	defaultOrchestratorFetchTO   = 10 * time.Second
)

// Lifecycle + misc defaults.
const (
	defaultShutdownTimeout = 30 * time.Second
	envBoolTrue            = "true"
	defaultSSEMaxPerUser   = 3
)

// PostgreSQL pool sizing defaults. Sized for free-beta single-pod /
// ~10-20 concurrent chats; operators raise via PG_* env at scale.
const (
	defaultPGMaxConns              = 25
	defaultPGMinConns              = 2
	defaultPGMaxConnLifetime       = 30 * time.Minute
	defaultPGMaxConnIdleTime       = 15 * time.Minute
	defaultPGHealthCheckPeriod     = 1 * time.Minute
	defaultPGMaxConnLifetimeJitter = 3 * time.Minute
)

// Default endpoint URLs — dev-mode fallbacks; production deployments must
// set the corresponding env vars.
const (
	defaultVKRedirectURI     = "http://localhost/api/v1/oauth/vk/callback"
	defaultYandexRedirectURI = "http://localhost/api/v1/oauth/yandex_business/callback"
	defaultGoogleRedirectURI = "http://localhost/api/v1/oauth/google_business/callback"
	defaultOrchestratorURL   = "http://localhost:8090"
	defaultPublicURL         = "http://localhost:8080"
	defaultCORSDevOrigin     = "http://localhost:3000"
)

// SelfHostedEndpoint holds configuration for one self-hosted LLM inference
// endpoint. Mirrors services/orchestrator/internal/config/config.go.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
}

// Config is the API service's runtime configuration.
type Config struct {
	Port          string
	PostgresHost  string
	PostgresPort  string
	PostgresUser  string
	PostgresPass  string
	PostgresDB    string
	MongoURI      string
	MongoDB       string
	RedisHost     string
	RedisPort     string
	JWTSecret     string
	EncryptionKey string
	SecureCookies bool

	VKClientID     string
	VKClientSecret string
	VKRedirectURI  string
	// VKServiceKey backs wall.getComments / groups.getById. Intentionally
	// separate from VKClientID (the VK ID app used only for user auth).
	VKServiceKey       string
	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string
	TelegramBotToken   string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	InternalPort string

	OrchestratorURL string

	NATSUrl string // empty disables review sync

	ReviewSyncInterval int // minutes, 0 = disabled

	ReviewDraftEnabled     bool
	ReviewDraftMaxExamples int
	ReviewDraftBatchLimit  int

	S3Endpoint        string
	S3AccessKey       string
	S3SecretKey       string
	S3Bucket          string
	S3UseSSL          bool
	S3PublicURLPrefix string

	PublicURL string

	CORSAllowedOrigins []string

	HTTPReadTimeout          time.Duration
	HTTPReadHeaderTimeout    time.Duration
	HTTPIdleTimeout          time.Duration
	OrchestratorFetchTimeout time.Duration

	RateLimitRegister int
	RateLimitLogin    int
	RateLimitChat     int
	RateLimitHITL     int
	RateLimitConsents int

	ShutdownTimeout time.Duration

	UnisenderAPIKey    string
	UnisenderFromEmail string
	UnisenderFromName  string
	OutboxPollInterval time.Duration
	OutboxMaxAttempts  int

	// Legal entity (152-ФЗ Art. 14 data controller). Placeholder defaults
	// render fallback /legal/* copy; pre-launch checklist verifies real values.
	LegalEntityName string
	LegalINN        string
	LegalAddress    string
	LegalEmailPDN   string

	HealthCheckTimeout time.Duration

	LockoutFailThresholdCaptcha int
	LockoutFailThresholdLock    int
	LockoutDuration             time.Duration
	SmartCaptchaSiteKey         string
	SmartCaptchaSecretKey       string
	TrustedProxyCIDRs           string
	SmartCaptchaFailOpen        bool

	LLMModel    string
	LLMTier     string
	TitlerModel string

	OpenRouterAPIKey    string
	OpenAIAPIKey        string
	AnthropicAPIKey     string
	SelfHostedEndpoints []SelfHostedEndpoint

	FreeTierDailySpendUSD        float64
	RedisDownPolicy              string
	LocalFallbackRequestsPerHour int

	SSEMaxPerUser int

	PGMaxConns              int
	PGMinConns              int
	PGMaxConnLifetime       time.Duration
	PGMaxConnIdleTime       time.Duration
	PGHealthCheckPeriod     time.Duration
	PGMaxConnLifetimeJitter time.Duration
}

// Load reads env vars and returns a validated *Config or a fail-loud error.
func Load() (*Config, error) {
	shutdownTimeout := defaultShutdownTimeout
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			shutdownTimeout = d
		}
	}

	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		PostgresHost:  getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:  getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:  getEnv("POSTGRES_USER", "postgres"),
		PostgresPass:  getEnv("POSTGRES_PASSWORD", ""),
		PostgresDB:    getEnv("POSTGRES_DB", "onevoice"),
		MongoURI:      getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:       getEnv("MONGO_DB", "onevoice"),
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		EncryptionKey: getEnv("ENCRYPTION_KEY", ""),
		SecureCookies: getEnv("SECURE_COOKIES", envBoolTrue) == envBoolTrue,

		VKClientID:         os.Getenv("VK_CLIENT_ID"),
		VKClientSecret:     os.Getenv("VK_CLIENT_SECRET"),
		VKRedirectURI:      getEnv("VK_REDIRECT_URI", defaultVKRedirectURI),
		VKServiceKey:       os.Getenv("VK_SERVICE_KEY"),
		YandexClientID:     os.Getenv("YANDEX_CLIENT_ID"),
		YandexClientSecret: os.Getenv("YANDEX_CLIENT_SECRET"),
		YandexRedirectURI:  getEnv("YANDEX_REDIRECT_URI", defaultYandexRedirectURI),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURI:  getEnv("GOOGLE_REDIRECT_URI", defaultGoogleRedirectURI),
		InternalPort:       getEnv("INTERNAL_PORT", "8443"),
		OrchestratorURL:    getEnv("ORCHESTRATOR_URL", defaultOrchestratorURL),
		NATSUrl:            os.Getenv("NATS_URL"),
		ReviewSyncInterval: getEnvInt("REVIEW_SYNC_INTERVAL_MINUTES", 30), //nolint:mnd // env-driven default

		ReviewDraftEnabled:     getEnv("REVIEW_DRAFT_ENABLED", "false") == envBoolTrue,
		ReviewDraftMaxExamples: getEnvInt("REVIEW_DRAFT_MAX_EXAMPLES", 5), //nolint:mnd // env-driven default
		ReviewDraftBatchLimit:  getEnvInt("REVIEW_DRAFT_BATCH_LIMIT", 10),

		S3Endpoint:        getEnv("S3_ENDPOINT", "minio:9000"),
		S3AccessKey:       getEnv("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:       getEnv("S3_SECRET_KEY", "minioadmin"),
		S3Bucket:          getEnv("S3_BUCKET", "onevoice"),
		S3UseSSL:          getEnv("S3_USE_SSL", "false") == envBoolTrue,
		S3PublicURLPrefix: getEnv("S3_PUBLIC_URL_PREFIX", "/media"),

		PublicURL:          getEnv("PUBLIC_URL", defaultPublicURL),
		CORSAllowedOrigins: getEnvSlice("CORS_ALLOWED_ORIGINS", []string{defaultCORSDevOrigin}),

		HTTPReadTimeout:          getEnvDuration("HTTP_READ_TIMEOUT", defaultHTTPReadTimeout),
		HTTPReadHeaderTimeout:    getEnvDuration("HTTP_READ_HEADER_TIMEOUT", defaultHTTPReadHeaderTimeout),
		HTTPIdleTimeout:          getEnvDuration("HTTP_IDLE_TIMEOUT", defaultHTTPIdleTimeout),
		OrchestratorFetchTimeout: getEnvDuration("ORCHESTRATOR_FETCH_TIMEOUT", defaultOrchestratorFetchTO),

		RateLimitRegister: getEnvInt("RATE_LIMIT_REGISTER", 5), //nolint:mnd // env-driven default
		RateLimitLogin:    getEnvInt("RATE_LIMIT_LOGIN", 10),
		RateLimitChat:     getEnvInt("RATE_LIMIT_CHAT", 10),
		RateLimitHITL:     getEnvInt("RATE_LIMIT_HITL", 10),
		RateLimitConsents: getEnvInt("RATE_LIMIT_CONSENTS", 10),

		ShutdownTimeout: shutdownTimeout,

		UnisenderAPIKey:    os.Getenv("UNISENDER_API_KEY"),
		UnisenderFromEmail: getEnv("UNISENDER_FROM_EMAIL", "noreply@onevoice.app"),
		UnisenderFromName:  getEnv("UNISENDER_FROM_NAME", "OneVoice"),
		OutboxPollInterval: getEnvDuration("OUTBOX_POLL_INTERVAL", 5*time.Second), //nolint:mnd // env-driven default
		OutboxMaxAttempts:  getEnvInt("OUTBOX_MAX_ATTEMPTS", 5),                   //nolint:mnd // env-driven default

		// No placeholder defaults: production boot is gated by
		// validateLegalProduction; non-production accepts empty strings
		// with a slog.Warn so dev / CI still boot without operator config.
		LegalEntityName: os.Getenv("LEGAL_ENTITY_NAME"),
		LegalINN:        os.Getenv("LEGAL_INN"),
		LegalAddress:    os.Getenv("LEGAL_ADDRESS"),
		LegalEmailPDN:   os.Getenv("LEGAL_EMAIL_PDN"),

		HealthCheckTimeout: getEnvDuration("HEALTH_CHECK_TIMEOUT", 2*time.Second),
	}

	// defensive clamp — getEnvDuration falls back on parse errors; a zero or
	// negative explicit value would slip past otherwise.
	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = 2 * time.Second
	}

	// defaults match pkg/lockout.Default* constants so they stay in sync.
	const (
		defaultLockoutCaptcha  = 4
		defaultLockoutLock     = 10
		defaultLockoutDuration = 15 * time.Minute
	)
	cfg.LockoutFailThresholdCaptcha = getEnvInt("LOCKOUT_FAIL_THRESHOLD_CAPTCHA", defaultLockoutCaptcha)
	cfg.LockoutFailThresholdLock = getEnvInt("LOCKOUT_FAIL_THRESHOLD_LOCK", defaultLockoutLock)
	cfg.LockoutDuration = getEnvDuration("LOCKOUT_DURATION", defaultLockoutDuration)
	// defend against operator typo (negative threshold etc.)
	if cfg.LockoutFailThresholdCaptcha <= 0 {
		cfg.LockoutFailThresholdCaptcha = defaultLockoutCaptcha
	}
	if cfg.LockoutFailThresholdLock <= 0 {
		cfg.LockoutFailThresholdLock = defaultLockoutLock
	}
	if cfg.LockoutDuration <= 0 {
		cfg.LockoutDuration = defaultLockoutDuration
	}
	cfg.SmartCaptchaSiteKey = os.Getenv("SMARTCAPTCHA_SITE_KEY")
	cfg.SmartCaptchaSecretKey = os.Getenv("SMARTCAPTCHA_SECRET_KEY")
	cfg.TrustedProxyCIDRs = os.Getenv("TRUSTED_PROXY_CIDRS")
	// fail-open during Yandex outages so legitimate users keep logging in
	cfg.SmartCaptchaFailOpen = getEnv("SMARTCAPTCHA_FAIL_OPEN", envBoolTrue) == envBoolTrue

	// Auto-titler: API does NOT fail-fast on missing LLMModel (different
	// from orchestrator) — graceful disable so the API boots in dev with no
	// LLM env at all.
	cfg.LLMModel = os.Getenv("LLM_MODEL")
	cfg.LLMTier = os.Getenv("LLM_TIER")
	if cfg.LLMTier == "" {
		cfg.LLMTier = "free"
	}
	cfg.TitlerModel = os.Getenv("TITLER_MODEL")
	if cfg.TitlerModel == "" {
		cfg.TitlerModel = cfg.LLMModel // graceful fallback
	}
	cfg.OpenRouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	cfg.SelfHostedEndpoints = parseIndexedEndpoints()

	if v := os.Getenv("LLM_FREE_TIER_DAILY_SPEND_USD"); v != "" {
		if f, perr := strconv.ParseFloat(v, 64); perr == nil {
			cfg.FreeTierDailySpendUSD = f
		}
	}
	cfg.RedisDownPolicy = getEnv("LLM_RATELIMIT_ON_REDIS_DOWN", "block")
	switch cfg.RedisDownPolicy {
	case "block", "local_fallback":
	default:
		return nil, fmt.Errorf("LLM_RATELIMIT_ON_REDIS_DOWN must be \"block\" or \"local_fallback\", got %q", cfg.RedisDownPolicy)
	}
	cfg.LocalFallbackRequestsPerHour = 2000
	if v := os.Getenv("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR"); v != "" {
		// strconv.Atoi fails loud — non-integer is misconfiguration
		n, perr := strconv.Atoi(v)
		if perr != nil {
			return nil, fmt.Errorf("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR must be a positive integer, got %q: %w", v, perr)
		}
		cfg.LocalFallbackRequestsPerHour = n
	}
	if cfg.RedisDownPolicy == "local_fallback" && cfg.LocalFallbackRequestsPerHour <= 0 {
		return nil, fmt.Errorf("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR must be > 0 when LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback")
	}

	// SSE_MAX_PER_USER: fail loud on non-integer — silent default coercion
	// has bitten cost-guard wiring before. 0 disables the gate entirely.
	cfg.SSEMaxPerUser = defaultSSEMaxPerUser
	if v := os.Getenv("SSE_MAX_PER_USER"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil {
			return nil, fmt.Errorf("SSE_MAX_PER_USER must be a non-negative integer, got %q: %w", v, perr)
		}
		if n < 0 {
			return nil, fmt.Errorf("SSE_MAX_PER_USER must be >= 0, got %d", n)
		}
		cfg.SSEMaxPerUser = n
	}

	// PG_* parse + validate fail loud — silent default coercion on a typo
	// would silently hide pool starvation in production.
	pgMaxConns, err := parseIntEnv("PG_MAX_CONNS", defaultPGMaxConns)
	if err != nil {
		return nil, err
	}
	cfg.PGMaxConns = pgMaxConns

	pgMinConns, err := parseIntEnv("PG_MIN_CONNS", defaultPGMinConns)
	if err != nil {
		return nil, err
	}
	cfg.PGMinConns = pgMinConns

	pgMaxConnLifetime, err := parseDurationEnv("PG_MAX_CONN_LIFETIME", defaultPGMaxConnLifetime)
	if err != nil {
		return nil, err
	}
	cfg.PGMaxConnLifetime = pgMaxConnLifetime

	pgMaxConnIdleTime, err := parseDurationEnv("PG_MAX_CONN_IDLE_TIME", defaultPGMaxConnIdleTime)
	if err != nil {
		return nil, err
	}
	cfg.PGMaxConnIdleTime = pgMaxConnIdleTime

	pgHealthCheckPeriod, err := parseDurationEnv("PG_HEALTH_CHECK_PERIOD", defaultPGHealthCheckPeriod)
	if err != nil {
		return nil, err
	}
	cfg.PGHealthCheckPeriod = pgHealthCheckPeriod

	pgMaxConnLifetimeJitter, err := parseDurationEnv("PG_MAX_CONN_LIFETIME_JITTER", defaultPGMaxConnLifetimeJitter)
	if err != nil {
		return nil, err
	}
	cfg.PGMaxConnLifetimeJitter = pgMaxConnLifetimeJitter

	if cfg.PGMaxConns <= 0 {
		return nil, fmt.Errorf("PG_MAX_CONNS must be > 0, got %d", cfg.PGMaxConns)
	}
	// upper bound matches pgxpool.Config.MaxConns (int32) — bounding here
	// lets wire/databases.go convert without a gosec G115 false positive.
	if cfg.PGMaxConns > math.MaxInt32 {
		return nil, fmt.Errorf("PG_MAX_CONNS must be <= %d, got %d", math.MaxInt32, cfg.PGMaxConns)
	}
	if cfg.PGMinConns < 0 {
		return nil, fmt.Errorf("PG_MIN_CONNS must be >= 0, got %d", cfg.PGMinConns)
	}
	if cfg.PGMinConns > cfg.PGMaxConns {
		return nil, fmt.Errorf("PG_MIN_CONNS=%d must be <= PG_MAX_CONNS=%d", cfg.PGMinConns, cfg.PGMaxConns)
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < auth.JWTSecretMinLen {
		return nil, fmt.Errorf("JWT_SECRET must be at least %d characters", auth.JWTSecretMinLen)
	}
	if cfg.EncryptionKey == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required")
	}
	if len(cfg.EncryptionKey) != crypto.AES256KeyLen {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be exactly %d bytes", crypto.AES256KeyLen)
	}
	if err := validateEncryptionKey(cfg.EncryptionKey); err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY validation failed: %w (generate a new key with: openssl rand -base64 24)", err)
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		if err := validateLegalProduction(cfg); err != nil {
			return nil, fmt.Errorf("LEGAL_* validation failed in production: %w", err)
		}
	} else if err := validateLegalProduction(cfg); err != nil {
		slog.Warn("LEGAL_* placeholders present (allowed in non-production)", "issue", err.Error())
	}

	return cfg, nil
}

// getEnv returns the env var or defaultValue when unset / empty.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt returns the env var parsed as int or defaultValue on miss /
// parse error (silent fallback).
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return defaultValue
}

// getEnvDuration returns the env var parsed as Go duration or defaultValue
// on miss / parse error (silent fallback so a typo can't crash startup of
// an otherwise-healthy service).
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

// getEnvSlice splits a comma-separated env var into a trimmed []string.
// Empty entries are dropped; a fully blank value returns defaultValue.
func getEnvSlice(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return defaultValue
	}
	return out
}

// parseIndexedEndpoints scans SELF_HOSTED_N_URL / _MODEL / _API_KEY env
// vars for N = 0, 1, 2, … stopping when SELF_HOSTED_N_URL is missing.
// Entries without MODEL are skipped. Mirrors orchestrator config.
func parseIndexedEndpoints() []SelfHostedEndpoint {
	var result []SelfHostedEndpoint
	for i := 0; ; i++ {
		prefix := fmt.Sprintf("SELF_HOSTED_%d_", i)
		url := os.Getenv(prefix + "URL")
		if url == "" {
			break
		}
		model := os.Getenv(prefix + "MODEL")
		if model == "" {
			continue
		}
		result = append(result, SelfHostedEndpoint{
			URL:    url,
			Model:  model,
			APIKey: os.Getenv(prefix + "API_KEY"),
		})
	}
	return result
}

// parseIntEnv reads an integer env var with fail-loud parsing. Empty
// returns the default; non-empty must parse or Load() aborts boot.
func parseIntEnv(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	// strconv.Atoi fails loud — non-integer is misconfiguration
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q: %w", key, value, err)
	}
	return n, nil
}

// parseDurationEnv reads a Go-duration env var with fail-loud parsing.
// Empty returns the default; non-empty must parse via time.ParseDuration or
// Load() aborts boot.
func parseDurationEnv(key string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	// time.ParseDuration fails loud — bad duration is misconfiguration
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration, got %q: %w", key, value, err)
	}
	return d, nil
}
