package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// closingVKServer returns an httptest server that hijacks and immediately
// closes every connection without writing a response. h.httpClient.Do then
// returns a *url.Error carrying the full request URL — which is exactly the
// transport-failure path that used to leak the VK service key / community
// token embedded in the query string.
func closingVKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("ResponseWriter does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		_ = conn.Close()
	}))
	return srv
}

// assertNoSecret fails when s leaks the query string or the secret value.
func assertNoSecret(t *testing.T, where, s, secret string) {
	t.Helper()
	if strings.Contains(s, "access_token=") {
		t.Errorf("%s leaks query string (access_token=): %q", where, s)
	}
	if strings.Contains(s, secret) {
		t.Errorf("%s leaks the secret value %q: %q", where, secret, s)
	}
}

// TestResolveVKGroupID_TransportError_NoServiceKeyLeak drives a transport
// failure through resolveVKGroupID (which embeds the VK service key in the
// query) and asserts the returned error string carries neither the query
// string nor the key. Reverting redactURLErr reintroduces the full URL
// (incl. access_token=<VKServiceKey>) into the *url.Error and this fails.
func TestResolveVKGroupID_TransportError_NoServiceKeyLeak(t *testing.T) {
	srv := closingVKServer(t)
	defer srv.Close()

	const serviceKey = "SUPER_SECRET_VK_SERVICE_KEY"
	cfg := ConnectConfig{vkAPIBaseURL: srv.URL, VKServiceKey: serviceKey}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, srv.Client())

	// Non-numeric input forces the VK lookup (numeric ids short-circuit).
	_, err := h.resolveVKGroupID(context.Background(), "onevoice")
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	assertNoSecret(t, "resolveVKGroupID error", err.Error(), serviceKey)
}

// TestProbeVKCommunityToken_TransportError_NoTokenLeak drives a transport
// failure through probeVKCommunityToken (which embeds the pasted community
// token in the query) and asserts neither the returned error nor the value
// that ConnectVK would log carries the token. Reverting redactURLErr fails.
func TestProbeVKCommunityToken_TransportError_NoTokenLeak(t *testing.T) {
	srv := closingVKServer(t)
	defer srv.Close()

	const pastedToken = "vk1.a.SUPER_SECRET_COMMUNITY_TOKEN"
	cfg := ConnectConfig{vkAPIBaseURL: srv.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, srv.Client())

	_, _, err := h.probeVKCommunityToken(context.Background(), pastedToken, "")
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	assertNoSecret(t, "probeVKCommunityToken error", err.Error(), pastedToken)
}

// TestResolveVKGroupID_ViaProbe_TransportError_NoServiceKeyLeak exercises the
// full wrap chain ErrVKCommunityResolveFailed -> resolveVKGroupID transport
// error, the exact value ConnectVK turns into the user-facing `detail`. The
// resulting error string must not carry the service key.
func TestResolveVKGroupID_ViaProbe_TransportError_NoServiceKeyLeak(t *testing.T) {
	srv := closingVKServer(t)
	defer srv.Close()

	const serviceKey = "SUPER_SECRET_VK_SERVICE_KEY"
	cfg := ConnectConfig{vkAPIBaseURL: srv.URL, VKServiceKey: serviceKey}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, srv.Client())

	_, _, err := h.probeVKCommunityToken(context.Background(), "tok", "onevoice")
	if err == nil {
		t.Fatal("expected a resolve transport error, got nil")
	}
	assertNoSecret(t, "probeVKCommunityToken(resolve) error", err.Error(), serviceKey)
}

// TestFetchVKCommunityName_TransportError_NoTokenLeak drives a transport
// failure through fetchVKCommunityName (logged at INFO with a DECRYPTED
// community token in the query) and asserts the bare returned error carries
// neither the query string nor the token. Reverting redactURLErr fails.
func TestFetchVKCommunityName_TransportError_NoTokenLeak(t *testing.T) {
	srv := closingVKServer(t)
	defer srv.Close()

	const decryptedToken = "vk1.a.DECRYPTED_COMMUNITY_TOKEN"
	cfg := ConnectConfig{vkAPIBaseURL: srv.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, srv.Client())

	_, err := h.fetchVKCommunityName(context.Background(), "236912172", decryptedToken)
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	assertNoSecret(t, "fetchVKCommunityName error", err.Error(), decryptedToken)
}
