package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// GetYandexAuthURL generates a Yandex OAuth authorization URL (PermIntegrationsConnect required).
func (h *OAuthHandler) GetYandexAuthURL(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetYandexAuthURL: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	state, nonce, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     bc.UserID,
		BusinessID: bc.BusinessID,
		Platform:   a2a.AgentYandexBusiness,
	})
	if err != nil {
		slog.Error("failed to generate OAuth state for Yandex", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf(defaultYandexAuthURL+"?response_type=code&client_id=%s&redirect_uri=%s&state=%s",
		url.QueryEscape(h.cfg.YandexClientID),
		url.QueryEscape(h.cfg.YandexRedirectURI),
		url.QueryEscape(state),
	)

	h.issueOAuthCSRFCookie(w, nonce)
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// YandexCallback handles the Yandex OAuth callback (public — state validates identity).
func (h *OAuthHandler) YandexCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid Yandex OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	if !csrfCookieMatches(r, stateData.Nonce) {
		slog.Warn("Yandex OAuth CSRF cookie mismatch", "business_id", stateData.BusinessID)
		h.clearOAuthCSRFCookie(w)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}
	h.clearOAuthCSRFCookie(w)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {h.cfg.YandexClientID},
		"client_secret": {h.cfg.YandexClientSecret},
	}
	resp, err := h.httpClient.PostForm(h.yandexTokenURL(), form)
	if err != nil {
		slog.Error("Yandex token exchange failed", "error", err)
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
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.Error("Yandex token response invalid", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   stateData.BusinessID,
		ActorID:      stateData.UserID,
		Platform:     a2a.AgentYandexBusiness,
		ExternalID:   "pending:" + stateData.BusinessID.String(),
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    &expiresAt,
		ActorIP:      middleware.ClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		ParsedFormat: service.ParsedFormatOAuthCode,
	})
	if err != nil {
		slog.Warn("failed to connect Yandex.Business integration", "error", err)
		http.Redirect(w, r, "/integrations?error="+connectErrorRedirectCode(err), http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?connected=yandex_business", http.StatusFound)
}
