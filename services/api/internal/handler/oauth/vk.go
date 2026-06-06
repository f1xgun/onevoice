package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/vkapi"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// GetVKAuthURL generates a classic VK OAuth authorization URL (PermIntegrationsConnect required).
// Uses oauth.vk.com code flow (not VK ID / id.vk.com) — the resulting user
// token works with groups.get and wall.getComments, which VK ID tokens
// reject with error 1051 ("method unavailable with current profile type").
func (h *OAuthHandler) GetVKAuthURL(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetVKAuthURL: no BusinessContext in ctx — middleware misconfiguration")
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
		Platform:   a2a.AgentVK,
	})
	if err != nil {
		slog.Error("failed to generate OAuth state for VK", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf("%s/authorize?client_id=%s&redirect_uri=%s&scope=wall,groups,manage&response_type=code&state=%s&v="+vkapi.APIVersion,
		h.vkTokenBaseURL(),
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKRedirectURI),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// VKCallback handles the classic VK OAuth callback (public — state validates identity).
func (h *OAuthHandler) VKCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		slog.Warn("VK callback missing params", "code_present", code != "", "state_present", state != "")
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid VK OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	tokenEndpoint := fmt.Sprintf("%s/access_token?client_id=%s&client_secret=%s&redirect_uri=%s&code=%s",
		h.vkTokenBaseURL(),
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKClientSecret),
		url.QueryEscape(h.cfg.VKRedirectURI),
		url.QueryEscape(code),
	)
	resp, err := h.httpClient.Get(tokenEndpoint)
	if err != nil {
		slog.Error("VK token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		UserID      int64  `json:"user_id"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.Error("VK token response invalid",
			"error", err,
			"vk_error", tokenResp.Error,
			"vk_error_desc", tokenResp.ErrorDesc,
			"status", resp.StatusCode,
			"body", string(body),
		)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	redisKey := fmt.Sprintf("vk_temp_token:%s", stateData.BusinessID.String())
	if err := h.redis.Set(r.Context(), redisKey, tokenResp.AccessToken, tempOAuthCredsTTL).Err(); err != nil {
		slog.Error("failed to store temp VK token", "error", err)
		http.Redirect(w, r, "/integrations?error=internal", http.StatusFound)
		return
	}

	slog.Info("VK user token stored temporarily, awaiting community selection",
		"business_id", stateData.BusinessID,
		"user_id", tokenResp.UserID,
	)

	http.Redirect(w, r, "/integrations?vk_step=select_community", http.StatusFound)
}

// VKCommunities returns communities where the user is an admin (PermIntegrationsConnect required).
// Uses the temporary user token stored in Redis during VK OAuth step 1.
func (h *OAuthHandler) VKCommunities(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "VKCommunities: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	redisKey := fmt.Sprintf("vk_temp_token:%s", bc.BusinessID.String())
	token, err := h.redis.Get(r.Context(), redisKey).Result()
	if err != nil {
		slog.Warn("VK temp token not found or expired", "error", err)
		writeJSONError(w, http.StatusGone, "VK session expired, please reconnect")
		return
	}

	vkURL := fmt.Sprintf(vkapi.DefaultAPIBaseURL+"/method/groups.get?filter=admin&extended=1&fields=name,photo_50,screen_name,members_count&access_token=%s&v="+vkapi.APIVersion,
		url.QueryEscape(token),
	)
	resp, err := h.httpClient.Get(vkURL)
	if err != nil {
		slog.Error("VK groups.get failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "failed to fetch communities")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var vkResp struct {
		Response struct {
			Items []struct {
				ID           int64  `json:"id"`
				Name         string `json:"name"`
				ScreenName   string `json:"screen_name"`
				Photo50      string `json:"photo_50"`
				MembersCount int    `json:"members_count"`
			} `json:"items"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &vkResp); err != nil {
		slog.Error("VK groups response parse error", "error", err, "body", string(body))
		writeJSONError(w, http.StatusBadGateway, "invalid VK response")
		return
	}
	if vkResp.Error != nil {
		slog.Error("VK API error", "code", vkResp.Error.ErrorCode, "msg", vkResp.Error.ErrorMsg)
		writeJSONError(w, http.StatusBadGateway, vkResp.Error.ErrorMsg)
		return
	}

	writeJSON(w, http.StatusOK, vkResp.Response.Items)
}

// VKCommunityAuthURL generates the second OAuth URL for community token (PermIntegrationsConnect required).
// Uses old VK OAuth with group_ids to get a community-scoped token.
func (h *OAuthHandler) VKCommunityAuthURL(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "VKCommunityAuthURL: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	groupInput := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupInput == "" {
		writeJSONError(w, http.StatusBadRequest, "group_id is required")
		return
	}

	groupID, err := h.resolveVKGroupID(r.Context(), groupInput)
	if err != nil {
		slog.Warn("VK group resolution failed", "input", groupInput, "error", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     bc.UserID,
		BusinessID: bc.BusinessID,
		Platform:   a2a.AgentVK,
	})
	if err != nil {
		slog.Error("failed to generate state for VK community OAuth", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf(vkapi.DefaultOAuthBaseURL+"/authorize?client_id=%s&redirect_uri=%s&group_ids=%s&scope=wall,manage&response_type=code&state=%s&v="+vkapi.APIVersion,
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKCommunityRedirectURI()),
		url.QueryEscape(groupID),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// VKCommunityCallback handles the old VK OAuth callback for community tokens (public).
func (h *OAuthHandler) VKCommunityCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid VK community OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	tokenURL := fmt.Sprintf(vkapi.DefaultOAuthBaseURL+"/access_token?client_id=%s&client_secret=%s&redirect_uri=%s&code=%s",
		h.cfg.VKClientID,
		h.cfg.VKClientSecret,
		url.QueryEscape(h.cfg.VKCommunityRedirectURI()),
		code,
	)
	resp, err := h.httpClient.Get(tokenURL)
	if err != nil {
		slog.Error("VK community token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	var tokenResp struct {
		Groups []struct {
			GroupID     int64  `json:"group_id"`
			AccessToken string `json:"access_token"`
		} `json:"groups"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		slog.Error("VK community token response parse error", "error", err, "body", string(body))
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	if tokenResp.Error != "" {
		slog.Error("VK community token error", "error", tokenResp.Error, "desc", tokenResp.ErrorDesc, "body", string(body))
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	if len(tokenResp.Groups) == 0 {
		slog.Error("VK community token response has no groups", "body", string(body))
		http.Redirect(w, r, "/integrations?error=no_community_token", http.StatusFound)
		return
	}

	group := tokenResp.Groups[0]
	groupIDStr := strconv.FormatInt(group.GroupID, 10)

	redisKey := fmt.Sprintf("vk_temp_token:%s", stateData.BusinessID.String())
	userToken, _ := h.redis.Get(r.Context(), redisKey).Result()

	communityName, _ := h.fetchVKCommunityName(r.Context(), groupIDStr, group.AccessToken)

	metadata := map[string]any{
		"group_id": group.GroupID,
	}
	if communityName != "" {
		metadata["community_name"] = communityName
	}
	_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   stateData.BusinessID,
		ActorID:      stateData.UserID,
		Platform:     a2a.AgentVK,
		ExternalID:   groupIDStr,
		AccessToken:  group.AccessToken,
		UserToken:    userToken,
		Metadata:     metadata,
		ActorIP:      middleware.ClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		ParsedFormat: "oauth_code",
	})
	if err != nil {
		slog.Error("failed to connect VK community integration", "error", err)
		http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
		return
	}

	_ = h.redis.Del(r.Context(), redisKey).Err()

	slog.Info("VK community integration connected",
		"business_id", stateData.BusinessID,
		"group_id", group.GroupID,
	)

	http.Redirect(w, r, "/integrations?connected=vk", http.StatusFound)
}
