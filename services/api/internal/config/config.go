package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/crypto"

	"github.com/f1xgun/onevoice/services/api/internal/auth"
)

// Configuration constants. These back default values for environment-driven
// knobs and validation thresholds. Named here (rather than inlined) so the
// linter doesn't flag them as magic numbers and so callers see semantic intent.
//
// Cryptographic key lengths are sourced from their owning packages:
//   - JWT secret minimum: services/api/internal/auth.JWTSecretMinLen
//   - Encryption key length: pkg/crypto.AES256KeyLen
//
// keeping a single source of truth across the API service.
const (
	// HTTP server timeout defaults.
	defaultHTTPReadTimeout       = 15 * time.Second
	defaultHTTPReadHeaderTimeout = 10 * time.Second
	defaultHTTPIdleTimeout       = 60 * time.Second
	defaultOrchestratorFetchTO   = 10 * time.Second

	// Lifecycle.
	defaultShutdownTimeout = 30 * time.Second

	// String literal used for env-var "true" comparisons.
	envBoolTrue = "true"
)

// Default endpoint URLs for env-driven config. These are dev-mode
// fallbacks; production deployments must set the corresponding env vars.
const (
	defaultVKRedirectURI     = "http://localhost/api/v1/oauth/vk/callback"
	defaultYandexRedirectURI = "http://localhost/api/v1/oauth/yandex_business/callback"
	defaultGoogleRedirectURI = "http://localhost/api/v1/oauth/google_business/callback"
	defaultOrchestratorURL   = "http://localhost:8090"
	defaultPublicURL         = "http://localhost:8080"
	defaultCORSDevOrigin     = "http://localhost:3000"
)

// SelfHostedEndpoint holds configuration for one self-hosted LLM inference
// endpoint. Lifted verbatim from
// services/orchestrator/internal/config/config.go so the API-side titler
// reuses the same wiring shape.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
}

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

	// OAuth credentials
	VKClientID     string
	VKClientSecret string
	VKRedirectURI  string
	// VKServiceKey is the service access token from the VK Mini-App that
	// backs wall.getComments / groups.getById. It's intentionally separate
	// from VKClientID (a VK ID app used only for user auth).
	VKServiceKey       string
	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string
	TelegramBotToken   string

	// Google OAuth
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	// Internal server
	InternalPort string

	// Orchestrator
	OrchestratorURL string

	// NATS (optional — review sync is disabled if empty)
	NATSUrl string

	// Review sync
	ReviewSyncInterval int // minutes, 0 = disabled

	// AI review-draft generation. When ReviewDraftEnabled is true, every
	// successful sync pass triggers an LLM-backed pass over pending reviews
	// without a draft. Disabled by default to avoid silent LLM spend on
	// upgrade. ReviewDraftMaxExamples caps the few-shot context window;
	// ReviewDraftBatchLimit caps how many drafts one sync pass produces.
	ReviewDraftEnabled     bool
	ReviewDraftMaxExamples int
	ReviewDraftBatchLimit  int

	// Object storage (MinIO / S3) for user uploads
	S3Endpoint        string
	S3AccessKey       string
	S3SecretKey       string
	S3Bucket          string
	S3UseSSL          bool
	S3PublicURLPrefix string // prefix used in client-facing URLs, e.g. "/media"

	PublicURL string

	// CORS — comma-separated list of allowed origins, parsed from
	// CORS_ALLOWED_ORIGINS. Defaults to a single localhost:3000 entry for
	// dev parity. In production this MUST be set to the public frontend
	// origin (e.g. https://app.example.com); a missing env var leaves the
	// API reachable only from localhost.
	CORSAllowedOrigins []string

	// HTTP server / orchestrator timeouts. All optional; defaults preserve
	// the values that were hardcoded in services/api/cmd/main.go. Knob purpose:
	//   HTTPReadTimeout       — http.Server.ReadTimeout for the public API.
	//   HTTPReadHeaderTimeout — http.Server.ReadHeaderTimeout for the internal mTLS server.
	//   HTTPIdleTimeout       — http.Server.IdleTimeout for keepalive sockets.
	//   OrchestratorFetchTimeout — per-request budget on /internal/tools/* and
	//                              token-refresh fan-out toward Google/etc.
	HTTPReadTimeout          time.Duration
	HTTPReadHeaderTimeout    time.Duration
	HTTPIdleTimeout          time.Duration
	OrchestratorFetchTimeout time.Duration

	// Per-endpoint per-minute request budgets. Defaults match the values
	// previously hardcoded in services/api/internal/router/router.go.
	// Operators tune these via RATE_LIMIT_* env vars when the service sees
	// abnormal traffic shape (e.g., a customer integration polling /chat).
	RateLimitRegister int
	RateLimitLogin    int
	RateLimitChat     int
	RateLimitHITL     int
	RateLimitConsents int // Phase 22 T-22-08 — per-minute budget for /auth/consents + /users/me/consents/pdn/withdraw

	// Shutdown
	ShutdownTimeout time.Duration

	// Phase 21a — transactional email infrastructure.
	// UnisenderAPIKey: empty = NoopSender (dev/local); set = UnisenderSender.
	// Operators: see docs/runbook-email-dns.md for the DKIM/SPF/DMARC
	// pre-req that gates production sends.
	UnisenderAPIKey    string
	UnisenderFromEmail string        // default "noreply@onevoice.app"
	UnisenderFromName  string        // default "OneVoice"
	OutboxPollInterval time.Duration // default 5s
	OutboxMaxAttempts  int           // default 5

	// Phase 22 — Legal entity (152-ФЗ Art. 14 data controller). D-19, D-20.
	// When any of these is a placeholder, /legal/* renders fallback copy
	// and the footer emits console.warn (D-22 — must not crash). Phase
	// 22-03 launch checklist (D-29) verifies non-placeholder values in
	// production. Frontend reads the mirrored NEXT_PUBLIC_LEGAL_* vars
	// (see .env.example) so the data-controller block renders SSR-safe.
	LegalEntityName string
	LegalINN        string
	LegalAddress    string
	LegalEmailPDN   string

	// HealthCheckTimeout caps any single dep ping inside /health/ready.
	// Checks run concurrently (sync.WaitGroup in pkg/health.ReadyHandler),
	// so total wall-clock budget = HealthCheckTimeout (max, not Σ deps ×
	// timeout). Default 2s preserves k8s readinessProbe (5s default) headroom.
	HealthCheckTimeout time.Duration

	// Lockout + SmartCaptcha + trusted-proxy knobs.
	//
	// LockoutFailThresholdCaptcha — counter at which TierCaptcha kicks in.
	// LockoutFailThresholdLock    — counter at which TierLocked kicks in.
	// LockoutDuration             — Redis TTL and lock window (also the
	//                               retry_after_seconds value on 423 responses).
	// SmartCaptchaSiteKey         — public key for the JS widget; exposed to
	//                               the frontend via NEXT_PUBLIC_SMARTCAPTCHA_SITE_KEY.
	// SmartCaptchaSecretKey       — server-side validation secret. Empty = Noop
	//                               verifier (captcha disabled).
	// TrustedProxyCIDRs           — comma-separated CIDR list controlling which
	//                               X-Forwarded-For sources are trusted.
	//                               Empty falls back to Yandex Cloud LB defaults.
	// SmartCaptchaFailOpen        — on ErrCaptchaTransient (Yandex unreachable):
	//                               true → log+proceed (safer default);
	//                               false → reject as 403.
	LockoutFailThresholdCaptcha int
	LockoutFailThresholdLock    int
	LockoutDuration             time.Duration
	SmartCaptchaSiteKey         string
	SmartCaptchaSecretKey       string
	TrustedProxyCIDRs           string
	SmartCaptchaFailOpen        bool

	// Auto-titler. TitlerModel falls back to LLMModel when unset;
	// when both are unset the titler is disabled (graceful no-op —
	// API must boot cleanly without any LLM env).
	LLMModel    string
	LLMTier     string
	TitlerModel string

	// LLM provider API keys. Lifted verbatim from
	// services/orchestrator/internal/config/config.go so the API-side
	// titler Router constructs over the same provider set as the orchestrator.
	// At least one must be set when TitlerModel != "" — otherwise the titler
	// is left disabled (graceful no-op) and the trigger gate becomes a
	// no-op. The API service itself does NOT fail-fast on missing keys
	// (different from orchestrator, which requires LLM_MODEL).
	OpenRouterAPIKey    string
	OpenAIAPIKey        string
	AnthropicAPIKey     string
	SelfHostedEndpoints []SelfHostedEndpoint
}

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
		RateLimitConsents: getEnvInt("RATE_LIMIT_CONSENTS", 10), // Phase 22 T-22-08; 10/min/user is generous (genuine retry budget, blocks UPSERT thrash)

		ShutdownTimeout: shutdownTimeout,

		// Phase 21a — transactional email infrastructure.
		UnisenderAPIKey:    os.Getenv("UNISENDER_API_KEY"),
		UnisenderFromEmail: getEnv("UNISENDER_FROM_EMAIL", "noreply@onevoice.app"),
		UnisenderFromName:  getEnv("UNISENDER_FROM_NAME", "OneVoice"),
		OutboxPollInterval: getEnvDuration("OUTBOX_POLL_INTERVAL", 5*time.Second), //nolint:mnd // env-driven default
		OutboxMaxAttempts:  getEnvInt("OUTBOX_MAX_ATTEMPTS", 5),                   //nolint:mnd // env-driven default

		// Phase 22 — Legal entity (152-ФЗ Art. 14 data controller). D-19,
		// D-20, D-21, D-22. Defaults render the «[Юридическое лицо — будет
		// обновлено]» / «—» stubs so the API boots without operator
		// configuration; pre-launch checklist (D-29) catches placeholders.
		LegalEntityName: getEnv("LEGAL_ENTITY_NAME", "[Юридическое лицо — будет обновлено]"),
		LegalINN:        os.Getenv("LEGAL_INN"),
		LegalAddress:    os.Getenv("LEGAL_ADDRESS"),
		LegalEmailPDN:   getEnv("LEGAL_EMAIL_PDN", "—"),

		// Per-dep readiness check timeout. Never trust the operator's bytes;
		// getEnvDuration already falls back on parse errors, and a zero or
		// negative explicit value would be rejected by the defensive clamp
		// below.
		HealthCheckTimeout: getEnvDuration("HEALTH_CHECK_TIMEOUT", 2*time.Second),
	}

	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = 2 * time.Second
	}

	// Lockout + SmartCaptcha + trusted-proxy env loading. Defaults match
	// pkg/lockout.Default* constants so they stay in sync; the clamp below
	// also defends against operator typos (negative threshold etc.).
	const (
		defaultLockoutCaptcha  = 4
		defaultLockoutLock     = 10
		defaultLockoutDuration = 15 * time.Minute
	)
	cfg.LockoutFailThresholdCaptcha = getEnvInt("LOCKOUT_FAIL_THRESHOLD_CAPTCHA", defaultLockoutCaptcha)
	cfg.LockoutFailThresholdLock = getEnvInt("LOCKOUT_FAIL_THRESHOLD_LOCK", defaultLockoutLock)
	cfg.LockoutDuration = getEnvDuration("LOCKOUT_DURATION", defaultLockoutDuration)
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
	// SMARTCAPTCHA_FAIL_OPEN defaults to "true" — fail-open during Yandex
	// outages so legitimate users keep logging in.
	cfg.SmartCaptchaFailOpen = getEnv("SMARTCAPTCHA_FAIL_OPEN", envBoolTrue) == envBoolTrue

	// Auto-titler env loading. Mirrors
	// services/orchestrator/internal/config/config.go but does NOT fail-fast
	// on missing LLMModel — graceful disable mandated so the API service
	// boots in dev environments with no LLM env configured at all.
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

	// Validate required fields
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

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return defaultValue
}

// getEnvDuration parses a Go-style duration env var (e.g. "30s", "5m").
// Invalid or missing values fall back to the supplied default so an
// operator typo can't crash startup of an otherwise-healthy service.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

// getEnvSlice parses a comma-separated env var into a trimmed []string.
// Empty entries (e.g. "a,,b") are dropped; a fully blank value falls back
// to the supplied default (typically a dev-friendly localhost entry).
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

// parseIndexedEndpoints scans SELF_HOSTED_N_URL / _MODEL / _API_KEY env vars
// for N = 0, 1, 2, … stopping when SELF_HOSTED_N_URL is missing.
// Entries without MODEL are skipped.
//
// Lifted verbatim from
// services/orchestrator/internal/config/config.go so byte-identical
// semantics apply on the API side.
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
