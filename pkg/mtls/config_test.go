package mtls_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/mtls"
)

// generatePKI builds an in-process ephemeral CA + server-leaf cert pair,
// writes them as PEM files in dir, and returns a ServiceCertPaths pointing
// at them. Pure stdlib — no openssl shell-out — so unit tests don't depend
// on the dev script having been run.
func generatePKI(t *testing.T, dir string) mtls.ServiceCertPaths {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-service"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost", "test-service"},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	caPath := filepath.Join(dir, "ca.crt")
	certPath := filepath.Join(dir, "leaf.crt")
	keyPath := filepath.Join(dir, "leaf.key")

	writePEM(t, caPath, "CERTIFICATE", caDER)
	writePEM(t, certPath, "CERTIFICATE", leafDER)
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	require.NoError(t, err)
	writePEM(t, keyPath, "PRIVATE KEY", leafKeyDER)

	return mtls.ServiceCertPaths{CACertPath: caPath, CertPath: certPath, KeyPath: keyPath}
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}))
}

// TestLoadServerTLSConfig asserts ClientAuth==RequireAndVerifyClientCert
// + ClientCAs populated.
func TestLoadServerTLSConfig(t *testing.T) {
	paths := generatePKI(t, t.TempDir())

	cfg, err := mtls.LoadServerTLSConfig(paths)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth,
		"server config must require + verify the client cert")
	assert.NotNil(t, cfg.ClientCAs, "ClientCAs must be populated for client-cert verification")
	assert.Len(t, cfg.Certificates, 1, "server must present exactly one leaf cert")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "TLS 1.2 floor")
}

// TestLoadClientTLSConfig asserts Certificates len==1 + RootCAs populated.
func TestLoadClientTLSConfig(t *testing.T) {
	paths := generatePKI(t, t.TempDir())

	cfg, err := mtls.LoadClientTLSConfig(paths)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.Certificates, 1, "client must present exactly one leaf cert")
	assert.NotNil(t, cfg.RootCAs, "RootCAs must be populated so the client trusts only the dev CA")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestLoad_MissingFile asserts wrapping with an underlying fs error, never
// a panic.
func TestLoad_MissingFile(t *testing.T) {
	bogus := mtls.ServiceCertPaths{
		CACertPath: "/nonexistent/ca.crt",
		CertPath:   "/nonexistent/leaf.crt",
		KeyPath:    "/nonexistent/leaf.key",
	}

	t.Run("server", func(t *testing.T) {
		require.NotPanics(t, func() {
			_, err := mtls.LoadServerTLSConfig(bogus)
			require.Error(t, err)
			assert.True(t, errors.Is(err, fs.ErrNotExist),
				"missing key/cert must chain fs.ErrNotExist; got %v", err)
		})
	})

	t.Run("client", func(t *testing.T) {
		require.NotPanics(t, func() {
			_, err := mtls.LoadClientTLSConfig(bogus)
			require.Error(t, err)
			assert.True(t, errors.Is(err, fs.ErrNotExist),
				"missing key/cert must chain fs.ErrNotExist; got %v", err)
		})
	})

	t.Run("missing CA only", func(t *testing.T) {
		// Valid leaf cert + key, missing CA — should still chain fs.ErrNotExist.
		good := generatePKI(t, t.TempDir())
		broken := mtls.ServiceCertPaths{
			CACertPath: "/nonexistent/ca.crt",
			CertPath:   good.CertPath,
			KeyPath:    good.KeyPath,
		}
		_, err := mtls.LoadServerTLSConfig(broken)
		require.Error(t, err)
		assert.True(t, errors.Is(err, fs.ErrNotExist))
	})
}

// TestPathsFromEnv exercises env-var loading. Errors when
// ONEVOICE_MTLS_ENABLED=true and any path is missing.
func TestPathsFromEnv(t *testing.T) {
	t.Run("enabled with all paths set", func(t *testing.T) {
		t.Setenv(mtls.EnvEnabled, "true")
		t.Setenv(mtls.EnvCAPath, "/certs/ca.crt")
		t.Setenv(mtls.EnvCertPath, "/certs/api.crt")
		t.Setenv(mtls.EnvKeyPath, "/certs/api.key")

		p, err := mtls.PathsFromEnv()
		require.NoError(t, err)
		assert.Equal(t, "/certs/ca.crt", p.CACertPath)
		assert.Equal(t, "/certs/api.crt", p.CertPath)
		assert.Equal(t, "/certs/api.key", p.KeyPath)
	})

	t.Run("enabled but missing CA path returns error", func(t *testing.T) {
		t.Setenv(mtls.EnvEnabled, "true")
		t.Setenv(mtls.EnvCAPath, "")
		t.Setenv(mtls.EnvCertPath, "/certs/api.crt")
		t.Setenv(mtls.EnvKeyPath, "/certs/api.key")

		_, err := mtls.PathsFromEnv()
		require.Error(t, err)
		assert.Contains(t, err.Error(), mtls.EnvCAPath,
			"error must name the missing env var so operators can fix the misconfig")
	})

	t.Run("disabled returns empty struct without error", func(t *testing.T) {
		t.Setenv(mtls.EnvEnabled, "")
		t.Setenv(mtls.EnvCAPath, "")
		t.Setenv(mtls.EnvCertPath, "")
		t.Setenv(mtls.EnvKeyPath, "")

		p, err := mtls.PathsFromEnv()
		require.NoError(t, err)
		assert.Empty(t, p.CACertPath)
		assert.False(t, mtls.IsEnabled())
	})

	t.Run("disabled with stray vars set is still OK", func(t *testing.T) {
		// Real-world: docker-compose may leak partial env into a dev shell.
		// Without ONEVOICE_MTLS_ENABLED=true, we just hand back whatever is set
		// — the caller branches on IsEnabled().
		t.Setenv(mtls.EnvEnabled, "false")
		t.Setenv(mtls.EnvCAPath, "/some/path")
		t.Setenv(mtls.EnvCertPath, "")
		t.Setenv(mtls.EnvKeyPath, "")

		p, err := mtls.PathsFromEnv()
		require.NoError(t, err)
		assert.Equal(t, "/some/path", p.CACertPath)
	})
}

// TestLoadClientTLSConfig_Idempotent — two loads of the same file produce
// equivalent leaf certs.
func TestLoadClientTLSConfig_Idempotent(t *testing.T) {
	paths := generatePKI(t, t.TempDir())

	cfg1, err := mtls.LoadClientTLSConfig(paths)
	require.NoError(t, err)
	cfg2, err := mtls.LoadClientTLSConfig(paths)
	require.NoError(t, err)

	require.Len(t, cfg1.Certificates, 1)
	require.Len(t, cfg2.Certificates, 1)
	assert.Equal(t, cfg1.Certificates[0].Certificate[0], cfg2.Certificates[0].Certificate[0],
		"loading the same key/cert twice must yield identical leaf bytes")
}
