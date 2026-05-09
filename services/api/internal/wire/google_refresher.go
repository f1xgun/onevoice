package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// googleTokenRefresher implements service.TokenRefresher for Google OAuth2.
type googleTokenRefresher struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

// NewGoogleTokenRefresher constructs a service.TokenRefresher backed by the
// Google OAuth2 token endpoint. clientID and clientSecret are mandatory —
// callers should gate construction on cfg.GoogleClientID/Secret being set.
// httpClient may be nil; defaults to http.DefaultClient.
func NewGoogleTokenRefresher(clientID, clientSecret string, httpClient *http.Client) service.TokenRefresher {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &googleTokenRefresher{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
	}
}

func (r *googleTokenRefresher) RefreshToken(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, expiresIn int64, err error) {
	form := url.Values{
		"client_id":     {r.clientID},
		"client_secret": {r.clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("refresh request: %w", err)
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
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", "", 0, fmt.Errorf("parse refresh response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", "", 0, fmt.Errorf("google token refresh error: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", "", 0, fmt.Errorf("google token refresh returned empty access token")
	}
	return tokenResp.AccessToken, tokenResp.RefreshToken, tokenResp.ExpiresIn, nil
}
