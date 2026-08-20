// Package config loads the API service's env-driven configuration.
//
// See docs/api/config.md for the full field reference, env var defaults,
// ranges, and fail-loud parsing policy.
package config

import (
	"encoding/json"
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

// Chat-history fetch limit defaults. The number of prior messages loaded into
// the LLM context for a chat turn. maxMessageHistoryLimit caps the operator
// override so an over-large value can't blow up the prompt size / per-turn cost.
const (
	defaultMessageHistoryLimit = 100
	maxMessageHistoryLimit     = 500
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
	// PublicURL is the user-facing reverse-proxy origin (nginx): it serves the
	// frontend at /, the API under /api/v1, and uploads under /media. It is the
	// base for links emailed to users (verify-email, password-reset) — which
	// are FRONTEND routes — so it must NOT point at the API's :8080 port.
	defaultPublicURL     = "http://localhost"
	defaultCORSDevOrigin = "http://localhost:3000"
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
	// AppEnv is the deployment environment ("production" enables fail-closed
	// gates; any other value, including empty, is treated as dev/non-prod).
	// Compare via IsProduction rather than reading the field directly.
	AppEnv       string
	Port         string
	PostgresHost string
	PostgresPort string
	PostgresUser string
	PostgresPass string
	PostgresDB   string
	MongoURI     string
	MongoDB      string
	RedisHost    string
	RedisPort    string
	// RedisPassword authenticates the Redis connection (requirepass). Empty
	// keeps the connection unauthenticated for local dev.
	RedisPassword string
	JWTSecret     string
	EncryptionKey string
	// A2APayloadKey is an optional 32-byte AES-256 key used to encrypt secret
	// A2A tool arguments (currently the Yandex connect cookies) before they
	// cross the NATS bus. Empty leaves those arguments in plaintext (dev). It
	// MUST match A2A_PAYLOAD_KEY on the receiving agent.
	A2APayloadKey string
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
	// YandexRepLogin is the shared representative Yandex ID shown to owners so
	// they know which account to add as a представитель in Yandex Business →
	// Доступы. Empty disables the delegated-representative access endpoints
	// fail-closed (they return "delegated access not configured").
	YandexRepLogin string
	// YandexSharedBusinessID is the config-pinned sentinel business UUID under
	// which the single KMS-wrapped shared representative session cookie JSON is
	// stored (external_id "__shared_rep__"). Empty disables the delegated
	// endpoints fail-closed. Must match the agent's YANDEX_SHARED_BUSINESS_ID.
	YandexSharedBusinessID string
	TelegramBotToken       string
	// TelegramApprovalHMACSecret signs/verifies the opaque callback_data on the
	// inline [Approve]/[Reject] buttons of an owner HITL approval notification.
	// It MUST match the telegram agent's TELEGRAM_APPROVAL_HMAC_SECRET. Empty
	// disables the inbound approval plane fail-closed: the consumer refuses to
	// subscribe, so absence never exposes an unvalidated off-app approval path.
	TelegramApprovalHMACSecret string

	// TelegramBotUsername is the @-less username of the system bot (e.g.
	// "onevoice_bot"). It is used only to render the /start owner-link deep link
	// https://t.me/<username>?start=<token>. Empty disables the owner-link
	// handshake fail-closed: the mint endpoint returns 404 and the bind consumer
	// refuses to subscribe. Read from config rather than a getMe call so the hot
	// mint path never makes a runtime Bot API request.
	TelegramBotUsername string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	InternalPort string

	// InternalACL is the declarative CN→[]platforms map enforced on
	// /internal/v1/tokens. Each trusted client cert CommonName maps to the
	// platforms it may request; a "*" entry grants any platform. A CN absent
	// from the map is fail-closed (403). Loaded from ONEVOICE_INTERNAL_ACL_JSON
	// at boot — missing or malformed JSON aborts startup.
	InternalACL map[string][]string

	OrchestratorURL string

	NATSUrl string // empty disables review sync

	ReviewSyncInterval int // minutes, 0 = disabled

	ReviewDraftEnabled     bool
	ReviewDraftMaxExamples int
	ReviewDraftBatchLimit  int

	// SyncReconcileEnabled gates the proactive platform-sync reconciler worker.
	// Ships DARK (false): the sync_state table + drift/verify endpoints exist
	// regardless, but the background reconcile loop is only started when this is
	// explicitly enabled, so the default deploy carries zero extra polling load.
	SyncReconcileEnabled bool
	// SyncReconcilePollInterval is how often the reconciler wakes to check for
	// due channels. Per-channel cadence (24h free / 6h paid) is enforced via each
	// row's next_check_at, so this is only the poll granularity, not the re-check
	// frequency.
	SyncReconcilePollInterval time.Duration

	// CreditGrantEnabled gates the monthly credit-grant worker that lands each
	// business's plan allowance into credit_ledger. Defaults ON: without a grant
	// the ledger has no `grant` rows, so every business reads a 0 balance and the
	// billing UI shows remaining=0 for everyone. The grant is idempotent per
	// (business, period) and changes no gating (nothing charges credits yet — the
	// meter only records overage and never blocks a turn), so enabling it is a
	// pure balance-visibility fix with no downside.
	CreditGrantEnabled bool
	// CreditGrantPollInterval is how often the grant worker re-checks the fleet.
	// The grant is idempotent per period, so a poll is cheap (an EXISTS pre-check
	// per business); the interval only bounds how quickly a brand-new business or
	// a new month's reset lands its allowance. Default 1h.
	CreditGrantPollInterval time.Duration

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

	RateLimitRegister  int
	RateLimitLogin     int
	RateLimitChat      int
	RateLimitHITL      int
	RateLimitConsents  int
	RateLimitTelemetry int
	// RateLimitWrites is the shared per-user/minute budget for state-changing
	// routes that trigger external work (integration connect/refresh, RPA
	// probes, review reply/refresh, business update). Generous by default;
	// caps abuse without throttling normal use.
	RateLimitWrites int
	// RateLimitInvitations is a tighter per-user/minute budget for creating
	// member invitations, which send email (amplification vector).
	RateLimitInvitations int
	// RateLimitSearch is the per-user/minute budget for the search endpoint.
	// Search fans out into regex scans over scoped messages, so an unlimited
	// loop is a DoS amplification vector; this caps it without throttling
	// normal interactive search.
	RateLimitSearch int

	ShutdownTimeout time.Duration

	UnisenderAPIKey    string
	UnisenderFromEmail string
	UnisenderFromName  string
	OutboxPollInterval time.Duration
	OutboxMaxAttempts  int

	// FeedbackNotifyEmail receives an owner-notification when a user submits
	// in-app feedback. Empty disables the notification (the feedback row is
	// still persisted).
	FeedbackNotifyEmail string

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

	// OwnerBriefEnabled gates the weekly owner-brief worker that composes and
	// dispatches a proactive weekly summary DM to each eligible business owner.
	// Defaults ON: the brief is the whole point of the feature (weekly AI-actioned
	// presence value without the owner opening the dashboard), and per-business
	// opt-out lives in businesses.settings.ownerBrief.enabled, so a fleet-wide
	// off-switch is only for an operational emergency.
	OwnerBriefEnabled bool
	// OwnerBriefPollInterval is how often the worker re-checks the fleet for due
	// briefs. The per-week idempotency stamp makes each poll cheap; the interval
	// only bounds how tightly the weekday/hour window is hit. Default 1h.
	OwnerBriefPollInterval time.Duration
	// OwnerBriefModel is the LLM model the brief composer meters through the
	// shared titler Router. Defaults to TITLER_MODEL (which itself falls back to
	// LLM_MODEL), so the cheap background model is reused unless overridden.
	OwnerBriefModel string

	// PresenceHealthSnapshotEnabled gates the weekly presence-health snapshot
	// worker that stamps each active business's composite score per ISO-week.
	// Defaults ON: the snapshot is what the read-only presence-health trend reads
	// a prior week from, and the per-week UNIQUE + upsert makes the pass cheap and
	// idempotent, so a fleet-wide off-switch is only for an operational emergency.
	PresenceHealthSnapshotEnabled bool
	// PresenceHealthSnapshotPollInterval is how often the worker re-checks the
	// fleet. The snapshot is idempotent per (business, week), so a poll only
	// bounds how promptly a fresh deploy or a new week's first point lands.
	// Default 24h.
	PresenceHealthSnapshotPollInterval time.Duration

	// ConnectionHealthEnabled gates the proactive connection-health worker that
	// re-probes each active Yandex session and DMs the owner on a fresh break.
	// Defaults ON: a silently-expired session pauses review replies and profile
	// sync, so surfacing it early is the point of the feature; the transition-only
	// nudge gate + nudged_at throttle keep it from spamming.
	ConnectionHealthEnabled bool
	// ConnectionHealthPollInterval is how often the worker re-probes the fleet.
	// The fail-soft verdict + nudge throttle make each poll cheap; the interval
	// only bounds how promptly a break is detected. Default 6h.
	ConnectionHealthPollInterval time.Duration

	OpenRouterAPIKey    string
	OpenAIAPIKey        string
	AnthropicAPIKey     string
	SelfHostedEndpoints []SelfHostedEndpoint

	FreeTierDailySpendUSD        float64
	RedisDownPolicy              string
	LocalFallbackRequestsPerHour int

	SSEMaxPerUser int

	// MessageHistoryLimit is the number of prior messages loaded into the LLM
	// context per chat turn. Defaults to 100; operators override via
	// MESSAGE_HISTORY_LIMIT (must be > 0 and <= 500).
	MessageHistoryLimit int

	PGMaxConns              int
	PGMinConns              int
	PGMaxConnLifetime       time.Duration
	PGMaxConnIdleTime       time.Duration
	PGHealthCheckPeriod     time.Duration
	PGMaxConnLifetimeJitter time.Duration

	// TokenEncryptionKMSKeyID is the Yandex KMS symmetric key resource ID. Required.
	TokenEncryptionKMSKeyID string
	// YCServiceAccountKeyJSON holds the raw JSON content of the Yandex Cloud
	// Service Account key file used to authenticate KMS API calls. Required.
	YCServiceAccountKeyJSON string
	// TokenEncryptionKMSDualDecryptCSV is an optional comma-separated list of
	// KMS version ID strings to attempt on Decrypt (rolling key rotation). Max 5.
	TokenEncryptionKMSDualDecryptCSV string
	// TokenEncryptionKMSVersionMap maps KMS version ID strings (as returned by
	// the Yandex KMS API) to the int16 values stored in the DB key_version
	// column. Parsed from TOKEN_ENCRYPTION_KMS_VERSION_MAP env var using
	// "versionA=1,versionB=2" format.
	TokenEncryptionKMSVersionMap map[string]int16
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
		AppEnv:        os.Getenv("APP_ENV"),
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
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		EncryptionKey: getEnv("ENCRYPTION_KEY", ""),
		A2APayloadKey: os.Getenv("A2A_PAYLOAD_KEY"),
		SecureCookies: getEnv("SECURE_COOKIES", envBoolTrue) == envBoolTrue,

		VKClientID:                 os.Getenv("VK_CLIENT_ID"),
		VKClientSecret:             os.Getenv("VK_CLIENT_SECRET"),
		VKRedirectURI:              getEnv("VK_REDIRECT_URI", defaultVKRedirectURI),
		VKServiceKey:               os.Getenv("VK_SERVICE_KEY"),
		YandexClientID:             os.Getenv("YANDEX_CLIENT_ID"),
		YandexClientSecret:         os.Getenv("YANDEX_CLIENT_SECRET"),
		YandexRedirectURI:          getEnv("YANDEX_REDIRECT_URI", defaultYandexRedirectURI),
		YandexRepLogin:             os.Getenv("YANDEX_REP_LOGIN"),
		YandexSharedBusinessID:     os.Getenv("YANDEX_SHARED_BUSINESS_ID"),
		TelegramBotToken:           os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramApprovalHMACSecret: os.Getenv("TELEGRAM_APPROVAL_HMAC_SECRET"),
		TelegramBotUsername:        os.Getenv("TELEGRAM_BOT_USERNAME"),
		GoogleClientID:             os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:         os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURI:          getEnv("GOOGLE_REDIRECT_URI", defaultGoogleRedirectURI),
		InternalPort:               getEnv("INTERNAL_PORT", "8443"),
		OrchestratorURL:            getEnv("ORCHESTRATOR_URL", defaultOrchestratorURL),
		NATSUrl:                    os.Getenv("NATS_URL"),
		ReviewSyncInterval:         getEnvInt("REVIEW_SYNC_INTERVAL_MINUTES", 30), //nolint:mnd // env-driven default

		ReviewDraftEnabled:     getEnv("REVIEW_DRAFT_ENABLED", "false") == envBoolTrue,
		ReviewDraftMaxExamples: getEnvInt("REVIEW_DRAFT_MAX_EXAMPLES", 5), //nolint:mnd // env-driven default
		ReviewDraftBatchLimit:  getEnvInt("REVIEW_DRAFT_BATCH_LIMIT", 10),

		SyncReconcileEnabled:      getEnv("SYNC_RECONCILE_ENABLED", "false") == envBoolTrue,
		SyncReconcilePollInterval: getEnvDuration("SYNC_RECONCILE_POLL_INTERVAL", 30*time.Minute), //nolint:mnd // env-driven default

		CreditGrantEnabled:      getEnv("CREDIT_GRANT_ENABLED", envBoolTrue) == envBoolTrue,
		CreditGrantPollInterval: getEnvDuration("CREDIT_GRANT_POLL_INTERVAL", time.Hour),

		OwnerBriefEnabled:      getEnv("OWNER_BRIEF_ENABLED", envBoolTrue) == envBoolTrue,
		OwnerBriefPollInterval: getEnvDuration("OWNER_BRIEF_POLL_INTERVAL", time.Hour),

		PresenceHealthSnapshotEnabled:      getEnv("PRESENCE_HEALTH_SNAPSHOT_ENABLED", envBoolTrue) == envBoolTrue,
		PresenceHealthSnapshotPollInterval: getEnvDuration("PRESENCE_HEALTH_SNAPSHOT_POLL_INTERVAL", 24*time.Hour), //nolint:mnd // env-driven default

		ConnectionHealthEnabled:      getEnv("CONNECTION_HEALTH_ENABLED", envBoolTrue) == envBoolTrue,
		ConnectionHealthPollInterval: getEnvDuration("CONNECTION_HEALTH_POLL_INTERVAL", 6*time.Hour), //nolint:mnd // env-driven default

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

		ShutdownTimeout: shutdownTimeout,

		UnisenderAPIKey:     os.Getenv("UNISENDER_API_KEY"),
		UnisenderFromEmail:  getEnv("UNISENDER_FROM_EMAIL", "noreply@onevoice.app"),
		UnisenderFromName:   getEnv("UNISENDER_FROM_NAME", "OneVoice"),
		FeedbackNotifyEmail: os.Getenv("FEEDBACK_NOTIFY_EMAIL"),
		OutboxPollInterval:  getEnvDuration("OUTBOX_POLL_INTERVAL", 5*time.Second), //nolint:mnd // env-driven default
		OutboxMaxAttempts:   getEnvInt("OUTBOX_MAX_ATTEMPTS", 5),                   //nolint:mnd // env-driven default

		LegalEntityName: os.Getenv("LEGAL_ENTITY_NAME"),
		LegalINN:        os.Getenv("LEGAL_INN"),
		LegalAddress:    os.Getenv("LEGAL_ADDRESS"),
		LegalEmailPDN:   os.Getenv("LEGAL_EMAIL_PDN"),

		HealthCheckTimeout: getEnvDuration("HEALTH_CHECK_TIMEOUT", 2*time.Second),
	}

	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = 2 * time.Second
	}

	if cfg.CreditGrantPollInterval <= 0 {
		cfg.CreditGrantPollInterval = time.Hour
	}

	if cfg.ConnectionHealthPollInterval <= 0 {
		cfg.ConnectionHealthPollInterval = 6 * time.Hour //nolint:mnd // documented default
	}

	if cfg.OutboxPollInterval <= 0 {
		return nil, fmt.Errorf("OUTBOX_POLL_INTERVAL must be > 0, got %v", cfg.OutboxPollInterval)
	}

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
	cfg.SmartCaptchaFailOpen = getEnv("SMARTCAPTCHA_FAIL_OPEN", envBoolTrue) == envBoolTrue

	cfg.LLMModel = os.Getenv("LLM_MODEL")
	cfg.LLMTier = os.Getenv("LLM_TIER")
	if cfg.LLMTier == "" {
		cfg.LLMTier = "free"
	}
	cfg.TitlerModel = os.Getenv("TITLER_MODEL")
	if cfg.TitlerModel == "" {
		cfg.TitlerModel = cfg.LLMModel
	}
	cfg.OwnerBriefModel = os.Getenv("OWNER_BRIEF_MODEL")
	if cfg.OwnerBriefModel == "" {
		cfg.OwnerBriefModel = cfg.TitlerModel
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
		n, perr := strconv.Atoi(v)
		if perr != nil {
			return nil, fmt.Errorf("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR must be a positive integer, got %q: %w", v, perr)
		}
		cfg.LocalFallbackRequestsPerHour = n
	}
	if cfg.RedisDownPolicy == "local_fallback" && cfg.LocalFallbackRequestsPerHour <= 0 {
		return nil, fmt.Errorf("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR must be > 0 when LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback")
	}

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

	cfg.MessageHistoryLimit = defaultMessageHistoryLimit
	if v := os.Getenv("MESSAGE_HISTORY_LIMIT"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil {
			return nil, fmt.Errorf("MESSAGE_HISTORY_LIMIT must be a positive integer, got %q: %w", v, perr)
		}
		if n <= 0 {
			return nil, fmt.Errorf("MESSAGE_HISTORY_LIMIT must be > 0, got %d", n)
		}
		if n > maxMessageHistoryLimit {
			return nil, fmt.Errorf("MESSAGE_HISTORY_LIMIT must be <= %d, got %d", maxMessageHistoryLimit, n)
		}
		cfg.MessageHistoryLimit = n
	}

	const (
		defaultRateLimitRegister    = 5
		defaultRateLimitLogin       = 10
		defaultRateLimitChat        = 10
		defaultRateLimitHITL        = 10
		defaultRateLimitConsents    = 10
		defaultRateLimitTelemetry   = 60
		defaultRateLimitWrites      = 60
		defaultRateLimitInvitations = 10
		defaultRateLimitSearch      = 30
	)
	userRateLimits := []struct {
		key    string
		def    int
		target *int
	}{
		{"RATE_LIMIT_REGISTER", defaultRateLimitRegister, &cfg.RateLimitRegister},
		{"RATE_LIMIT_LOGIN", defaultRateLimitLogin, &cfg.RateLimitLogin},
		{"RATE_LIMIT_CHAT", defaultRateLimitChat, &cfg.RateLimitChat},
		{"RATE_LIMIT_HITL", defaultRateLimitHITL, &cfg.RateLimitHITL},
		{"RATE_LIMIT_CONSENTS", defaultRateLimitConsents, &cfg.RateLimitConsents},
		{"RATE_LIMIT_TELEMETRY", defaultRateLimitTelemetry, &cfg.RateLimitTelemetry},
		{"RATE_LIMIT_WRITES", defaultRateLimitWrites, &cfg.RateLimitWrites},
		{"RATE_LIMIT_INVITATIONS", defaultRateLimitInvitations, &cfg.RateLimitInvitations},
		{"RATE_LIMIT_SEARCH", defaultRateLimitSearch, &cfg.RateLimitSearch},
	}
	for _, rl := range userRateLimits {
		n, perr := parseIntEnv(rl.key, rl.def)
		if perr != nil {
			return nil, perr
		}
		if n <= 0 {
			return nil, fmt.Errorf("%s must be > 0, got %d", rl.key, n)
		}
		*rl.target = n
	}

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
	if err := validateJWTSecret(cfg.JWTSecret); err != nil {
		return nil, fmt.Errorf("JWT_SECRET validation failed: %w (generate a new secret with: openssl rand -base64 48)", err)
	}
	// ENCRYPTION_KEY is optional in new deployments that use KMS-only encryption.
	// When set it must pass the same strength checks to prevent weak-key attacks.
	if cfg.EncryptionKey != "" {
		if len(cfg.EncryptionKey) != crypto.AES256KeyLen {
			return nil, fmt.Errorf("ENCRYPTION_KEY must be exactly %d bytes", crypto.AES256KeyLen)
		}
		if err := validateEncryptionKey(cfg.EncryptionKey); err != nil {
			return nil, fmt.Errorf("ENCRYPTION_KEY validation failed: %w (generate a new key with: openssl rand -base64 24)", err)
		}
	}
	// A2A_PAYLOAD_KEY is optional; when set it encrypts secret tool arguments
	// over NATS and must be a valid AES-256 key so a misconfigured length fails
	// loud at boot rather than at the first Yandex connect.
	if cfg.A2APayloadKey != "" && len(cfg.A2APayloadKey) != crypto.AES256KeyLen {
		return nil, fmt.Errorf("A2A_PAYLOAD_KEY must be exactly %d bytes", crypto.AES256KeyLen)
	}

	cfg.TokenEncryptionKMSKeyID = os.Getenv("TOKEN_ENCRYPTION_KMS_KEY_ID")
	if cfg.TokenEncryptionKMSKeyID == "" {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KMS_KEY_ID is required")
	}

	cfg.YCServiceAccountKeyJSON = os.Getenv("YC_SA_JSON_CREDENTIALS")
	if cfg.YCServiceAccountKeyJSON == "" {
		return nil, fmt.Errorf("YC_SA_JSON_CREDENTIALS is required")
	}

	cfg.TokenEncryptionKMSDualDecryptCSV = os.Getenv("TOKEN_ENCRYPTION_KMS_DUAL_DECRYPT_VERSIONS")
	if cfg.TokenEncryptionKMSDualDecryptCSV != "" {
		parts := strings.Split(cfg.TokenEncryptionKMSDualDecryptCSV, ",")
		var count int
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				count++
			}
		}
		if count > 5 { //nolint:mnd // documented cap
			return nil, fmt.Errorf("TOKEN_ENCRYPTION_KMS_DUAL_DECRYPT_VERSIONS: cap exceeded (%d > 5)", count)
		}
	}

	cfg.TokenEncryptionKMSVersionMap = map[string]int16{}
	if raw := os.Getenv("TOKEN_ENCRYPTION_KMS_VERSION_MAP"); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("TOKEN_ENCRYPTION_KMS_VERSION_MAP: invalid entry %q (want versionID=int16)", entry)
			}
			versionID := strings.TrimSpace(parts[0])
			n, parseErr := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 16)
			if parseErr != nil {
				return nil, fmt.Errorf("TOKEN_ENCRYPTION_KMS_VERSION_MAP: invalid int16 value for %q: %w", versionID, parseErr)
			}
			cfg.TokenEncryptionKMSVersionMap[versionID] = int16(n)
		}
	}

	if cfg.IsProduction() {
		if err := validateLegalProduction(cfg); err != nil {
			return nil, fmt.Errorf("LEGAL_* validation failed in production: %w", err)
		}
	} else if err := validateLegalProduction(cfg); err != nil {
		slog.Warn("LEGAL_* placeholders present (allowed in non-production)", "issue", err.Error())
	}

	acl, err := parseInternalACL()
	if err != nil {
		return nil, err
	}
	cfg.InternalACL = acl

	return cfg, nil
}

// parseInternalACL reads ONEVOICE_INTERNAL_ACL_JSON and decodes the declarative
// CN→[]platforms map gating /internal/v1/tokens. The variable is required: an
// empty value, unparsable JSON, or an empty object aborts boot so a misconfigured
// deploy fails loud rather than running the internal token endpoint without a
// usable platform gate (an empty map rejects every CN).
func parseInternalACL() (map[string][]string, error) {
	raw := os.Getenv("ONEVOICE_INTERNAL_ACL_JSON")
	if raw == "" {
		return nil, fmt.Errorf(`ONEVOICE_INTERNAL_ACL_JSON is required; example: {"agent-telegram":["telegram"],"orchestrator":["telegram","vk","yandex_business","google_business"],"api":["*"]}`)
	}
	var acl map[string][]string
	if err := json.Unmarshal([]byte(raw), &acl); err != nil {
		return nil, fmt.Errorf("ONEVOICE_INTERNAL_ACL_JSON invalid: %w", err)
	}
	if len(acl) == 0 {
		return nil, fmt.Errorf("ONEVOICE_INTERNAL_ACL_JSON must contain at least one CN→platforms entry")
	}
	return acl, nil
}

// IsProduction reports whether the service is running in the production
// environment. The single source of truth for the APP_ENV gate that fail-closed
// boot checks (legal validation, transactional email) share, so each gate uses
// identical matching semantics (case-insensitive, whitespace-trimmed).
func (c *Config) IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(c.AppEnv), "production")
}

// RequireInternalMTLS returns a fatal boot error when the internal listener —
// which serves the decrypted-token endpoint — would run over plain HTTP in a
// context that demands mTLS. mTLS is mandatory in production, and in any
// deployment that wraps tokens with KMS (a production-grade posture): without
// it, the platform ACL middleware degrades a missing client cert to the
// "system" identity, leaving the decrypted-token endpoint reachable over plain
// HTTP by anyone who can route to the port. tlsConfigured reports whether
// MaybeServerTLSConfig returned a non-nil *tls.Config. Dev (non-prod, no KMS)
// is unaffected and may serve the internal listener over plain HTTP.
func (c *Config) RequireInternalMTLS(tlsConfigured bool) error {
	if tlsConfigured {
		return nil
	}
	if c.IsProduction() || c.TokenEncryptionKMSKeyID != "" {
		return fmt.Errorf("internal listener: mTLS is required but not configured — the decrypted-token endpoint must not be served over plain HTTP in production or with KMS enabled; set ONEVOICE_MTLS_ENABLED=true with the mTLS cert paths (run `make mtls-certs`; see docs/security.md)")
	}
	return nil
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
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration, got %q: %w", key, value, err)
	}
	return d, nil
}
