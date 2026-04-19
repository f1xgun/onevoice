package config

import "os"

// Config holds the agent-yandex-business configuration.
type Config struct {
	NATSUrl        string
	APIInternalURL string
	HealthPort     string
	// RedisURL is the dial URL for the HITL dedupe Redis instance. Empty
	// disables the dedupe gate — the handler falls through to legacy behavior.
	RedisURL string
}

// Load reads configuration from environment variables with defaults.
func Load() *Config {
	return &Config{
		NATSUrl:        getEnv("NATS_URL", "nats://localhost:4222"),
		APIInternalURL: getEnv("API_INTERNAL_URL", "http://localhost:8443"),
		HealthPort:     getEnv("HEALTH_PORT", "8083"),
		RedisURL:       getEnv("REDIS_URL", "redis://redis:6379"),
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
