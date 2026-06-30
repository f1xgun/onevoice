package agentbase

import (
	"strings"
	"testing"
)

// TestRedactConnURL_MasksPassword pins that a connection URL carrying an
// embedded userinfo password is never logged verbatim. The host:port must
// survive (operators still need it to identify the endpoint) while the secret
// is masked. Reverting the call sites to log the raw URL makes "s3cretpass"
// reappear, failing the NotContains assertion below.
func TestRedactConnURL_MasksPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{"redis with password", "redis://:s3cretpass@localhost:6379/0"},
		{"redis with user and password", "redis://user:s3cretpass@localhost:6379/0"},
		{"nats with user and password", "nats://user:s3cretpass@localhost:6379"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := redactConnURL(tt.raw)
			if strings.Contains(got, "s3cretpass") {
				t.Errorf("redactConnURL(%q) = %q, must not contain the password", tt.raw, got)
			}
			if !strings.Contains(got, "localhost:6379") {
				t.Errorf("redactConnURL(%q) = %q, want host:port preserved", tt.raw, got)
			}
		})
	}
}

func TestRedactConnURL_EmptyAndUnparseable(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "://no scheme with spaces"} {
		if got := redactConnURL(raw); got != "<redacted>" {
			t.Errorf("redactConnURL(%q) = %q, want %q", raw, got, "<redacted>")
		}
	}
}
