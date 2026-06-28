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
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// --- Google OAuth Tests ---

func newGoogleMockServer(t *testing.T, tokenResp map[string]interface{}, accounts, locations []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			_ = json.NewEncoder(w).Encode(tokenResp)
		case strings.Contains(r.URL.Path, "/v1/accounts") && !strings.Contains(r.URL.Path, "/locations"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"accounts": accounts})
		case strings.Contains(r.URL.Path, "/locations"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"locations": locations})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGetGoogleAuthURL_ReturnsURL(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockOAuth.On("GenerateState", mock.Anything, service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "google_business",
	}).Return("google-state-token", "google-nonce", nil)

	cfg := OAuthConfig{
		GoogleClientID:    "my_google_client",
		GoogleRedirectURI: "https://example.com/callback/google",
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.GetGoogleAuthURL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	authURL := resp["url"]
	if authURL == "" {
		t.Fatal("expected 'url' in response")
	}

	if !strings.Contains(authURL, "accounts.google.com") {
		t.Errorf("expected accounts.google.com in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "prompt=consent") {
		t.Errorf("expected prompt=consent in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "access_type=offline") {
		t.Errorf("expected access_type=offline in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "business.manage") {
		t.Errorf("expected business.manage scope in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "my_google_client") {
		t.Errorf("expected client_id in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "google-state-token") {
		t.Errorf("expected state in URL, got: %s", authURL)
	}

	mockOAuth.AssertExpectations(t)
}

// TestGetGoogleAuthURL_NoBusinessContext: handler returns 500 when middleware
// fails to seed BusinessContext (renamed from _Unauthorized).
func TestGetGoogleAuthURL_NoBusinessContext(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetGoogleAuthURL(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// TestGetGoogleAuthURL_Forbidden: BusinessContext present but missing
// PermIntegrationsConnect → 403.
func TestGetGoogleAuthURL_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetGoogleAuthURL(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestGoogleCallback_SingleLocation_AutoConnects(t *testing.T) {
	businessID := uuid.New()
	_ = uuid.New()

	server := newGoogleMockServer(t,
		map[string]interface{}{
			"access_token":  "google_access_token",
			"refresh_token": "google_refresh_token",
			"expires_in":    3600,
		},
		[]map[string]interface{}{
			{"name": "accounts/123"},
		},
		[]map[string]interface{}{
			{"name": "locations/456", "title": "My Coffee Shop"},
		},
	)
	defer server.Close()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		BusinessID: businessID,
		Platform:   "google_business",
		Nonce:      "csrf-nonce",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-google-state").Return(stateData, nil)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		acctID, _ := p.Metadata["account_id"].(string)
		locID, _ := p.Metadata["location_id"].(string)
		locTitle, _ := p.Metadata["location_title"].(string)
		return p.BusinessID == businessID &&
			p.Platform == "google_business" &&
			p.ExternalID == "locations/456" &&
			p.AccessToken == "google_access_token" &&
			p.RefreshToken == "google_refresh_token" &&
			acctID == "accounts/123" &&
			locID == "locations/456" &&
			locTitle == "My Coffee Shop"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "google_business"}, nil)

	cfg := OAuthConfig{
		GoogleClientID:        "google_client",
		GoogleClientSecret:    "google_secret",
		GoogleRedirectURI:     "https://example.com/callback/google",
		googleTokenBaseURL:    server.URL,
		googleAccountsBaseURL: server.URL,
		googleBusinessInfoURL: server.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, server.Client(), nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=auth_code&state=valid-google-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "csrf-nonce"})
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "connected=google_business") {
		t.Errorf("expected redirect to connected=google_business, got: %s", location)
	}

	mockOAuth.AssertExpectations(t)
	mockIntegration.AssertExpectations(t)
}

func TestGoogleCallback_MultipleLocations_RedirectsToSelection(t *testing.T) {
	businessID := uuid.New()
	_ = uuid.New()

	server := newGoogleMockServer(t,
		map[string]interface{}{
			"access_token":  "google_access_token",
			"refresh_token": "google_refresh_token",
			"expires_in":    3600,
		},
		[]map[string]interface{}{
			{"name": "accounts/123"},
		},
		[]map[string]interface{}{
			{"name": "locations/456", "title": "Shop A"},
			{"name": "locations/789", "title": "Shop B"},
		},
	)
	defer server.Close()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		BusinessID: businessID,
		Platform:   "google_business",
		Nonce:      "csrf-nonce",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-google-state").Return(stateData, nil)

	cfg := OAuthConfig{
		GoogleClientID:        "google_client",
		GoogleClientSecret:    "google_secret",
		GoogleRedirectURI:     "https://example.com/callback/google",
		googleTokenBaseURL:    server.URL,
		googleAccountsBaseURL: server.URL,
		googleBusinessInfoURL: server.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, server.Client(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=auth_code&state=valid-google-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "csrf-nonce"})
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "google_step=select_location") {
		t.Errorf("expected redirect to google_step=select_location, got: %s", location)
	}

	val, err := redisClient.Get(context.Background(), "google_temp:"+businessID.String()).Result()
	if err != nil {
		t.Fatalf("expected temp data in Redis: %v", err)
	}
	if !strings.Contains(val, "google_access_token") {
		t.Errorf("expected access_token in temp data")
	}
	if !strings.Contains(val, "Shop A") {
		t.Errorf("expected location title in temp data")
	}

	mockOAuth.AssertExpectations(t)
}

func TestGoogleCallback_MissingParams(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=missing_params") {
		t.Errorf("expected error=missing_params, got: %s", location)
	}
}

func TestGoogleCallback_InvalidState(t *testing.T) {
	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("ValidateState", mock.Anything, "bad-state").Return(nil, fmt.Errorf("invalid state"))

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=code&state=bad-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=invalid_state") {
		t.Errorf("expected error=invalid_state, got: %s", location)
	}
}

func TestGoogleCallback_NoRefreshToken(t *testing.T) {
	businessID := uuid.New()
	_ = uuid.New()

	server := newGoogleMockServer(t,
		map[string]interface{}{
			"access_token": "google_access_token",
			"expires_in":   3600,
		},
		nil, nil,
	)
	defer server.Close()

	mockOAuth := new(MockOAuthStateService)
	stateData := &service.OAuthStateData{
		BusinessID: businessID,
		Platform:   "google_business",
		Nonce:      "csrf-nonce",
	}
	mockOAuth.On("ValidateState", mock.Anything, "state").Return(stateData, nil)

	cfg := OAuthConfig{
		GoogleClientID:     "client",
		GoogleClientSecret: "secret",
		GoogleRedirectURI:  "https://example.com/cb",
		googleTokenBaseURL: server.URL,
	}

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), cfg, server.Client(), nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=code&state=state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "csrf-nonce"})
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=no_refresh_token") {
		t.Errorf("expected error=no_refresh_token, got: %s", location)
	}
}

func TestGoogleLocations_ReturnsTempData(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	tempData := `{"access_token":"tok","refresh_token":"ref","expires_in":3600,"business_id":"` + businessID.String() + `","locations":[{"account_name":"accounts/1","location_name":"locations/2","title":"Test Shop"}]}`
	_ = mr.Set("google_temp:"+businessID.String(), tempData)

	mockBusiness := new(MockBusinessService)

	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), mockBusiness, OAuthConfig{}, nil, redisClient)

	req := httptest.NewRequest(http.MethodGet, "/integrations/google_business/locations", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.GoogleLocations(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	locations, ok := resp["locations"].([]interface{})
	if !ok || len(locations) != 1 {
		t.Fatalf("expected 1 location, got %v", resp["locations"])
	}

	loc := locations[0].(map[string]interface{})
	if loc["title"] != "Test Shop" {
		t.Errorf("expected title 'Test Shop', got %v", loc["title"])
	}
}

func TestGoogleLocations_Expired(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockBusiness := new(MockBusinessService)

	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), mockBusiness, OAuthConfig{}, nil, redisClient)

	req := httptest.NewRequest(http.MethodGet, "/integrations/google_business/locations", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.GoogleLocations(rr, req)

	if rr.Code != http.StatusGone {
		t.Errorf("expected 410, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGoogleSelectLocation_ConnectsAndCleansUp(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	tempData := `{"access_token":"tok","refresh_token":"ref","expires_in":3600,"business_id":"` + businessID.String() + `","locations":[{"account_name":"accounts/1","location_name":"locations/2","title":"Test Shop"},{"account_name":"accounts/1","location_name":"locations/3","title":"Other Shop"}]}`
	_ = mr.Set("google_temp:"+businessID.String(), tempData)

	mockBusiness := new(MockBusinessService)

	mockIntegration := new(MockOAuthIntegrationService)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		acctID, _ := p.Metadata["account_id"].(string)
		locID, _ := p.Metadata["location_id"].(string)
		locTitle, _ := p.Metadata["location_title"].(string)
		return p.BusinessID == businessID &&
			p.Platform == "google_business" &&
			p.ExternalID == "locations/2" &&
			p.AccessToken == "tok" &&
			p.RefreshToken == "ref" &&
			acctID == "accounts/1" &&
			locID == "locations/2" &&
			locTitle == "Test Shop"
	})).Return(&domain.Integration{
		ID:       integrationID,
		Platform: "google_business",
	}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), mockIntegration, mockBusiness, OAuthConfig{}, nil, redisClient)

	reqBody := `{"account_id":"accounts/1","location_id":"locations/2"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/google_business/select-location", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.GoogleSelectLocation(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	exists := mr.Exists("google_temp:" + businessID.String())
	if exists {
		t.Error("expected Redis temp key to be deleted after select-location")
	}

	mockIntegration.AssertExpectations(t)
}
