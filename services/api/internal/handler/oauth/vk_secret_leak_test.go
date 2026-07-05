package oauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// captureSlog redirects the default logger through the production RedactHandler
// into buf for the duration of the test, mirroring the real logging pipeline
// (the "error" key is deliberately NOT on the deny-list, so redaction of a
// secret-in-URL must happen at the call site via redactURLErr).
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(logger.NewRedactHandler(jsonHandler, nil)))
	return &buf
}

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

// TestVKCallback_TransportError_NoClientSecretLeak drives a transport failure
// through VKCallback's token exchange, whose URL embeds client_secret in the
// query. The handler logs the *url.Error via slog; the captured log output
// must carry neither the client_secret value nor the client_secret= query
// prefix. Reverting the redactURLErr wrap puts the full URL (incl.
// client_secret=<secret>) into the log line and this fails.
func TestVKCallback_TransportError_NoClientSecretLeak(t *testing.T) {
	const clientSecret = "SUPER_SECRET_VK_CLIENT_SECRET"

	buf := captureSlog(t)

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockOAuth := new(MockOAuthStateService)
	stateData := &service.OAuthStateData{
		BusinessID: uuid.New(),
		Platform:   "vk",
		Nonce:      "csrf-nonce",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-state").Return(stateData, nil)

	cfg := OAuthConfig{
		VKClientID:     "client_id",
		VKClientSecret: clientSecret,
		VKRedirectURI:  "https://example.com/oauth/vk/callback",
		vkTokenBaseURL: "https://oauth.vk.example",
	}
	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), cfg, failingVKClient(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback?code=auth_code&state=valid-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "csrf-nonce"})
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect on transport failure, got %d: %s", rr.Code, rr.Body.String())
	}
	logged := buf.String()
	if strings.Contains(logged, clientSecret) {
		t.Errorf("VKCallback log leaked the client_secret value %q: %s", clientSecret, logged)
	}
	if strings.Contains(logged, "client_secret=") {
		t.Errorf("VKCallback log leaked the query string (client_secret=): %s", logged)
	}
}

// TestVKCommunities_TransportError_NoAccessTokenLeak drives a transport failure
// through VKCommunities' groups.get call, whose URL embeds the user access_token
// in the query. The handler logs the *url.Error via slog; the captured log
// output must carry neither the access_token value nor the access_token= query
// prefix. Reverting the redactURLErr wrap puts the full URL (incl.
// access_token=<secret>) into the log line and this fails.
func TestVKCommunities_TransportError_NoAccessTokenLeak(t *testing.T) {
	const userToken = "vk1.a.USER_ACCESS_TOKEN_SECRET"

	buf := captureSlog(t)

	businessID := uuid.New()
	userID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	if err := redisClient.Set(context.Background(), fmt.Sprintf("vk_temp_token:%s", businessID.String()), userToken, 0).Err(); err != nil {
		t.Fatalf("failed to seed temp token: %v", err)
	}

	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, failingVKClient(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/communities", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.VKCommunities(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on transport failure, got %d: %s", rr.Code, rr.Body.String())
	}
	logged := buf.String()
	if strings.Contains(logged, userToken) {
		t.Errorf("VKCommunities log leaked the access_token value %q: %s", userToken, logged)
	}
	if strings.Contains(logged, "access_token=") {
		t.Errorf("VKCommunities log leaked the query string (access_token=): %s", logged)
	}
}
