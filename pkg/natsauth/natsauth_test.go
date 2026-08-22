package natsauth

import (
	"os"
	"path/filepath"
	"testing"
)

// testSeed is a throwaway nkey user seed (generated with nkeys.CreateUser,
// never used against a real server) so tests can exercise the NATS_NKEY_SEED
// path without importing nkeys or reaching the network.
const testSeed = "SUAAEQGGCN7475XS35QK3WURFP3MQJRV5P3JV7PT2V3OQVYQSKAXROXCSU"

// clearAuthEnv zeroes every env var Options() reads so each test starts hermetic.
func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"NATS_CREDS", "NATS_NKEY_SEED", "NATS_USER", "NATS_PASSWORD",
		"NATS_CA_PATH", "NATS_CLIENT_CERT", "NATS_CLIENT_KEY",
	} {
		t.Setenv(k, "")
	}
}

// writeSeedFile writes testSeed to a temp file and returns its path.
func writeSeedFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "user.nk")
	if err := os.WriteFile(path, []byte(testSeed+"\n"), 0o600); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	return path
}

func TestOptions_EmptyEnvYieldsNoOptions(t *testing.T) {
	clearAuthEnv(t)
	if got := Options(); len(got) != 0 {
		t.Fatalf("expected no options with empty env, got %d", len(got))
	}
}

func TestOptions_UserPassword(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_USER", "svc")
	t.Setenv("NATS_PASSWORD", "s3cret")
	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option for user/password, got %d", len(got))
	}
}

func TestOptions_CredsTakesPrecedenceOverUserPassword(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_CREDS", "/etc/nats/svc.creds")
	t.Setenv("NATS_USER", "svc")
	t.Setenv("NATS_PASSWORD", "s3cret")
	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option (creds only), got %d", len(got))
	}
}

func TestOptions_NkeySeed(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_NKEY_SEED", writeSeedFile(t))
	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option for nkey seed, got %d", len(got))
	}
}

func TestOptions_CredsBeatsNkey(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_CREDS", "/etc/nats/svc.creds")
	t.Setenv("NATS_NKEY_SEED", writeSeedFile(t))
	// creds wins; the nkey seed must not add a second option.
	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option (creds beats nkey), got %d", len(got))
	}
}

func TestOptions_NkeyBeatsUserPassword(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_NKEY_SEED", writeSeedFile(t))
	t.Setenv("NATS_USER", "svc")
	t.Setenv("NATS_PASSWORD", "s3cret")
	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option (nkey beats user/password), got %d", len(got))
	}
}

func TestOptions_InvalidNkeySeedSkipped(t *testing.T) {
	clearAuthEnv(t)
	bad := filepath.Join(t.TempDir(), "bad.nk")
	if err := os.WriteFile(bad, []byte("not-a-valid-seed"), 0o600); err != nil {
		t.Fatalf("write bad seed: %v", err)
	}
	t.Setenv("NATS_NKEY_SEED", bad)
	// A malformed seed yields no option (the failure surfaces at Connect, not here).
	if got := Options(); len(got) != 0 {
		t.Fatalf("expected 0 options for invalid seed, got %d", len(got))
	}
}

func TestOptions_TLSMaterial(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_CA_PATH", "/etc/nats/ca.crt")
	t.Setenv("NATS_CLIENT_CERT", "/etc/nats/client.crt")
	t.Setenv("NATS_CLIENT_KEY", "/etc/nats/client.key")
	if got := Options(); len(got) != 2 {
		t.Fatalf("expected 2 options (RootCAs + ClientCert), got %d", len(got))
	}
}

func TestOptions_NkeyWithTLS(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_NKEY_SEED", writeSeedFile(t))
	t.Setenv("NATS_CA_PATH", "/etc/nats/ca.crt")
	// nkey auth (1) + server-cert verification (1).
	if got := Options(); len(got) != 2 {
		t.Fatalf("expected 2 options (nkey + RootCAs), got %d", len(got))
	}
}
