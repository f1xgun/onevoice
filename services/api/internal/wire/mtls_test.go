package wire_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/mtls"
	"github.com/f1xgun/onevoice/services/api/internal/wire"
)

func writePEMFile(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}))
}

func generateEphemeralCertPair(t *testing.T) (caPath, certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
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
		Subject:      pkix.Name{CommonName: "api"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	caPath = filepath.Join(dir, "ca.crt")
	certPath = filepath.Join(dir, "leaf.crt")
	keyPath = filepath.Join(dir, "leaf.key")
	writePEMFile(t, caPath, "CERTIFICATE", caDER)
	writePEMFile(t, certPath, "CERTIFICATE", leafDER)
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	require.NoError(t, err)
	writePEMFile(t, keyPath, "PRIVATE KEY", leafKeyDER)
	return
}

func TestMaybeServerTLSConfig_DisabledReturnsNil(t *testing.T) {
	t.Setenv(mtls.EnvEnabled, "")
	cfg, err := wire.MaybeServerTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg, "mTLS disabled — listener falls back to ListenAndServe")
}

func TestMaybeServerTLSConfig_EnabledReturnsConfig(t *testing.T) {
	caPath, certPath, keyPath := generateEphemeralCertPair(t)
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, caPath)
	t.Setenv(mtls.EnvCertPath, certPath)
	t.Setenv(mtls.EnvKeyPath, keyPath)

	cfg, err := wire.MaybeServerTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	assert.Len(t, cfg.Certificates, 1)
	assert.NotNil(t, cfg.ClientCAs)
}

func TestMaybeServerTLSConfig_EnabledMissingEnvErrors(t *testing.T) {
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, "")
	t.Setenv(mtls.EnvCertPath, "")
	t.Setenv(mtls.EnvKeyPath, "")

	cfg, err := wire.MaybeServerTLSConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "internal listener",
		"error must indicate the failure is on the listener path so operators recognize it at startup")
}
