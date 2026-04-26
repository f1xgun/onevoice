package config

import (
	"fmt"
	"os"
	"strconv"
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

	// Object storage (MinIO / S3) for user uploads
	S3Endpoint        string
	S3AccessKey       string
	S3SecretKey       string
	S3Bucket          string
	S3UseSSL          bool
	S3PublicURLPrefix string // prefix used in client-facing URLs, e.g. "/media"

	PublicURL string

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

		S3Endpoint:        getEnv("S3_ENDPOINT", "minio:9000"),
		S3AccessKey:       getEnv("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:       getEnv("S3_SECRET_KEY", "minioadmin"),
		S3Bucket:          getEnv("S3_BUCKET", "onevoice"),
		S3UseSSL:          getEnv("S3_USE_SSL", "false") == "true",
		S3PublicURLPrefix: getEnv("S3_PUBLIC_URL_PREFIX", "/media"),

		PublicURL:       getEnv("PUBLIC_URL", "http://localhost:8080"),
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
