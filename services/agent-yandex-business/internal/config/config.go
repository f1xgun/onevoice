package config

import (
	"fmt"
	"os"
	"strconv"
)

// defaultAPIInternalURL is the dev-mode fallback for API_INTERNAL_URL —
// the local API service binding. Production must set the env var.
const defaultAPIInternalURL = "http://localhost:8443"

// defaultBrowserPoolMaxContexts caps the BrowserPool at 10 live Chromium
// contexts by default. Each context costs ~80–150 MB; 10 fits comfortably in
// ~1.5 GB of agent headroom.
const defaultBrowserPoolMaxContexts = 10

// Config holds the agent-yandex-business configuration.
type Config struct {
	NATSUrl        string
	APIInternalURL string
	HealthPort     string
	// RedisURL is the dial URL for the HITL dedupe Redis instance. Empty
	// disables the dedupe gate — the handler falls through to legacy behavior.
	RedisURL string
	// BrowserPoolMaxContexts bounds the live Playwright BrowserContexts.
	// 0 disables the cap (dev/test only — production must keep > 0).
	BrowserPoolMaxContexts int
}

// Load reads configuration from environment variables with defaults. Returns
// a non-nil error if any env var fails to parse so the binary fails loud at
// boot rather than running with a silently-coerced default.
func Load() (*Config, error) {
	maxContexts, err := loadIntEnv("BROWSER_POOL_MAX_CONTEXTS", defaultBrowserPoolMaxContexts)
	if err != nil {
		return nil, err
	}
	if maxContexts < 0 {
		return nil, fmt.Errorf("BROWSER_POOL_MAX_CONTEXTS must be >= 0, got %d", maxContexts)
	}
	return &Config{
		NATSUrl:                getEnv("NATS_URL", "nats://localhost:4222"),
		APIInternalURL:         getEnv("API_INTERNAL_URL", defaultAPIInternalURL),
		HealthPort:             getEnv("HEALTH_PORT", "8083"),
		RedisURL:               getEnv("REDIS_URL", "redis://redis:6379"),
		BrowserPoolMaxContexts: maxContexts,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func loadIntEnv(key string, defaultValue int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, raw, err)
	}
	return v, nil
}
