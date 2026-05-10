package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/authz"
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
	})).Return("test-state-token", nil)

	cfg := OAuthConfig{
		VKClientID:    "my_vk_client",
		VKRedirectURI: "https://example.com/callback/vk",
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
	// no BusinessContext seeded
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
	req = req.WithContext(oauthBizCtx(businessID, userID /* no perms */))
	rr := httptest.NewRecorder()
	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// --- VK Callback Tests ---

func TestVKCallback_ExchangesCode(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	// Mock VK token exchange server
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
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "vk",
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
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "vk_step=select_community") {
		t.Errorf("expected redirect to /integrations?vk_step=select_community, got: %s", location)
	}

	// Verify temp token was stored in Redis
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
