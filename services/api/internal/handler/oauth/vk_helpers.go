package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/vkapi"
)

// redactURLErr scrubs the query string from a *url.Error before the error is
// wrapped, logged, or surfaced to a client. VK outbound calls carry the
// service key / community token in the URL query (access_token=…); on a
// transport failure net/http returns a *url.Error whose .Error() embeds the
// FULL URL, which would otherwise leak the secret into log lines and JSON
// error bodies. Non-*url.Error inputs are returned unchanged.
func redactURLErr(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	if u, parseErr := url.Parse(ue.URL); parseErr == nil && u.RawQuery != "" {
		u.RawQuery = "REDACTED"
		ue.URL = u.String()
	}
	return err
}

// resolveVKGroupID turns user input (numeric id, screen_name, or full VK URL)
// into a numeric VK group id via groups.getById with the Mini-App service key.
func (h *OAuthHandler) resolveVKGroupID(ctx context.Context, input string) (string, error) {
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
		return "", fmt.Errorf("vk API request failed: %w", redactURLErr(err))
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
		return "", redactURLErr(err)
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
