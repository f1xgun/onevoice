package platform

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/vkapi"
)

// VKSyncer pushes business updates to a VK community via the public API.
// VK's groups.edit accepts description + phone + website in a single call,
// so this is exposed as a single InfoSyncer capability — splitting it
// per-field would 3× the API quota for no benefit.
type VKSyncer struct {
	integrations integrationProvider
	httpClient   *http.Client
	vkBase       string
}

// Compile-time interface assertion.
var _ InfoSyncer = (*VKSyncer)(nil)

// NewVKSyncer wires a VKSyncer. integrations is required; httpClient defaults
// to a 10s client; vkBase defaults to vkapi.DefaultAPIBaseURL.
func NewVKSyncer(integrations integrationProvider, httpClient *http.Client, vkBase string) *VKSyncer {
	if integrations == nil {
		panic("platform.NewVKSyncer: integrations cannot be nil")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPClientTimeout}
	}
	if vkBase == "" {
		vkBase = vkapi.DefaultAPIBaseURL
	}
	return &VKSyncer{
		integrations: integrations,
		httpClient:   httpClient,
		vkBase:       vkBase,
	}
}

// SyncInfo pushes business name + description + phone + website to VK using the
// dedicated groups.edit API. The dispatcher records the AgentTask; this
// method only performs the API call and returns an error on failure.
//
// title carries the business name: groups.edit treats it as the community name,
// so omitting it (the prior behavior) meant a business rename never reached VK —
// the task reported "done" because description/phone/website still synced.
//
// Error-message shape ("token fetch failed: <inner>") is preserved verbatim
// from the prior implementation so existing log/UI assertions continue to
// match.
func (v *VKSyncer) SyncInfo(ctx context.Context, b *domain.Business, integ domain.Integration) error {
	groupID := integ.ExternalID

	token, err := v.integrations.GetDecryptedToken(ctx, b.ID, a2a.AgentVK, groupID, reasonVKSyncGroups)
	if err != nil {
		slog.Error("platform sync: vk: get token failed", "group_id", groupID, "error", err)
		return errTokenFetchFailed{cause: err}
	}

	params := url.Values{
		"group_id":     {groupID},
		"access_token": {token},
		"v":            {vkapi.APIVersion},
	}
	if b.Name != "" {
		params.Set("title", b.Name)
	}
	params.Set("description", b.Description)
	if b.Phone != "" {
		params.Set("phone", b.Phone)
	}
	if b.Website != nil && *b.Website != "" {
		params.Set("website", *b.Website)
	}

	if apiErr := v.callVKAPI(ctx, "groups.edit", params, groupID); apiErr != "" {
		return vkAPIError{msg: apiErr}
	}
	return nil
}

// errTokenFetchFailed wraps a token-fetch error so the recorded AgentTask
// shows "token fetch failed: <inner>" — matching the original sync.go shape.
type errTokenFetchFailed struct{ cause error }

func (e errTokenFetchFailed) Error() string { return "token fetch failed: " + e.cause.Error() }
func (e errTokenFetchFailed) Unwrap() error { return e.cause }

// vkAPIError carries the VK API error_msg verbatim — the dispatcher records
// it as the AgentTask error string.
type vkAPIError struct{ msg string }

func (e vkAPIError) Error() string { return e.msg }

// callVKAPI makes a VK API request and logs the result. Returns the VK error
// message or empty string on success — matches the prior method on Syncer.
func (v *VKSyncer) callVKAPI(ctx context.Context, method string, params url.Values, groupID string) string {
	apiURL := v.vkBase + "/method/" + method + "?" + params.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.Error("platform sync: vk "+method+" build request failed", "group_id", groupID, "error", err)
		return err.Error()
	}
	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		slog.Error("platform sync: vk "+method+" request failed", "group_id", groupID, "error", redactURLErr(err))
		return err.Error()
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Response interface{} `json:"response"`
		Error    *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		slog.Error("platform sync: vk "+method+" response parse failed", "group_id", groupID, "error", err, "body", string(respBody))
		return err.Error()
	}
	if result.Error != nil {
		slog.Error("platform sync: vk "+method+" API error", "group_id", groupID, "code", result.Error.ErrorCode, "msg", result.Error.ErrorMsg)
		return result.Error.ErrorMsg
	}
	slog.Info("platform sync: vk "+method+" success", "group_id", groupID)
	return ""
}
