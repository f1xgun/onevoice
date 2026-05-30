package middleware_test

import (
	"crypto/tls"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto/x509"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// newRequestWithPeerCert builds a request whose TLS connection state has a
// single peer certificate with the given Common Name. Used to simulate a
// post-handshake mTLS client identity from inside an httptest harness.
func newRequestWithPeerCert(method, target, cn string) *http.Request {
	req := httptest.NewRequest(method, target, http.NoBody)
	if cn == "" {
		return req
	}
	cert := &x509.Certificate{
		Subject: pkix.Name{CommonName: cn},
	}
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}
	return req
}

// TestRequireServiceIdentity_AllowsListedCN: peer cert CN is in the allowlist.
func TestRequireServiceIdentity_AllowsListedCN(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")

	var capturedIdentity string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedIdentity = middleware.ServiceIdentityFromContext(r.Context())
	})
	mw := middleware.RequireServiceIdentity([]string{"orchestrator", "api"}, nil)(next)

	req := newRequestWithPeerCert(http.MethodPost, "/internal/v1/billing/usage_logs", "orchestrator")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "orchestrator", capturedIdentity)
}

// TestRequireServiceIdentity_RejectsUnlistedCN: peer cert CN is NOT in the allowlist.
func TestRequireServiceIdentity_RejectsUnlistedCN(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	mw := middleware.RequireServiceIdentity([]string{"orchestrator", "api"}, nil)(next)

	req := newRequestWithPeerCert(http.MethodPost, "/internal/v1/billing/usage_logs", "attacker")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, called, "next handler must not run on rejected identity")
}

// TestRequireServiceIdentity_RejectsMissingCert_WhenMTLSEnabled.
func TestRequireServiceIdentity_RejectsMissingCert_WhenMTLSEnabled(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	mw := middleware.RequireServiceIdentity([]string{"orchestrator"}, nil)(next)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/billing/usage_logs", http.NoBody)
	// req.TLS == nil intentionally.
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, called, "next handler must not run when mtls required but no cert")
}

// TestRequireServiceIdentity_AllowsMissingCert_WhenMTLSDisabled.
func TestRequireServiceIdentity_AllowsMissingCert_WhenMTLSDisabled(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "false")

	var capturedIdentity string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedIdentity = middleware.ServiceIdentityFromContext(r.Context())
	})
	mw := middleware.RequireServiceIdentity([]string{"orchestrator"}, nil)(next)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/billing/usage_logs", http.NoBody)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "system", capturedIdentity)
}

// TestServiceIdentityFromContext_DefaultEmpty: bare ctx returns empty string.
func TestServiceIdentityFromContext_DefaultEmpty(t *testing.T) {
	got := middleware.ServiceIdentityFromContext(t.Context())
	require.Equal(t, "", got)
}

// TestRequireServiceIdentity_CaseSensitiveMatch: CN match is case-sensitive.
func TestRequireServiceIdentity_CaseSensitiveMatch(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	mw := middleware.RequireServiceIdentity([]string{"orchestrator"}, nil)(next)

	req := newRequestWithPeerCert(http.MethodPost, "/internal/v1/billing/usage_logs", "Orchestrator")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, called)
}
