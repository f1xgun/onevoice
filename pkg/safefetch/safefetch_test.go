package safefetch

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "public https host", rawURL: "https://example.com/photo.jpg", wantErr: false},
		{name: "public ip literal", rawURL: "https://93.184.216.34/photo.jpg", wantErr: false},
		{name: "http scheme rejected", rawURL: "http://example.com/photo.jpg", wantErr: true},
		{name: "ftp scheme rejected", rawURL: "ftp://example.com/photo.jpg", wantErr: true},
		{name: "empty host rejected", rawURL: "https:///photo.jpg", wantErr: true},
		{name: "loopback ipv4 rejected", rawURL: "https://127.0.0.1/photo.jpg", wantErr: true},
		{name: "loopback ipv6 rejected", rawURL: "https://[::1]/photo.jpg", wantErr: true},
		{name: "metadata link-local rejected", rawURL: "https://169.254.169.254/latest/meta-data", wantErr: true},
		{name: "private 10/8 rejected", rawURL: "https://10.0.0.1/photo.jpg", wantErr: true},
		{name: "private 172.16/12 rejected", rawURL: "https://172.16.0.1/photo.jpg", wantErr: true},
		{name: "private 192.168/16 rejected", rawURL: "https://192.168.1.1/photo.jpg", wantErr: true},
		{name: "cgnat 100.64/10 rejected", rawURL: "https://100.64.0.1/photo.jpg", wantErr: true},
		{name: "this-host 0.0.0.1 rejected", rawURL: "https://0.0.0.1/photo.jpg", wantErr: true},
		{name: "this-host 0.10.0.1 mid-range rejected", rawURL: "https://0.10.0.1/photo.jpg", wantErr: true},
		{name: "unique local ipv6 rejected", rawURL: "https://[fc00::1]/photo.jpg", wantErr: true},
		{name: "link-local ipv6 rejected", rawURL: "https://[fe80::1]/photo.jpg", wantErr: true},
		{name: "unparseable url rejected", rawURL: "https://exa mple.com/photo.jpg", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateURL(%q) = nil, want error", tt.rawURL)
				}
				if !errors.Is(err, ErrUnsafeURL) {
					t.Fatalf("ValidateURL(%q) error = %v, want ErrUnsafeURL", tt.rawURL, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateURL(%q) = %v, want nil", tt.rawURL, err)
			}
		})
	}
}

func TestGetRejectsNonHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	_, _, err := New(Options{}).Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected Get to reject plain-http test server URL")
	}
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("expected ErrUnsafeURL, got: %v", err)
	}
}

// TestGetScreensResolvedPeerIP drives the dial Control hook directly: a host
// that is a DNS name (not an IP literal, so ValidateURL alone cannot screen it)
// resolves to a loopback address. safeControl must refuse that resolved peer,
// which is what defeats DNS rebinding. Calling safeControl with the post-
// resolution address is exactly what the net.Dialer does after DNS, so this is
// hermetic and does not depend on real DNS.
func TestGetScreensResolvedPeerIP(t *testing.T) {
	resolved := []string{
		"127.0.0.1:443",
		"169.254.169.254:80",
		"10.0.0.1:443",
		"[::1]:443",
		"100.64.0.1:443",
	}
	for _, addr := range resolved {
		if err := safeControl("tcp", addr, nil); err == nil {
			t.Fatalf("safeControl allowed disallowed resolved peer %q", addr)
		} else if !errors.Is(err, ErrUnsafeURL) {
			t.Fatalf("safeControl(%q) = %v, want ErrUnsafeURL", addr, err)
		}
	}
	if err := safeControl("tcp", "93.184.216.34:443", nil); err != nil {
		t.Fatalf("safeControl rejected a public resolved peer: %v", err)
	}
}

// TestGetScreensResolvedHostnameEndToEnd exercises the full dial path: a DNS
// hostname that resolves to loopback must be refused by the Control hook inside
// a real Get call. localtest.me resolves to 127.0.0.1; if DNS is unavailable in
// the environment, the dial still fails with a network error (not a success),
// so the request is never satisfied either way.
func TestGetScreensResolvedHostnameEndToEnd(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	target := "https://localtest.me:" + port + "/secret"
	_, _, err = New(Options{}).Get(context.Background(), target)
	if err == nil {
		t.Fatal("expected Get to refuse a hostname resolving to loopback")
	}
}

// TestGetRevalidatesRedirect proves a redirect from an allowed host to a
// disallowed (loopback) target is blocked by CheckRedirect.
func TestGetRevalidatesRedirect(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, w2r(), "https://127.0.0.1/internal", http.StatusFound)
	}))
	defer srv.Close()

	f := New(Options{})
	if transport, ok := f.client.Transport.(*http.Transport); ok {
		transport.TLSClientConfig = testServerTLSConfig(srv)
	} else {
		t.Fatalf("unexpected transport type %T", f.client.Transport)
	}
	_, _, err := f.Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected Get to reject a redirect to a loopback address")
	}
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("expected ErrUnsafeURL on redirect, got: %v", err)
	}
}

// w2r is a tiny shim so http.Redirect (which wants *http.Request only for
// relative-URL resolution) can be called with an absolute Location.
func w2r() *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "https://allowed.example/", http.NoBody)
	return r
}

func TestGetAllowsPublicHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xffimage-bytes"))
	}))
	defer srv.Close()

	// The test server binds to 127.0.0.1, which the Control hook would refuse.
	// Route the dial through the server's own listener address while still
	// exercising ValidateURL + body cap against a public-looking hostname.
	f := newFetcherDialingTo(t, srv)
	body, ct, err := f.Get(context.Background(), "https://images.example.test/ok.jpg")
	if err != nil {
		t.Fatalf("expected public https fetch to succeed, got: %v", err)
	}
	if !strings.HasPrefix(ct, "image/") {
		t.Fatalf("content-type = %q, want image/*", ct)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty body")
	}
}

func TestGetCapsBodySize(t *testing.T) {
	const capBytes int64 = 1024
	oversized := bytes.Repeat([]byte("a"), int(capBytes)+512)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(oversized)
	}))
	defer srv.Close()

	f := newFetcherDialingTo(t, srv)
	f.maxBytes = capBytes
	_, _, err := f.Get(context.Background(), "https://images.example.test/big.jpg")
	if err == nil {
		t.Fatal("expected Get to reject an over-cap body")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-cap error, got: %v", err)
	}
}

// newFetcherDialingTo returns a Fetcher that resolves any hostname to the test
// server's loopback listener, so tests can assert ValidateURL/body behavior
// against a public-looking hostname without tripping the IP screen on the
// listener itself. The screen is exercised separately in the resolved-peer and
// redirect tests. The test server's self-signed certificate is trusted via a
// dedicated cert pool rather than disabling verification, so the production
// fetcher keeps full TLS verification.
func newFetcherDialingTo(t *testing.T, srv *httptest.Server) *Fetcher {
	t.Helper()
	srvURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	f := New(Options{})
	transport, ok := f.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", f.client.Transport)
	}
	transport.TLSClientConfig = testServerTLSConfig(srv)
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srvURL.Host)
	}
	return f
}

// testServerTLSConfig returns a *tls.Config that trusts the given httptest TLS
// server's self-signed certificate. Tests use this to exercise the real fetcher
// (with verification enabled) against a loopback test server instead of
// disabling certificate verification. ServerName is pinned to a name the
// httptest certificate covers (example.com) because requests target a different
// public-looking hostname than the cert's SAN.
func testServerTLSConfig(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12}
}
