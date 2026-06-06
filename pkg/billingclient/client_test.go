package billingclient

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
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/mtls"
)

// mustNewTestClient wraps New for tests that want a Client and treat any
// construction error as fatal — the bulk of the suite asserts behavior of
// an already-built client, so error-handling boilerplate at every call site
// would obscure intent. Tests that exercise the fail-closed contract call
// New directly.
func mustNewTestClient(t *testing.T, baseURL string, hc *http.Client) *Client {
	t.Helper()
	c, err := New(baseURL, hc)
	require.NoError(t, err)
	return c
}

// disableMTLSEnv resets the ONEVOICE_MTLS_* triplet for plain-HTTP tests.
// Without it, a stray process-level export of ONEVOICE_MTLS_ENABLED=true
// would steer the default http.Client into a TLS handshake against an
// httptest.NewServer (plain HTTP), masking real assertions.
func disableMTLSEnv(t *testing.T) {
	t.Helper()
	t.Setenv(mtls.EnvEnabled, "")
	t.Setenv(mtls.EnvCAPath, "")
	t.Setenv(mtls.EnvCertPath, "")
	t.Setenv(mtls.EnvKeyPath, "")
}

// sampleLog returns a valid UsageLog for happy-path tests.
func sampleLog(t *testing.T) *llm.UsageLog {
	t.Helper()
	return &llm.UsageLog{
		ID:              uuid.New(),
		BusinessID:      uuid.New(),
		UserID:          uuid.New(),
		ConversationID:  "conv-abc",
		RequestID:       "req-xyz",
		Model:           "claude-3-5-sonnet",
		Provider:        "anthropic",
		InputTokens:     1000,
		OutputTokens:    200,
		ProviderCostUSD: 0.0033,
		CommissionUSD:   0.00066,
		UserCostUSD:     0.00396,
		UserTier:        "free",
		CreatedAt:       time.Now().UTC(),
	}
}

// counterValue returns the current value of BillingPostFailures{reason=r}.
// Used to assert metric deltas without depending on prior test ordering.
func counterValue(reason string) float64 {
	return testutil.ToFloat64(metrics.BillingPostFailures.WithLabelValues(reason))
}

func TestLogUsage_Success_204(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	var seenBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/v1/billing/usage_logs", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var got llm.UsageLog
		require.NoError(t, json.Unmarshal(body, &got))
		seenBody.Store(&got)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), log)
	require.NoError(t, err)

	gotPtr, ok := seenBody.Load().(*llm.UsageLog)
	require.True(t, ok, "server handler should have been invoked")
	assert.Equal(t, log.BusinessID, gotPtr.BusinessID)
	assert.Equal(t, log.Model, gotPtr.Model)
	assert.Equal(t, log.InputTokens, gotPtr.InputTokens)
	assert.Equal(t, log.ProviderCostUSD, gotPtr.ProviderCostUSD)
}

func TestLogUsage_Success_200(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), log)
	require.NoError(t, err)
}

func TestLogUsage_BadRequest_IsInvalidPayload(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	before := counterValue("invalid_payload")
	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), log)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidPayload),
		"400 must surface as ErrInvalidPayload; got %v", err)
	assert.Equal(t, before+1, counterValue("invalid_payload"))
}

func TestLogUsage_ServerError_IsTransient(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			before := counterValue("transient")
			client := mustNewTestClient(t, srv.URL, nil)
			err := client.LogUsage(context.Background(), log)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrTransient),
				"%d must surface as ErrTransient; got %v", status, err)
			assert.Equal(t, before+1, counterValue("transient"))
		})
	}
}

func TestLogUsage_NetworkError_IsTransient(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	before := counterValue("transient")
	client := mustNewTestClient(t, "http://"+addr, &http.Client{Timeout: 500 * time.Millisecond})
	err = client.LogUsage(context.Background(), log)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransient),
		"network failure must surface as ErrTransient; got %v", err)
	assert.Equal(t, before+1, counterValue("transient"))
}

func TestLogUsage_NilLog_IsInvalidPayload(t *testing.T) {
	disableMTLSEnv(t)

	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	before := counterValue("invalid_payload")
	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidPayload))
	assert.Contains(t, err.Error(), "nil log")
	assert.False(t, hit.Load(), "no HTTP call should be made when log is nil")
	assert.Equal(t, before+1, counterValue("invalid_payload"))
}

func TestLogUsage_NilBusinessID_IsInvalidPayload(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)
	log.BusinessID = uuid.Nil

	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	before := counterValue("invalid_payload")
	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), log)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidPayload))
	assert.Contains(t, err.Error(), "business_id required")
	assert.False(t, hit.Load(), "no HTTP call should be made when business_id is nil")
	assert.Equal(t, before+1, counterValue("invalid_payload"))
}

func TestLogUsage_ContextCancelled(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	before := counterValue("transient")
	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(ctx, log)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"canceled context must chain context.Canceled; got %v", err)
	assert.True(t, errors.Is(err, ErrTransient),
		"canceled context is network-class failure → ErrTransient; got %v", err)
	assert.Equal(t, before+1, counterValue("transient"))
}

func TestLogUsage_UnexpectedStatus_NoSentinel(t *testing.T) {
	disableMTLSEnv(t)
	log := sampleLog(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	before := counterValue("unexpected_status")
	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), log)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrTransient),
		"unexpected non-5xx, non-4xx must NOT chain ErrTransient — likely proxy misconfig")
	assert.False(t, errors.Is(err, ErrInvalidPayload))
	assert.Equal(t, before+1, counterValue("unexpected_status"))
}

func TestNew_NilHTTPClient_DefaultTimeout(t *testing.T) {
	disableMTLSEnv(t)
	c := mustNewTestClient(t, "http://example.invalid", nil)
	require.NotNil(t, c)
	require.NotNil(t, c.httpClient)
	assert.Equal(t, 10*time.Second, c.httpClient.Timeout)
}

// ---------------------------------------------------------------------
// GetDailySpend tests
// ---------------------------------------------------------------------

// TestGetDailySpend_Success_200 — happy path: server returns the JSON envelope
// and the client surfaces the float64 verbatim.
func TestGetDailySpend_Success_200(t *testing.T) {
	disableMTLSEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/internal/v1/billing/daily_spend", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"daily_spend_usd": 0.42}`))
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	got, err := client.GetDailySpend(context.Background(), uuid.New(), time.Now().UTC())
	require.NoError(t, err)
	assert.InDelta(t, 0.42, got, 1e-9)
}

// TestGetDailySpend_ServerError_IsTransient — 5xx surfaces as ErrTransient.
func TestGetDailySpend_ServerError_IsTransient(t *testing.T) {
	disableMTLSEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	_, err := client.GetDailySpend(context.Background(), uuid.New(), time.Now().UTC())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransient), "got %v", err)
}

// TestGetDailySpend_BadRequest_IsInvalidPayload — 400 surfaces as ErrInvalidPayload.
func TestGetDailySpend_BadRequest_IsInvalidPayload(t *testing.T) {
	disableMTLSEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	_, err := client.GetDailySpend(context.Background(), uuid.New(), time.Now().UTC())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidPayload), "got %v", err)
}

// TestGetDailySpend_NetworkError_IsTransient — connection refused → ErrTransient.
func TestGetDailySpend_NetworkError_IsTransient(t *testing.T) {
	disableMTLSEnv(t)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	client := mustNewTestClient(t, "http://"+addr, &http.Client{Timeout: 500 * time.Millisecond})
	_, err = client.GetDailySpend(context.Background(), uuid.New(), time.Now().UTC())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransient), "got %v", err)
}

// TestGetDailySpend_MalformedBody_IsInvalidPayload — 200 with non-JSON body
// surfaces as ErrInvalidPayload because the response is malformed.
func TestGetDailySpend_MalformedBody_IsInvalidPayload(t *testing.T) {
	disableMTLSEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	_, err := client.GetDailySpend(context.Background(), uuid.New(), time.Now().UTC())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidPayload), "got %v", err)
}

// TestGetDailySpend_URLComposition — query params carry the UUID and the
// UTC date in YYYY-MM-DD form regardless of the input time zone.
func TestGetDailySpend_URLComposition(t *testing.T) {
	disableMTLSEnv(t)

	var seenQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery.Store(r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"daily_spend_usd": 0}`))
	}))
	defer srv.Close()

	bizID := uuid.New()
	loc, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	day := time.Date(2026, 5, 30, 14, 0, 0, 0, loc)

	client := mustNewTestClient(t, srv.URL, nil)
	_, err = client.GetDailySpend(context.Background(), bizID, day)
	require.NoError(t, err)

	q, ok := seenQuery.Load().(string)
	require.True(t, ok)
	assert.Contains(t, q, "business_id="+bizID.String())
	assert.Contains(t, q, "date=2026-05-30")
}

// TestGetDailySpend_UnexpectedStatus_NoSentinel — 418 surfaces as a plain
// error without chaining ErrTransient or ErrInvalidPayload so callers fail
// closed instead of retrying.
func TestGetDailySpend_UnexpectedStatus_NoSentinel(t *testing.T) {
	disableMTLSEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	_, err := client.GetDailySpend(context.Background(), uuid.New(), time.Now().UTC())
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrTransient))
	assert.False(t, errors.Is(err, ErrInvalidPayload))
}

func TestLogUsage_mTLS_WhenEnabled(t *testing.T) {
	h := newMTLSHarness(t)
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, h.caCertPath)
	t.Setenv(mtls.EnvCertPath, h.clientCertPath)
	t.Setenv(mtls.EnvKeyPath, h.clientKeyPath)

	var hit atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/v1/billing/usage_logs", r.URL.Path)
		hit.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = h.serverTLSConfig
	srv.StartTLS()
	defer srv.Close()

	client := mustNewTestClient(t, srv.URL, nil)
	err := client.LogUsage(context.Background(), sampleLog(t))
	require.NoError(t, err)
	assert.True(t, hit.Load(), "mTLS handshake should succeed and request should reach handler")
}

// mtlsTestHarness — ephemeral CA + server cert + client cert in t.TempDir().
// Used by Test 11. Trimmed copy of tokenclient's harness.
type mtlsHarness struct {
	caCertPath      string
	clientCertPath  string
	clientKeyPath   string
	serverTLSConfig *tls.Config
}

func newMTLSHarness(t *testing.T) *mtlsHarness {
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

	makeLeaf := func(cn string, parent *x509.Certificate, parentKey *rsa.PrivateKey, eku []x509.ExtKeyUsage) ([]byte, *rsa.PrivateKey) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  eku,
			DNSNames:     []string{"localhost", "api"},
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

	caCert, caKey, caDER := makeCA("dev-ca")
	serverDER, serverKey := makeLeaf("api", caCert, caKey, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientDER, clientKey := makeLeaf("orchestrator", caCert, caKey, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})

	h := &mtlsHarness{
		caCertPath:     filepath.Join(dir, "ca.crt"),
		clientCertPath: filepath.Join(dir, "client.crt"),
		clientKeyPath:  filepath.Join(dir, "client.key"),
	}
	serverCertPath := filepath.Join(dir, "server.crt")
	serverKeyPath := filepath.Join(dir, "server.key")

	write(h.caCertPath, "CERTIFICATE", caDER)
	write(serverCertPath, "CERTIFICATE", serverDER)
	writeKey(serverKeyPath, serverKey)
	write(h.clientCertPath, "CERTIFICATE", clientDER)
	writeKey(h.clientKeyPath, clientKey)

	srvCfg, err := mtls.LoadServerTLSConfig(mtls.ServiceCertPaths{
		CACertPath: h.caCertPath,
		CertPath:   serverCertPath,
		KeyPath:    serverKeyPath,
	})
	require.NoError(t, err)
	h.serverTLSConfig = srvCfg
	return h
}

// TestNew_FailClosedOnMissingCertFiles — billingclient mirrors tokenclient:
// ONEVOICE_MTLS_ENABLED=true with nonexistent cert paths returns an error,
// no silent plain-HTTP fallback.
func TestNew_FailClosedOnMissingCertFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, filepath.Join(dir, "missing-ca.crt"))
	t.Setenv(mtls.EnvCertPath, filepath.Join(dir, "missing-client.crt"))
	t.Setenv(mtls.EnvKeyPath, filepath.Join(dir, "missing-client.key"))

	c, err := New("https://api.test", nil)
	require.Error(t, err)
	assert.Nil(t, c)
}

// TestNew_FailClosedOnUnreadableCA — billingclient mirrors tokenclient: a CA
// path that resolves to a directory must return an error.
func TestNew_FailClosedOnUnreadableCA(t *testing.T) {
	h := newMTLSHarness(t)
	dir := t.TempDir()
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, dir)
	t.Setenv(mtls.EnvCertPath, h.clientCertPath)
	t.Setenv(mtls.EnvKeyPath, h.clientKeyPath)

	c, err := New("https://api.test", nil)
	require.Error(t, err)
	assert.Nil(t, c)
}

// TestNew_CallerProvidedClient_BypassesMTLSCheck — caller-supplied http.Client
// bypasses mTLS env loading even when ONEVOICE_MTLS_ENABLED=true points at
// nonexistent files.
func TestNew_CallerProvidedClient_BypassesMTLSCheck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(mtls.EnvEnabled, "true")
	t.Setenv(mtls.EnvCAPath, filepath.Join(dir, "missing-ca.crt"))
	t.Setenv(mtls.EnvCertPath, filepath.Join(dir, "missing-client.crt"))
	t.Setenv(mtls.EnvKeyPath, filepath.Join(dir, "missing-client.key"))

	c, err := New("https://api.test", &http.Client{Timeout: time.Second})
	require.NoError(t, err)
	require.NotNil(t, c)
}
