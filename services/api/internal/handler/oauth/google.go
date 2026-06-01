package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// --- Google Business Profile OAuth ---

// GetGoogleAuthURL generates a Google OAuth2 authorization URL (PermIntegrationsConnect required).
func (h *OAuthHandler) GetGoogleAuthURL(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetGoogleAuthURL: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     bc.UserID,
		BusinessID: bc.BusinessID,
		Platform:   a2a.AgentGoogleBusiness,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to generate OAuth state for Google", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf(
		defaultGoogleAuthURL,
		url.QueryEscape(h.cfg.GoogleClientID),
		url.QueryEscape(h.cfg.GoogleRedirectURI),
		url.QueryEscape(googleBusinessManageScope),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// GoogleCallback handles the Google OAuth callback (public -- state validates identity).
func (h *OAuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid Google OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	// Exchange authorization code for tokens
	form := url.Values{
		"code":          {code},
		"client_id":     {h.cfg.GoogleClientID},
		"client_secret": {h.cfg.GoogleClientSecret},
		"redirect_uri":  {h.cfg.GoogleRedirectURI},
		"grant_type":    {"authorization_code"},
	}
	resp, err := h.httpClient.PostForm(h.googleTokenURL(), form)
	if err != nil {
		slog.ErrorContext(r.Context(), "Google token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.ErrorContext(r.Context(), "Google token response invalid", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	// CRITICAL: refresh_token is only returned on first consent. If missing, the user
	// did not grant offline access (prompt=consent was missing from auth URL).
	if tokenResp.RefreshToken == "" {
		http.Redirect(w, r, "/integrations?error=no_refresh_token", http.StatusFound)
		return
	}

	// Discover accounts
	accounts, err := h.googleDiscoverAccounts(r.Context(), tokenResp.AccessToken)
	if err != nil {
		slog.ErrorContext(r.Context(), "Google account discovery failed", "error", err)
		http.Redirect(w, r, "/integrations?error=discovery_failed", http.StatusFound)
		return
	}

	if len(accounts) == 0 {
		http.Redirect(w, r, "/integrations?error=no_locations", http.StatusFound)
		return
	}

	// Discover locations for the first account
	var allLocations []googleLocationRef
	for _, acct := range accounts {
		locations, locErr := h.googleDiscoverLocations(r.Context(), tokenResp.AccessToken, acct.Name)
		if locErr != nil {
			slog.ErrorContext(r.Context(), "Google location discovery failed", "account", acct.Name, "error", locErr)
			continue
		}
		for _, loc := range locations {
			allLocations = append(allLocations, googleLocationRef{
				AccountName:  acct.Name,
				LocationName: loc.Name,
				Title:        loc.Title,
			})
		}
	}

	if len(allLocations) == 0 {
		http.Redirect(w, r, "/integrations?error=no_locations", http.StatusFound)
		return
	}

	// Single location: auto-connect
	if len(allLocations) == 1 {
		loc := allLocations[0]
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
			BusinessID:   stateData.BusinessID,
			ActorID:      stateData.UserID,
			Platform:     a2a.AgentGoogleBusiness,
			ExternalID:   loc.LocationName,
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ExpiresAt:    &expiresAt,
			Metadata: map[string]interface{}{
				"account_id":     loc.AccountName,
				"location_id":    loc.LocationName,
				"location_title": loc.Title,
			},
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to connect Google Business integration", "error", err)
			http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/integrations?connected=google_business", http.StatusFound)
		return
	}

	// Multiple locations: store temp data in Redis for selection step
	tempData := googleTempData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		BusinessID:   stateData.BusinessID.String(),
		Locations:    allLocations,
	}
	tempJSON, _ := json.Marshal(tempData)
	redisKey := "google_temp:" + stateData.BusinessID.String()
	if err := h.redis.Set(r.Context(), redisKey, tempJSON, tempOAuthCredsTTL).Err(); err != nil {
		slog.ErrorContext(r.Context(), "failed to store Google temp data in Redis", "error", err)
		http.Redirect(w, r, "/integrations?error=internal_error", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?google_step=select_location", http.StatusFound)
}

// googleDiscoverAccounts calls the Google Business Account Management API.
func (h *OAuthHandler) googleDiscoverAccounts(ctx context.Context, accessToken string) ([]googleAccount, error) {
	reqURL := h.googleAccountsURL() + "/v1/accounts"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build accounts request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("accounts request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Accounts []googleAccount `json:"accounts"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse accounts response: %w", err)
	}
	return result.Accounts, nil
}

// googleDiscoverLocations calls the Google Business Information API for a given account.
func (h *OAuthHandler) googleDiscoverLocations(ctx context.Context, accessToken, accountName string) ([]googleLocation, error) {
	reqURL := fmt.Sprintf("%s/v1/%s/locations?readMask=name,title,storefrontAddress", h.googleBusinessInfoBaseURL(), accountName)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build locations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locations request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Locations []googleLocation `json:"locations"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse locations response: %w", err)
	}
	return result.Locations, nil
}

// GoogleLocations returns discovered locations from temp token data in Redis (PermIntegrationsConnect required).
func (h *OAuthHandler) GoogleLocations(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GoogleLocations: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	redisKey := "google_temp:" + bc.BusinessID.String()
	val, err := h.redis.Get(r.Context(), redisKey).Result()
	if err != nil {
		writeJSONError(w, http.StatusGone, "Google session expired, please reconnect")
		return
	}

	var tempData googleTempData
	if err := json.Unmarshal([]byte(val), &tempData); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid temp data")
		return
	}

	writeJSON(w, http.StatusOK, openapi.GoogleLocationsResponse{
		Locations: googleLocationRefsToOpenAPI(tempData.Locations),
	})
}

// googleLocationRefsToOpenAPI maps the redis-cached googleLocationRef
// slice to the spec-owned openapi.GoogleLocationRef.
func googleLocationRefsToOpenAPI(in []googleLocationRef) []openapi.GoogleLocationRef {
	out := make([]openapi.GoogleLocationRef, len(in))
	for i, l := range in {
		out[i] = openapi.GoogleLocationRef{
			AccountName:  l.AccountName,
			LocationName: l.LocationName,
			Title:        l.Title,
		}
	}
	return out
}

// GoogleSelectLocation connects the selected Google Business location (PermIntegrationsConnect required, POST).
func (h *OAuthHandler) GoogleSelectLocation(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GoogleSelectLocation: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.GoogleSelectLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AccountId == "" || req.LocationId == "" {
		writeJSONError(w, http.StatusBadRequest, "account_id and location_id are required")
		return
	}

	redisKey := "google_temp:" + bc.BusinessID.String()
	val, err := h.redis.Get(r.Context(), redisKey).Result()
	if err != nil {
		writeJSONError(w, http.StatusGone, "Google session expired, please reconnect")
		return
	}

	var tempData googleTempData
	if err := json.Unmarshal([]byte(val), &tempData); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid temp data")
		return
	}

	// Find the matching location to get its title
	var locationTitle string
	found := false
	for _, loc := range tempData.Locations {
		if loc.AccountName == req.AccountId && loc.LocationName == req.LocationId {
			locationTitle = loc.Title
			found = true
			break
		}
	}
	if !found {
		writeJSONError(w, http.StatusBadRequest, "location not found in discovered locations")
		return
	}

	expiresAt := time.Now().Add(time.Duration(tempData.ExpiresIn) * time.Second)
	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   bc.BusinessID,
		ActorID:      bc.UserID,
		Platform:     a2a.AgentGoogleBusiness,
		ExternalID:   req.LocationId,
		AccessToken:  tempData.AccessToken,
		RefreshToken: tempData.RefreshToken,
		ExpiresAt:    &expiresAt,
		Metadata: map[string]interface{}{
			"account_id":     req.AccountId,
			"location_id":    req.LocationId,
			"location_title": locationTitle,
		},
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to connect Google Business integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	// Clean up temp data
	_ = h.redis.Del(r.Context(), redisKey).Err()

	writeJSON(w, http.StatusCreated, integration)
}
