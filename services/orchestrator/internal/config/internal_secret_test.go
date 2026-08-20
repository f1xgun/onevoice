package config_test

import (
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

func TestRequireInternalSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		appEnv  string
		wantErr bool
	}{
		{name: "set and long enough", secret: "abcdefghijklmnop-long", appEnv: "production", wantErr: false},
		{name: "set but too short", secret: "short", appEnv: "development", wantErr: true},
		{name: "unset in production", secret: "", appEnv: "production", wantErr: true},
		{name: "unset outside production", secret: "", appEnv: "development", wantErr: false},
		{name: "unset with empty app_env", secret: "", appEnv: "", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)
			cfg := &config.Config{InternalSecret: tc.secret}
			err := cfg.RequireInternalSecret()
			if tc.wantErr && err == nil {
				t.Fatalf("RequireInternalSecret() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("RequireInternalSecret() = %v, want nil", err)
			}
		})
	}
}

func TestLoad_InternalSecretFromEnv(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("ORCHESTRATOR_INTERNAL_SECRET", "abcdefghijklmnop-long")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.EqualFold(cfg.InternalSecret, "abcdefghijklmnop-long") {
		t.Fatalf("InternalSecret = %q, want the env value", cfg.InternalSecret)
	}
}
