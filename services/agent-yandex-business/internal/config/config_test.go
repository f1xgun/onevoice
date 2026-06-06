package config

import (
	"testing"
)

func TestConfig_BrowserPoolMaxContexts_Default(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "")

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

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.BrowserPoolMaxContexts != 0 {
		t.Fatalf("BrowserPoolMaxContexts = %d, want 0", cfg.BrowserPoolMaxContexts)
	}
}

func TestConfig_BrowserPoolMaxContexts_Negative_FailsLoud(t *testing.T) {
	t.Setenv("BROWSER_POOL_MAX_CONTEXTS", "-1")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() succeeded with negative cap; cfg=%+v", cfg)
	}
}
