package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
)

// failingRoundTripper always fails the request. net/http wraps the returned
// error in a *url.Error whose .URL carries the full request URL, including the
// access_token=<secret> query embedded by the VK helpers — exactly the
// transport-failure path that used to leak the VK service key.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("forced transport failure")
}

// failingVKClient returns an http.Client that turns every request into a
// transport error, reproducing a VK outage / timeout without a live server.
func failingVKClient() *http.Client {
	return &http.Client{Transport: failingRoundTripper{}}
}

// assertNoVKSecret fails when s leaks the query string or the secret value.
func assertNoVKSecret(t *testing.T, where, s, secret string) {
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
	const serviceKey = "SUPER_SECRET_VK_SERVICE_KEY"
	cfg := OAuthConfig{VKServiceKey: serviceKey}
	h := NewOAuthHandler(
		new(MockOAuthStateService), new(MockOAuthIntegrationService),
		new(MockBusinessService), cfg, failingVKClient(), nil,
	)

	// Non-numeric input forces the VK lookup (numeric ids short-circuit).
	_, err := h.resolveVKGroupID(context.Background(), "onevoice")
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	assertNoVKSecret(t, "resolveVKGroupID error", err.Error(), serviceKey)
}

// TestVKCommunityAuthURL_TransportError_NoServiceKeyLeak exercises the exact
// client-facing path: VKCommunityAuthURL surfaces the resolveVKGroupID error
// verbatim via writeJSONError. On a VK transport failure the HTTP 400 body
// must not carry the query string or the service key. Reverting redactURLErr
// puts access_token=<VKServiceKey> into the response and this fails.
func TestVKCommunityAuthURL_TransportError_NoServiceKeyLeak(t *testing.T) {
	const serviceKey = "SUPER_SECRET_VK_SERVICE_KEY"
	cfg := OAuthConfig{VKServiceKey: serviceKey}
	h := NewOAuthHandler(
		new(MockOAuthStateService), new(MockOAuthIntegrationService),
		new(MockBusinessService), cfg, failingVKClient(), nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/community-auth-url?group_id=onevoice", http.NoBody)
	req = req.WithContext(oauthBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.VKCommunityAuthURL(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on VK transport failure, got %d: %s", rr.Code, rr.Body.String())
	}
	assertNoVKSecret(t, "VKCommunityAuthURL response", rr.Body.String(), serviceKey)
}

// TestFetchVKCommunityName_TransportError_NoTokenLeak drives a transport
// failure through fetchVKCommunityName (which embeds the DECRYPTED community
// token in the query) and asserts the returned error carries neither the
// query string nor the token. Reverting redactURLErr fails.
func TestFetchVKCommunityName_TransportError_NoTokenLeak(t *testing.T) {
	const decryptedToken = "vk1.a.DECRYPTED_COMMUNITY_TOKEN"
	h := NewOAuthHandler(
		new(MockOAuthStateService), new(MockOAuthIntegrationService),
		new(MockBusinessService), OAuthConfig{}, failingVKClient(), nil,
	)

	_, err := h.fetchVKCommunityName(context.Background(), "236912172", decryptedToken)
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	assertNoVKSecret(t, "fetchVKCommunityName error", err.Error(), decryptedToken)
}
