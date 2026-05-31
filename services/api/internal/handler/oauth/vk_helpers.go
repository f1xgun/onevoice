package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/vkapi"
)

// resolveVKGroupID turns user input (numeric id, screen_name, or full VK URL)
// into a numeric VK group id via groups.getById with the Mini-App service key.
func (h *OAuthHandler) resolveVKGroupID(ctx context.Context, input string) (string, error) {
	// Strip URL prefix: https://vk.com/mygroup → mygroup
	input = strings.TrimSpace(input)
	for _, prefix := range vkapi.URLPrefixes {
		input = strings.TrimPrefix(input, prefix)
	}
	input = strings.TrimPrefix(input, "club")
	input = strings.TrimPrefix(input, "public")
	input = strings.SplitN(input, "/", 2)[0]
	input = strings.SplitN(input, "?", 2)[0]
	if input == "" {
		return "", fmt.Errorf("empty group id/URL")
	}
	// If already a positive numeric ID, return as-is.
	if _, err := strconv.ParseUint(input, 10, 64); err == nil {
		return input, nil
	}
	if h.cfg.VKServiceKey == "" {
		return "", fmt.Errorf("VK service key not configured; pass a numeric group id")
	}
	apiURL := fmt.Sprintf(vkapi.DefaultAPIBaseURL+"/method/groups.getById?group_id=%s&access_token=%s&v="+vkapi.APIVersion,
		url.QueryEscape(input), url.QueryEscape(h.cfg.VKServiceKey))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vk API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var vkResp struct {
		Response struct {
			Groups []struct {
				ID int64 `json:"id"`
			} `json:"groups"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &vkResp); err != nil {
		return "", fmt.Errorf("parse vk response: %w", err)
	}
	if vkResp.Error != nil {
		return "", fmt.Errorf("vk: %s", vkResp.Error.ErrorMsg)
	}
	if len(vkResp.Response.Groups) == 0 {
		return "", fmt.Errorf("community not found: %s", input)
	}
	return strconv.FormatInt(vkResp.Response.Groups[0].ID, 10), nil
}

// fetchVKCommunityName resolves a numeric VK group id into its display name
// via groups.getById. Prefers a caller-supplied community/user token; falls
// back to the Mini-App service key if the caller passes an empty token.
// Returns "" with no error when the lookup is best-effort and the token is
// unavailable — callers gate on len(name) > 0 before persisting.
func (h *OAuthHandler) fetchVKCommunityName(ctx context.Context, groupID, token string) (string, error) {
	if token == "" {
		token = h.cfg.VKServiceKey
	}
	if token == "" {
		return "", nil
	}
	apiURL := fmt.Sprintf(vkapi.DefaultAPIBaseURL+"/method/groups.getById?group_id=%s&fields=name&access_token=%s&v="+vkapi.APIVersion,
		url.QueryEscape(groupID), url.QueryEscape(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var vkResp struct {
		Response struct {
			Groups []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"groups"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &vkResp); err != nil {
		return "", err
	}
	if vkResp.Error != nil {
		return "", fmt.Errorf("vk: %s", vkResp.Error.ErrorMsg)
	}
	if len(vkResp.Response.Groups) == 0 {
		return "", nil
	}
	return vkResp.Response.Groups[0].Name, nil
}
