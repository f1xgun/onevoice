package natsauth

import "testing"

func TestOptions_EmptyEnvYieldsNoOptions(t *testing.T) {
	t.Setenv("NATS_CREDS", "")
	t.Setenv("NATS_USER", "")
	t.Setenv("NATS_PASSWORD", "")
	t.Setenv("NATS_CA_PATH", "")
	t.Setenv("NATS_CLIENT_CERT", "")
	t.Setenv("NATS_CLIENT_KEY", "")

	if got := Options(); len(got) != 0 {
		t.Fatalf("expected no options with empty env, got %d", len(got))
	}
}

func TestOptions_UserPassword(t *testing.T) {
	t.Setenv("NATS_CREDS", "")
	t.Setenv("NATS_USER", "svc")
	t.Setenv("NATS_PASSWORD", "s3cret")
	t.Setenv("NATS_CA_PATH", "")
	t.Setenv("NATS_CLIENT_CERT", "")
	t.Setenv("NATS_CLIENT_KEY", "")

	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option for user/password, got %d", len(got))
	}
}

func TestOptions_CredsTakesPrecedenceOverUserPassword(t *testing.T) {
	t.Setenv("NATS_CREDS", "/etc/nats/svc.creds")
	t.Setenv("NATS_USER", "svc")
	t.Setenv("NATS_PASSWORD", "s3cret")
	t.Setenv("NATS_CA_PATH", "")
	t.Setenv("NATS_CLIENT_CERT", "")
	t.Setenv("NATS_CLIENT_KEY", "")

	if got := Options(); len(got) != 1 {
		t.Fatalf("expected 1 option (creds only), got %d", len(got))
	}
}

func TestOptions_TLSMaterial(t *testing.T) {
	t.Setenv("NATS_CREDS", "")
	t.Setenv("NATS_USER", "")
	t.Setenv("NATS_PASSWORD", "")
	t.Setenv("NATS_CA_PATH", "/etc/nats/ca.crt")
	t.Setenv("NATS_CLIENT_CERT", "/etc/nats/client.crt")
	t.Setenv("NATS_CLIENT_KEY", "/etc/nats/client.key")

	if got := Options(); len(got) != 2 {
		t.Fatalf("expected 2 options (RootCAs + ClientCert), got %d", len(got))
	}
}
