package natsauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	natslib "github.com/nats-io/nats.go"
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

func TestOptions_SeedErrorsFailBeforeDial(t *testing.T) {
	for _, name := range []string{"missing", "malformed", "unreadable"} {
		t.Run(name, func(t *testing.T) {
			clearAuthEnv(t)
			path := filepath.Join(t.TempDir(), "user.nk")
			if name != "missing" {
				content := testSeed
				mode := os.FileMode(0o600)
				if name == "malformed" {
					content = "invalid"
				}
				if name == "unreadable" {
					mode = 0
				}
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
				if name == "unreadable" && os.Geteuid() == 0 {
					t.Skip("root bypasses file permissions")
				}
			}
			t.Setenv("NATS_NKEY_SEED", path)
			t.Setenv("NATS_USER", "fallback-must-not-be-used")
			conn, err := natslib.Connect(natslib.DefaultURL, Options()...)
			if conn != nil {
				conn.Close()
				t.Fatal("unexpected connection")
			}
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "load NATS nkey seed") {
				t.Fatalf("expected a credential error naming the path, got %v", err)
			}
		})
	}
}

func TestOptions_SeedSignsNonce(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("NATS_NKEY_SEED", writeSeedFile(t))
	var options natslib.Options
	for _, option := range Options() {
		if err := option(&options); err != nil {
			t.Fatal(err)
		}
	}
	if options.Nkey == "" || options.SignatureCB == nil {
		t.Fatal("missing nkey authentication")
	}
	signature, err := options.SignatureCB([]byte("test nonce"))
	if err != nil || len(signature) == 0 {
		t.Fatal("seed cannot sign nonce")
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
