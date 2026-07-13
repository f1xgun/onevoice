package oauth

import (
	"bytes"
	"context"
	"encoding/json"
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

// --- VK Auth URL Tests ---

func TestGetVKAuthURL_ReturnsURL(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockOAuth.On("GenerateState", mock.Anything, mock.MatchedBy(func(data service.OAuthStateData) bool {
		return data.UserID == userID && data.BusinessID == businessID && data.Platform == "vk"
	})).Return("test-state-token", "test-nonce", nil)

	cfg := OAuthConfig{
		VKClientID:     "my_vk_client",
		VKClientSecret: "my_vk_secret",
		VKRedirectURI:  "https://example.com/callback/vk",
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	authURL, ok := resp["url"]
	if !ok || authURL == "" {
		t.Fatal("expected 'url' in response")
	}

	if !strings.Contains(authURL, "oauth.vk.com") {
		t.Errorf("expected VK OAuth URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "my_vk_client") {
		t.Errorf("expected client_id in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "test-state-token") {
		t.Errorf("expected state in URL, got: %s", authURL)
	}

	mockOAuth.AssertExpectations(t)
}

// TestGetVKAuthURL_NoBusinessContext: handler returns 500 when middleware
// fails to seed BusinessContext (renamed from _Unauthorized — handler now
// trusts middleware to enforce auth).
func TestGetVKAuthURL_NoBusinessContext(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// TestGetVKAuthURL_Forbidden: BusinessContext present but missing
// PermIntegrationsConnect → 403.
func TestGetVKAuthURL_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// TestGetVKAuthURL_OAuthNotConfigured: BusinessContext + PermIntegrationsConnect
// present but VK OAuth not fully provisioned → 503 oauth_not_configured, so the
// frontend leads with the community-key paste flow instead of redirecting the
// owner to a broken VK page. The guard mirrors the PlatformAvailability.VK
// contract (both client id AND secret required), so a partial config (id set,
// secret empty) is caught too — otherwise the token exchange would fail
// mid-flow after the redirect.
func TestGetVKAuthURL_OAuthNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  OAuthConfig
	}{
		{"fully unset", OAuthConfig{}},
		{"client id set, secret empty", OAuthConfig{VKClientID: "my_vk_client"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			businessID := uuid.New()
			userID := uuid.New()
			h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), tc.cfg, nil, nil)

			req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
			req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
			rr := httptest.NewRecorder()
			h.GetVKAuthURL(rr, req)

			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
			}
			var resp map[string]string
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp["error"] != "oauth_not_configured" {
				t.Fatalf("expected error=oauth_not_configured, got %q", resp["error"])
			}
		})
	}
}

// --- VK Callback Tests ---

func TestVKCallback_ExchangesCode(t *testing.T) {
	businessID := uuid.New()
	_ = uuid.New()

	vkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "vk_access_token_123",
		})
	}))
	defer vkServer.Close()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		BusinessID: businessID,
		Platform:   "vk",
		Nonce:      "csrf-nonce",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-state").Return(stateData, nil)

	cfg := OAuthConfig{
		VKClientID:     "client_id",
		VKClientSecret: "client_secret",
		VKRedirectURI:  "https://example.com/callback/vk",
		vkTokenBaseURL: vkServer.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, vkServer.Client(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback?code=auth_code&state=valid-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "csrf-nonce"})
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "vk_step=select_community") {
		t.Errorf("expected redirect to /integrations?vk_step=select_community, got: %s", location)
	}

	tempKey := fmt.Sprintf("vk_temp_token:%s", businessID.String())
	storedToken, err := redisClient.Get(context.Background(), tempKey).Result()
	if err != nil {
		t.Fatalf("expected temp token in redis: %v", err)
	}
	if storedToken != "vk_access_token_123" {
		t.Errorf("expected stored token vk_access_token_123, got %s", storedToken)
	}

	mockOAuth.AssertExpectations(t)
}

func TestVKCallback_InvalidState(t *testing.T) {
	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("ValidateState", mock.Anything, "bad-state").Return(nil, fmt.Errorf("invalid or expired oauth state"))

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback?code=somecode&state=bad-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=invalid_state") {
		t.Errorf("expected redirect with error=invalid_state, got: %s", location)
	}
}

// TestVKCommunityCallback_NoGroupsBodyNotLogged guards against leaking a live
// VK access token into the logs. When VK returns a top-level user-token body
// (no community/group scope), the response carries a real access_token but
// len(Groups)==0, hitting the "no groups" branch. That branch must log only
// non-secret diagnostics — never the raw response body.
func TestVKCommunityCallback_NoGroupsBodyNotLogged(t *testing.T) {
	const secret = "vk1.a.SUPER_SECRET_VK_TOKEN"

	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(logger.NewRedactHandler(jsonHandler, nil)))

	businessID := uuid.New()

	vkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": secret,
			"user_id":      7,
		})
	}))
	defer vkServer.Close()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockOAuth := new(MockOAuthStateService)
	stateData := &service.OAuthStateData{
		BusinessID: businessID,
		Platform:   "vk",
		Nonce:      "csrf-nonce",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-state").Return(stateData, nil)

	cfg := OAuthConfig{
		VKClientID:     "client_id",
		VKClientSecret: "client_secret",
		VKRedirectURI:  "https://example.com/oauth/vk/callback",
		vkTokenBaseURL: vkServer.URL,
	}
	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), cfg, vkServer.Client(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/community-callback?code=auth_code&state=valid-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "csrf-nonce"})
	rr := httptest.NewRecorder()

	h.VKCommunityCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", rr.Code, rr.Body.String())
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=no_community_token") {
		t.Fatalf("expected no-groups branch (error=no_community_token), got: %s", location)
	}

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("log output leaked the VK access token: %s", buf.String())
	}
}

func TestVKCallback_MissingParams(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback", http.NoBody)
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=missing_params") {
		t.Errorf("expected error=missing_params in redirect, got: %s", location)
	}
}
