package tokenclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/mtls"
)

func TestGetToken_FetchesFromAPI(t *testing.T) {
	want := &TokenResponse{
		IntegrationID: "int-123",
		Platform:      "vk",
		ExternalID:    "group-456",
		AccessToken:   "secret-token",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/v1/tokens", r.URL.Path)
		assert.Equal(t, "biz-1", r.URL.Query().Get("business_id"))
		assert.Equal(t, "vk", r.URL.Query().Get("platform"))
		assert.Equal(t, "group-456", r.URL.Query().Get("external_id"))
		require.NoError(t, json.NewEncoder(w).Encode(want))
	}))
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)
	got, err := client.GetToken(context.Background(), "biz-1", "vk", "group-456")
	require.NoError(t, err)
	assert.Equal(t, "secret-token", got.AccessToken)
	assert.Equal(t, "group-456", got.ExternalID)
}

func TestGetToken_CachesResult(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{AccessToken: "tok"}))
	}))
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)

	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	assert.Equal(t, int32(1), callCount.Load(), "should only call API once due to caching")
}

func TestGetToken_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIntegrationNotFound),
		"404 must surface as ErrIntegrationNotFound; got %v", err)
	// Wire-format invariant: log greps depend on this exact prefix.
	assert.Contains(t, err.Error(), "tokenclient: integration not found")
}

func TestGetToken_Gone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTokenExpired),
		"410 must surface as ErrTokenExpired; got %v", err)
	assert.Contains(t, err.Error(), "tokenclient: token expired and refresh failed")
}

// TestGetToken_ServerError_IsTransient covers the 5xx bucket: a flaky
// upstream surfaces as ErrTransient so callers can mark it retryable
// instead of blanket-permanent.
func TestGetToken_ServerError_IsTransient(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			client, err := New(srv.URL, nil)
			require.NoError(t, err)
			_, err = client.GetToken(context.Background(), "b", "vk", "g")
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrTransient),
				"%d must surface as ErrTransient; got %v", status, err)
		})
	}
}

// TestGetToken_NetworkError_IsTransient covers the network-failure leg:
// connection refused / DNS failure / TLS hiccup chains ErrTransient and
// preserves the underlying error for diagnostics.
func TestGetToken_NetworkError_IsTransient(t *testing.T) {
	// Listen on a port, then close it — the next Dial gets connection refused.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	client, err := New("http://"+addr, &http.Client{Timeout: 500 * time.Millisecond})
	require.NoError(t, err)
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransient),
		"network failure must surface as ErrTransient; got %v", err)
}

// TestGetToken_UnexpectedNon5xx_NoSentinel covers 4xx-other-than-404/410:
// likely a request-shape bug rather than a transient outage. No sentinel
// is chained so the caller's default NonRetryable classification holds.
func TestGetToken_UnexpectedNon5xx_NoSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrTransient),
		"4xx other than 404/410 must NOT chain ErrTransient — likely a bug not a blip")
	assert.False(t, errors.Is(err, ErrIntegrationNotFound))
	assert.False(t, errors.Is(err, ErrTokenExpired))
}

// mtlsHarness generates an ephemeral CA + server cert + client cert into
// t.TempDir() and returns the file paths suitable for mtls.LoadServerTLSConfig
// / LoadClientTLSConfig. Used by the Test A/B/C/D pairs below to exercise
// the New(...) mTLS path without shelling out to openssl.
//
// Test isolation: the helper does NOT set env vars — each test calls
// t.Setenv to point ONEVOICE_MTLS_* at the desired subset (signed,
// rogue-signed, or absent).
type mtlsHarness struct {
	caCertPath        string
	serverCertPath    string
	serverKeyPath     string
	clientCertPath    string // signed by the same CA — server accepts
	clientKeyPath     string
	rogueClientCert   string // signed by an UNRELATED CA — server rejects
	rogueClientKey    string
	rogueClientCAPath string
	// Server-side tls.Config — used by httptest.NewUnstartedServer.
	serverTLSConfig *tls.Config
}

func mtlsTestHarness(t *testing.T) *mtlsHarness {
	t.Helper()
	dir := t.TempDir()

	makeCA := func(cn string) (*x509.Certificate, *rsa.PrivateKey, []byte) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: cn},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(24 * time.Hour),
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		require.NoError(t, err)
		return tmpl, key, der
	}

	makeLeaf := func(cn string, parent *x509.Certificate, parentKey *rsa.PrivateKey, sans []string, eku []x509.ExtKeyUsage) ([]byte, *rsa.PrivateKey) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  eku,
			DNSNames:     sans,
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
		require.NoError(t, err)
		return der, key
	}

	write := func(path, blockType string, der []byte) {
		f, err := os.Create(path)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		require.NoError(t, pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}))
	}

	writeKey := func(path string, key *rsa.PrivateKey) {
		der, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		write(path, "PRIVATE KEY", der)
	}

	// Primary CA + server + client.
	caCert, caKey, caDER := makeCA("dev-ca")
	serverDER, serverKey := makeLeaf("api", caCert, caKey,
		[]string{"localhost", "api"},
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientDER, clientKey := makeLeaf("client", caCert, caKey,
		[]string{"client", "localhost"},
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})

	// Rogue CA + rogue client cert — the API CA does NOT trust this CA.
	rogueCA, rogueCAKey, rogueCADER := makeCA("rogue-ca")
	rogueDER, rogueKey := makeLeaf("client", rogueCA, rogueCAKey,
		[]string{"localhost"},
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})

	h := &mtlsHarness{
		caCertPath:        filepath.Join(dir, "ca.crt"),
		serverCertPath:    filepath.Join(dir, "server.crt"),
		serverKeyPath:     filepath.Join(dir, "server.key"),
		clientCertPath:    filepath.Join(dir, "client.crt"),
		clientKeyPath:     filepath.Join(dir, "client.key"),
		rogueClientCert:   filepath.Join(dir, "rogue-client.crt"),
		rogueClientKey:    filepath.Join(dir, "rogue-client.key"),
		rogueClientCAPath: filepath.Join(dir, "rogue-ca.crt"),
	}
	write(h.caCertPath, "CERTIFICATE", caDER)
	write(h.serverCertPath, "CERTIFICATE", serverDER)
	writeKey(h.serverKeyPath, serverKey)
	write(h.clientCertPath, "CERTIFICATE", clientDER)
	writeKey(h.clientKeyPath, clientKey)
	write(h.rogueClientCert, "CERTIFICATE", rogueDER)
	writeKey(h.rogueClientKey, rogueKey)
	write(h.rogueClientCAPath, "CERTIFICATE", rogueCADER)

	serverPaths := mtls.ServiceCertPaths{
		CACertPath: h.caCertPath,
		CertPath:   h.serverCertPath,
		KeyPath:    h.serverKeyPath,
	}
	srvCfg, err := mtls.LoadServerTLSConfig(serverPaths)
	require.NoError(t, err)
	h.serverTLSConfig = srvCfg
	return h
}

// TestGetToken_mTLS_Success — httptest.NewUnstartedServer + server-side mTLS
// config; client.New() with
// nil http.Client picks up the env-driven client cert and completes the
// handshake; happy-path GET returns 200 and a parsed TokenResponse.
func TestGetToken_mTLS_Success(t *testing.T) {
	h := mtlsTestHarness(t)
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, h.caCertPath)
	t.Setenv(mtls.EnvCertPath, h.clientCertPath)
	t.Setenv(mtls.EnvKeyPath, h.clientKeyPath)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{AccessToken: "mtls-ok"}))
	}))
	srv.TLS = h.serverTLSConfig
	srv.StartTLS()
	defer srv.Close()

	client, err := New(srv.URL, nil) // nil → default client picks up ONEVOICE_MTLS_* env
	require.NoError(t, err)
	got, err := client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)
	assert.Equal(t, "mtls-ok", got.AccessToken)
}

// TestGetToken_mTLS_RejectsUnsignedClient — server requires client cert;
// client presents a cert signed by a
// DIFFERENT CA. Handshake fails → GetToken returns an error chained with
// ErrTransient (network-class failure — TLS handshake failure is a
// transport problem, not a 4xx from the API).
func TestGetToken_mTLS_RejectsUnsignedClient(t *testing.T) {
	h := mtlsTestHarness(t)
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, h.caCertPath)
	// Client presents a rogue-signed cert.
	t.Setenv(mtls.EnvCertPath, h.rogueClientCert)
	t.Setenv(mtls.EnvKeyPath, h.rogueClientKey)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{AccessToken: "never-served"}))
	}))
	srv.TLS = h.serverTLSConfig
	srv.StartTLS()
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransient),
		"unsigned client cert handshake failure must surface as ErrTransient; got %v", err)
}

// TestGetToken_mTLS_PlainHTTPRejected — server is TLS-only on https://;
// client makes plain http:// request →
// server cannot serve the request (returns Go's stdlib "client sent an
// HTTP request to an HTTPS server" 400). The test asserts the call fails
// with NO success path — the plain-HTTP request CANNOT extract a token
// from the TLS-only listener. This locks the invariant: tokenclient is
// never tricked into a non-TLS round trip when the API is TLS-only.
//
// Note: the error is NOT ErrTransient because the response IS a real HTTP
// 400 (not a network failure or 5xx). The classification stays "unexpected
// non-sentinel 4xx" per the existing TestGetToken_UnexpectedNon5xx_NoSentinel
// contract. What matters here is that no successful TokenResponse leaks
// through a misconfigured plain-HTTP client.
func TestGetToken_mTLS_PlainHTTPRejected(t *testing.T) {
	h := mtlsTestHarness(t)
	t.Setenv(mtls.EnvEnabled, "")

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{AccessToken: "never"}))
	}))
	srv.TLS = h.serverTLSConfig
	srv.StartTLS()
	defer srv.Close()

	// srv.URL is https://...; strip to http:// to simulate a plain-HTTP
	// client talking to a TLS-only listener.
	plainURL := "http://" + strings.TrimPrefix(srv.URL, "https://")

	// Use a short timeout so the test doesn't hang on a hopeless connection.
	client, err := New(plainURL, &http.Client{Timeout: 2 * time.Second})
	require.NoError(t, err)
	tok, err := client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err, "TLS-only listener must NEVER serve a useful response to a plain-HTTP client")
	assert.Nil(t, tok, "no token should leak through a plain-HTTP attempt against a TLS-only listener")
	// Sanity: it's NOT 404 / 410 / success — the server cannot satisfy a
	// plain-HTTP request on a TLS port.
	assert.False(t, errors.Is(err, ErrIntegrationNotFound))
	assert.False(t, errors.Is(err, ErrTokenExpired))
}

func TestGetToken_CacheEvictsExpiringSoon(t *testing.T) {
	var callCount atomic.Int32
	expiresAt := time.Now().Add(30 * time.Second) // expires in 30s (< 5 min threshold)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{
			AccessToken: "tok",
			ExpiresAt:   &expiresAt,
		}))
	}))
	defer srv.Close()

	client, err := New(srv.URL, nil)
	require.NoError(t, err)

	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	// Second call should fetch again because token expires within 1 minute
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	assert.Equal(t, int32(2), callCount.Load(), "should call API twice since token is expiring soon")
}

// TestNew_FailClosedOnMissingCertFiles — ONEVOICE_MTLS_ENABLED=true with all
// path env vars pointing at nonexistent files must return an error, not a
// degraded plain-HTTP client.
func TestNew_FailClosedOnMissingCertFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, filepath.Join(dir, "missing-ca.crt"))
	t.Setenv(mtls.EnvCertPath, filepath.Join(dir, "missing-client.crt"))
	t.Setenv(mtls.EnvKeyPath, filepath.Join(dir, "missing-client.key"))

	c, err := New("https://api.test", nil)
	require.Error(t, err, "mTLS enabled + missing cert files must fail closed")
	assert.Nil(t, c, "no client may be returned on mTLS load failure")
	assert.Contains(t, strings.ToLower(err.Error()), "mtls",
		"error must identify mtls as the root cause; got %v", err)
}

// TestNew_FailClosedOnUnreadableCA — ONEVOICE_MTLS_ENABLED=true with CA path
// pointing at a directory instead of a file must return an error.
func TestNew_FailClosedOnUnreadableCA(t *testing.T) {
	h := mtlsTestHarness(t)
	dir := t.TempDir() // directory in place of CA file
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, dir)
	t.Setenv(mtls.EnvCertPath, h.clientCertPath)
	t.Setenv(mtls.EnvKeyPath, h.clientKeyPath)

	c, err := New("https://api.test", nil)
	require.Error(t, err, "unreadable CA must fail closed")
	assert.Nil(t, c)
	assert.Contains(t, strings.ToLower(err.Error()), "mtls")
}

// TestNew_NoMTLSAllowsPlain — when mTLS is disabled the legacy plain
// transport path is preserved so unit tests + dev keep working.
func TestNew_NoMTLSAllowsPlain(t *testing.T) {
	t.Setenv(mtls.EnvEnabled, "false")
	t.Setenv(mtls.EnvCAPath, "")
	t.Setenv(mtls.EnvCertPath, "")
	t.Setenv(mtls.EnvKeyPath, "")

	c, err := New("http://api.test", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

// TestNew_CallerProvidedClient_BypassesMTLSCheck — when the caller passes a
// non-nil http.Client, the mTLS env state is ignored entirely so unit tests
// that inject an httptest client never trip the boot-time cert check.
func TestNew_CallerProvidedClient_BypassesMTLSCheck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, filepath.Join(dir, "missing-ca.crt"))
	t.Setenv(mtls.EnvCertPath, filepath.Join(dir, "missing-client.crt"))
	t.Setenv(mtls.EnvKeyPath, filepath.Join(dir, "missing-client.key"))

	custom := &http.Client{Timeout: time.Second}
	c, err := New("https://api.test", custom)
	require.NoError(t, err, "caller-supplied client must bypass mTLS env loading")
	require.NotNil(t, c)
}
