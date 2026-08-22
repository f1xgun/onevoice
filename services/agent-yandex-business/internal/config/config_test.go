package config

import (
	"testing"
)

// validPayloadKey is a 32-byte AES-256 key so Load() passes the mandatory
// A2A_PAYLOAD_KEY validation in tests that assert other behavior.
const validPayloadKey = "pT9wX2qZ7vN4mJ8yR3sB6kL1hG5dF0aC"

func TestConfig_BrowserPoolMaxContexts_Default(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "")
	t.Setenv("A2A_PAYLOAD_KEY", validPayloadKey)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.BrowserPoolMaxContexts != defaultBrowserPoolMaxContexts {
		t.Fatalf("BrowserPoolMaxContexts = %d, want %d", cfg.BrowserPoolMaxContexts, defaultBrowserPoolMaxContexts)
	}
}

func TestConfig_BrowserPoolMaxContexts_EnvOverride(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "25")
	t.Setenv("A2A_PAYLOAD_KEY", validPayloadKey)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.BrowserPoolMaxContexts != 25 {
		t.Fatalf("BrowserPoolMaxContexts = %d, want 25", cfg.BrowserPoolMaxContexts)
	}
}

func TestConfig_BrowserPoolMaxContexts_InvalidValue_FailsLoud(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "not-a-number")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() succeeded with invalid env; cfg=%+v", cfg)
	}
}

func TestConfig_BrowserPoolMaxContexts_Zero_DisablesCap(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "0")
	t.Setenv("A2A_PAYLOAD_KEY", validPayloadKey)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.BrowserPoolMaxContexts != 0 {
		t.Fatalf("BrowserPoolMaxContexts = %d, want 0", cfg.BrowserPoolMaxContexts)
	}
}

func TestConfig_A2APayloadKey_Required(t *testing.T) {
	t.Setenv("A2A_PAYLOAD_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without A2A_PAYLOAD_KEY; want error")
	}
}

func TestConfig_A2APayloadKey_WrongLength_FailsLoud(t *testing.T) {
	t.Setenv("A2A_PAYLOAD_KEY", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded with a short A2A_PAYLOAD_KEY; want error")
	}
}

func TestConfig_A2APayloadKey_Valid(t *testing.T) {
	t.Setenv("A2A_PAYLOAD_KEY", validPayloadKey)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.A2APayloadKey != validPayloadKey {
		t.Fatalf("A2APayloadKey = %q, want %q", cfg.A2APayloadKey, validPayloadKey)
	}
}

func TestConfig_BrowserPoolMaxContexts_Negative_FailsLoud(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "-1")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() succeeded with negative cap; cfg=%+v", cfg)
	}
}
