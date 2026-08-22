package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/f1xgun/onevoice/pkg/crypto"
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

	// YandexSharedBusinessID is the config-pinned sentinel business under which
	// the single KMS-wrapped shared representative session cookie JSON is stored
	// (external_id "__shared_rep__"). Empty disables the delegated-representative
	// access path fail-closed: any integration resolving with no per-business
	// credential returns a clear "delegated access not configured" error, and
	// nothing else changes. Must match the API's YANDEX_SHARED_BUSINESS_ID.
	YandexSharedBusinessID string

	// A2APayloadKey is the required 32-byte AES-256 key used to decrypt sealed
	// tool arguments (the Yandex connect cookies) the API seals before sending
	// them over NATS. Must match A2A_PAYLOAD_KEY on the API, or the connect flow
	// cannot decrypt the cookies.
	A2APayloadKey string

	// ScopeGateEnforce controls the RPA request scope gate. When false (the
	// default) the gate runs REPORT-ONLY: out-of-scope requests are metered,
	// audited, and logged but NOT aborted, adding observability without risking
	// the live RPA on a too-tight allowlist. Set SCOPE_GATE_ENFORCE=true to
	// hard-block. The gate is always installed (report-only or enforcing).
	ScopeGateEnforce bool
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
	// A2A_PAYLOAD_KEY is required and must be a valid AES-256 key: the API always
	// seals the connect cookies, so a missing or wrong-length key here would fail
	// the connect flow at decrypt time. Fail loud at boot instead, and keep it in
	// lockstep with the API's identical requirement.
	payloadKey := os.Getenv("A2A_PAYLOAD_KEY")
	if payloadKey == "" {
		return nil, fmt.Errorf("A2A_PAYLOAD_KEY is required (exactly %d bytes; must match the API) — generate with: openssl rand -base64 24", crypto.AES256KeyLen)
	}
	if len(payloadKey) != crypto.AES256KeyLen {
		return nil, fmt.Errorf("A2A_PAYLOAD_KEY must be exactly %d bytes", crypto.AES256KeyLen)
	}
	return &Config{
		NATSUrl:                getEnv("NATS_URL", "nats://localhost:4222"),
		APIInternalURL:         getEnv("API_INTERNAL_URL", defaultAPIInternalURL),
		HealthPort:             getEnv("HEALTH_PORT", "8083"),
		RedisURL:               getEnv("REDIS_URL", "redis://redis:6379"),
		BrowserPoolMaxContexts: maxContexts,
		YandexSharedBusinessID: os.Getenv("YANDEX_SHARED_BUSINESS_ID"),
		A2APayloadKey:          payloadKey,
		ScopeGateEnforce:       os.Getenv("SCOPE_GATE_ENFORCE") == "true",
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
