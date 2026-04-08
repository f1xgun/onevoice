package config

import "os"

// Config holds the agent-google-business configuration.
type Config struct {
	NATSUrl        string
	APIInternalURL string
	HealthPort     string
}

// Load reads configuration from environment variables with defaults.
func Load() *Config {
	return &Config{
		NATSUrl:        getEnv("NATS_URL", "nats://localhost:4222"),
		APIInternalURL: getEnv("API_INTERNAL_URL", "http://localhost:8443"),
		HealthPort:     getEnv("HEALTH_PORT", "8083"),
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
