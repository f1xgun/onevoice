package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// SelfHostedEndpoint holds configuration for one self-hosted LLM inference
// endpoint. Lifted verbatim from
// services/orchestrator/internal/config/config.go so the API-side titler
// reuses the same wiring shape (Phase 18 — Auto-titler).
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
	// the values that were hardcoded in services/api/cmd/main.go before
	// Wave 2.4. Knob purpose:
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

	// Shutdown
	ShutdownTimeout time.Duration

	// Phase 18 — Auto-titler. TitlerModel falls back to LLMModel when unset;
	// when both are unset the titler is disabled (graceful no-op per
	// Pitfall 1 / Assumption A6 — API must boot cleanly without any LLM env).
	LLMModel    string
	LLMTier     string
	TitlerModel string

	// Phase 18 — LLM provider API keys. Lifted verbatim from
	// services/orchestrator/internal/config/config.go:31-44 so the API-side
	// titler Router constructs over the same provider set as the orchestrator.
	// At least one must be set when TitlerModel != "" — otherwise the titler
	// is left disabled (graceful no-op) and Plan 05's trigger gate becomes a
	// no-op. The API service itself does NOT fail-fast on missing keys
	// (different from orchestrator, which requires LLM_MODEL).
	OpenRouterAPIKey    string
	OpenAIAPIKey        string
	AnthropicAPIKey     string
	SelfHostedEndpoints []SelfHostedEndpoint
}

func Load() (*Config, error) {
	shutdownTimeout := 30 * time.Second
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
		SecureCookies: getEnv("SECURE_COOKIES", "true") == "true",

		VKClientID:         os.Getenv("VK_CLIENT_ID"),
		VKClientSecret:     os.Getenv("VK_CLIENT_SECRET"),
		VKRedirectURI:      getEnv("VK_REDIRECT_URI", "http://localhost/api/v1/oauth/vk/callback"),
		VKServiceKey:       os.Getenv("VK_SERVICE_KEY"),
		YandexClientID:     os.Getenv("YANDEX_CLIENT_ID"),
		YandexClientSecret: os.Getenv("YANDEX_CLIENT_SECRET"),
		YandexRedirectURI:  getEnv("YANDEX_REDIRECT_URI", "http://localhost/api/v1/oauth/yandex_business/callback"),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURI:  getEnv("GOOGLE_REDIRECT_URI", "http://localhost/api/v1/oauth/google_business/callback"),
		InternalPort:       getEnv("INTERNAL_PORT", "8443"),
		OrchestratorURL:    getEnv("ORCHESTRATOR_URL", "http://localhost:8090"),
		NATSUrl:            os.Getenv("NATS_URL"),
		ReviewSyncInterval: getEnvInt("REVIEW_SYNC_INTERVAL_MINUTES", 30),

		ReviewDraftEnabled:     getEnv("REVIEW_DRAFT_ENABLED", "false") == "true",
		ReviewDraftMaxExamples: getEnvInt("REVIEW_DRAFT_MAX_EXAMPLES", 5),
		ReviewDraftBatchLimit:  getEnvInt("REVIEW_DRAFT_BATCH_LIMIT", 10),

		S3Endpoint:        getEnv("S3_ENDPOINT", "minio:9000"),
		S3AccessKey:       getEnv("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:       getEnv("S3_SECRET_KEY", "minioadmin"),
		S3Bucket:          getEnv("S3_BUCKET", "onevoice"),
		S3UseSSL:          getEnv("S3_USE_SSL", "false") == "true",
		S3PublicURLPrefix: getEnv("S3_PUBLIC_URL_PREFIX", "/media"),

		PublicURL:          getEnv("PUBLIC_URL", "http://localhost:8080"),
		CORSAllowedOrigins: getEnvSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),

		HTTPReadTimeout:          getEnvDuration("HTTP_READ_TIMEOUT", 15*time.Second),
		HTTPReadHeaderTimeout:    getEnvDuration("HTTP_READ_HEADER_TIMEOUT", 10*time.Second),
		HTTPIdleTimeout:          getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		OrchestratorFetchTimeout: getEnvDuration("ORCHESTRATOR_FETCH_TIMEOUT", 10*time.Second),

		RateLimitRegister: getEnvInt("RATE_LIMIT_REGISTER", 5),
		RateLimitLogin:    getEnvInt("RATE_LIMIT_LOGIN", 10),
		RateLimitChat:     getEnvInt("RATE_LIMIT_CHAT", 10),
		RateLimitHITL:     getEnvInt("RATE_LIMIT_HITL", 10),

		ShutdownTimeout: shutdownTimeout,
	}

	// Phase 18 — Auto-titler env loading. Mirrors
	// services/orchestrator/internal/config/config.go but does NOT fail-fast
	// on missing LLMModel — Pitfall 1 / Assumption A6 mandates graceful
	// disable so the API service boots in dev environments with no LLM env
	// configured at all.
	cfg.LLMModel = os.Getenv("LLM_MODEL")
	cfg.LLMTier = os.Getenv("LLM_TIER")
	if cfg.LLMTier == "" {
		cfg.LLMTier = "free"
	}
	cfg.TitlerModel = os.Getenv("TITLER_MODEL")
	if cfg.TitlerModel == "" {
		cfg.TitlerModel = cfg.LLMModel // graceful fallback per D-discretion
	}
	cfg.OpenRouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	cfg.SelfHostedEndpoints = parseIndexedEndpoints()

	// Validate required fields
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	if cfg.EncryptionKey == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required")
	}
	if len(cfg.EncryptionKey) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be exactly 32 bytes")
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
// services/orchestrator/internal/config/config.go:140-159 so byte-identical
// semantics apply on the API side (Phase 18 — Landmine 3 mitigation).
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
