package config

import (
	"fmt"
	"os"
	"strconv"
)

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

	// OAuth credentials
	VKClientID         string
	VKClientSecret     string
	VKRedirectURI      string
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

	// File uploads
	UploadDir string
	PublicURL string
}

func Load() (*Config, error) {
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

		VKClientID:         os.Getenv("VK_CLIENT_ID"),
		VKClientSecret:     os.Getenv("VK_CLIENT_SECRET"),
		VKRedirectURI:      getEnv("VK_REDIRECT_URI", "http://localhost/api/v1/oauth/vk/callback"),
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
		UploadDir:          getEnv("UPLOAD_DIR", "./uploads"),
		PublicURL:          getEnv("PUBLIC_URL", "http://localhost:8080"),
	}

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
