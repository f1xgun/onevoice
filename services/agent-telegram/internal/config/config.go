package config

import "os"

// defaultAPIInternalURL is the dev-mode fallback for API_INTERNAL_URL —
// the local API service binding. Production must set the env var.
const defaultAPIInternalURL = "http://localhost:8443"

// Config holds the agent-telegram configuration.
type Config struct {
	NATSUrl        string
	APIInternalURL string
	HealthPort     string
	// RedisURL is the dial URL for the HITL dedupe Redis instance. Empty
	// disables the dedupe gate — the handler falls through to legacy behavior.
	RedisURL string
	// ApprovalHMACSecret signs the opaque callback_data on the [Approve]/[Reject]
	// inline buttons attached to owner approval notifications. It MUST match the
	// api service's TELEGRAM_APPROVAL_HMAC_SECRET so the api-side consumer's MAC
	// verify accepts the buttons this agent builds. Empty disables the buttons
	// fail-closed: the notification is still sent, just without the inline
	// keyboard (no unauthenticated approval surface is ever opened).
	ApprovalHMACSecret string
}

// Load reads configuration from environment variables with defaults.
func Load() *Config {
	return &Config{
		NATSUrl:            getEnv("NATS_URL", "nats://localhost:4222"),
		APIInternalURL:     getEnv("API_INTERNAL_URL", defaultAPIInternalURL),
		HealthPort:         getEnv("HEALTH_PORT", "8081"),
		RedisURL:           getEnv("REDIS_URL", "redis://redis:6379"),
		ApprovalHMACSecret: os.Getenv("TELEGRAM_APPROVAL_HMAC_SECRET"),
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
