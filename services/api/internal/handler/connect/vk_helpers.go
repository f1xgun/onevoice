package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/vkapi"
)

// Connect-package sentinel errors. Internal English so the package stays
// locale-agnostic; handlers map via errors.Is to pkg/i18n keys at the
// response boundary.
var (
	// ErrVKCommunityResolveFailed surfaces when probeVKCommunityToken cannot
	// resolve a screen-name/URL into a numeric group id. Wrapped with the
	// underlying detail via fmt.Errorf("%w: %w", ...).
	ErrVKCommunityResolveFailed = errors.New("connect: vk community resolve failed")
	// ErrVKWallPermissionMissing means a community access token lacks the
	// `wall` scope. Returned bare (no detail) because the localized template
	// already names the exact missing permission.
	ErrVKWallPermissionMissing = errors.New("connect: vk wall permission missing")
)

// vkGroup is the subset of VK's groups.getById response we care about.
type vkGroup struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ScreenName string `json:"screen_name"`
	Photo50    string `json:"photo_50"`
}

// probeVKCommunityToken hits groups.getById with the supplied token. Returns
// (group, vkErrMsg, transportErr): exactly one of the three is non-zero. The
// rawGroupInput is normalised through resolveVKGroupID when non-empty (lets
// callers pass a URL/screen_name); empty rawGroupInput means "let VK decide
// from the token's scope" (community tokens self-identify).
func (h *ConnectHandler) probeVKCommunityToken(
	ctx context.Context,
	accessToken, rawGroupInput string,
) (*vkGroup, string, error) {
	groupParam := ""
	if strings.TrimSpace(rawGroupInput) != "" {
		resolved, err := h.resolveVKGroupID(ctx, rawGroupInput)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %w", ErrVKCommunityResolveFailed, err)
		}
		groupParam = resolved
	}

	apiURL := fmt.Sprintf(
		"%s/method/groups.getById?fields=name,screen_name,photo_50&access_token=%s&v="+vkapi.APIVersion,
		h.vkAPIBase(), url.QueryEscape(accessToken),
	)
	if groupParam != "" {
		apiURL += "&group_id=" + url.QueryEscape(groupParam)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, "", err
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("vk request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var vkResp struct {
		Response struct {
			Groups []vkGroup `json:"groups"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal(body, &vkResp); jsonErr != nil {
		return nil, "", fmt.Errorf("decode vk response: %w", jsonErr)
	}
	if vkResp.Error != nil {
		slog.Warn("VK token validation error",
			"code", vkResp.Error.ErrorCode, "msg", vkResp.Error.ErrorMsg)
		return nil, vkResp.Error.ErrorMsg, nil
	}
	if len(vkResp.Response.Groups) == 0 {
		return nil, "", nil
	}
	g := vkResp.Response.Groups[0]
	return &g, "", nil
}

// checkVKWallScope verifies the supplied token grants `wall` permission. The
// review-reply dispatch path needs it; surfacing the gap at connect time
// avoids a confusing "ok!" → "can't reply" UX a few clicks later. Returns
// nil when the scope is present, an error with a Russian-language message
// when it isn't, and nil (treating as best-effort) on transport failures
// — VK occasionally rate-limits this method and we'd rather connect than
// block on a flaky check.
func (h *ConnectHandler) checkVKWallScope(ctx context.Context, accessToken string) error {
	apiURL := fmt.Sprintf(
		"%s/method/groups.getTokenPermissions?access_token=%s&v="+vkapi.APIVersion,
		h.vkAPIBase(), url.QueryEscape(accessToken),
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var permResp struct {
		Response struct {
			Permissions []struct {
				Name string `json:"name"`
			} `json:"permissions"`
		} `json:"response"`
	}
	if jsonErr := json.Unmarshal(body, &permResp); jsonErr != nil {
		return nil
	}
	for _, p := range permResp.Response.Permissions {
		if p.Name == "wall" {
			return nil
		}
	}
	return ErrVKWallPermissionMissing
}

// resolveVKGroupID turns user input (numeric id, screen_name, or full VK URL)
// into a numeric VK group id via groups.getById with the Mini-App service key.
func (h *ConnectHandler) resolveVKGroupID(ctx context.Context, input string) (string, error) {
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
	apiURL := fmt.Sprintf("%s/method/groups.getById?group_id=%s&access_token=%s&v="+vkapi.APIVersion,
		h.vkAPIBase(), url.QueryEscape(input), url.QueryEscape(h.cfg.VKServiceKey))
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
func (h *ConnectHandler) fetchVKCommunityName(ctx context.Context, groupID, token string) (string, error) {
	if token == "" {
		token = h.cfg.VKServiceKey
	}
	if token == "" {
		return "", nil
	}
	apiURL := fmt.Sprintf("%s/method/groups.getById?group_id=%s&fields=name&access_token=%s&v="+vkapi.APIVersion,
		h.vkAPIBase(), url.QueryEscape(groupID), url.QueryEscape(token))
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
