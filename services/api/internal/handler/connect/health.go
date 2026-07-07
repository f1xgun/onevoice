package connect

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
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/vkapi"
	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
)

// connHealthTokenReason is the audit reason recorded when the health checker
// decrypts a VK token to probe scope. Distinct from the connect/refresh reasons
// so the token-access journal shows why the read happened.
const connHealthTokenReason = "connection_health_probe"

// CheckTelegramHealth evaluates a Telegram channel's connection health using the
// system bot token the connect handler already holds. It is the token-resolving
// entry point the connhealth checker (verify + worker) calls; the connect-time
// path uses EvaluateTelegramHealth directly with the same token.
func (h *ConnectHandler) CheckTelegramHealth(ctx context.Context, externalID string) connhealth.Result {
	return h.EvaluateTelegramHealth(ctx, h.cfg.TelegramBotToken, externalID)
}

// CheckVKHealth resolves the decrypted community token for a VK integration and
// evaluates its connection health. A token that cannot be decrypted is
// inconclusive (unknown) — fail-soft, never a false broken.
func (h *ConnectHandler) CheckVKHealth(ctx context.Context, businessID uuid.UUID, externalID string) connhealth.Result {
	now := time.Now().UTC()
	tok, err := h.integrationService.GetDecryptedToken(ctx, businessID, a2a.AgentVK, externalID, connHealthTokenReason)
	if err != nil || tok == nil || tok.AccessToken == "" {
		return connhealth.Result{Status: connhealth.StatusUnknown, ReasonCode: connhealth.ReasonInconclusive, CheckedAt: now}
	}
	return h.EvaluateVKHealth(ctx, tok.AccessToken)
}

// telegramGetMeResponse mirrors the Bot API getMe envelope; only the numeric
// bot id is needed to key getChatMember. Local struct — api.telegram.org owns
// the shape, so it stays out of the OneVoice spec.
type telegramGetMeResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		ID int64 `json:"id"`
	} `json:"result"`
	Description string `json:"description"`
}

// telegramChatMemberResponse mirrors the Bot API getChatMember envelope. status
// is one of creator|administrator|member|restricted|left|kicked; can_post_messages
// is meaningful only for administrators (creators post implicitly).
type telegramChatMemberResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Status          string `json:"status"`
		CanPostMessages bool   `json:"can_post_messages"`
	} `json:"result"`
	Description string `json:"description"`
}

// telegramGetMe fetches the bot's own numeric id (needed to key getChatMember).
// One extra call per health check — cheap; a cached bot id is a later
// optimization. Errors are classified as *telegramAPIError like telegramGetChat.
func (h *ConnectHandler) telegramGetMe(ctx context.Context, botToken string) (int64, error) {
	apiURL := fmt.Sprintf("%s/bot%s/getMe", h.telegramAPIBase(), botToken)
	body, err := h.telegramGet(ctx, apiURL)
	if err != nil {
		return 0, err
	}
	var meResp telegramGetMeResponse
	if jsonErr := json.Unmarshal(body, &meResp); jsonErr != nil {
		return 0, &telegramAPIError{Kind: telegramErrUnreachable, Err: jsonErr}
	}
	if !meResp.OK {
		return 0, &telegramAPIError{Kind: telegramErrAPIRejected, Description: meResp.Description}
	}
	return meResp.Result.ID, nil
}

// telegramGetChatMember reads the bot's membership record in a chat so the
// health evaluator can decide whether the bot still has post rights. botID is
// the bot's own numeric id (from telegramGetMe). Errors are classified as
// *telegramAPIError like telegramGetChat.
func (h *ConnectHandler) telegramGetChatMember(ctx context.Context, botToken, chatID string, botID int64) (telegramChatMemberResponse, error) {
	apiURL := fmt.Sprintf("%s/bot%s/getChatMember?chat_id=%s&user_id=%s",
		h.telegramAPIBase(), botToken, url.QueryEscape(chatID), url.QueryEscape(strconv.FormatInt(botID, 10)))
	body, err := h.telegramGet(ctx, apiURL)
	if err != nil {
		return telegramChatMemberResponse{}, err
	}
	var memberResp telegramChatMemberResponse
	if jsonErr := json.Unmarshal(body, &memberResp); jsonErr != nil {
		return telegramChatMemberResponse{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: jsonErr}
	}
	if !memberResp.OK {
		kind := telegramErrAPIRejected
		if hasForbiddenPrefix(memberResp.Description) {
			kind = telegramErrForbidden
		}
		return telegramChatMemberResponse{}, &telegramAPIError{Kind: kind, Description: memberResp.Description}
	}
	return memberResp, nil
}

// telegramGet issues a GET to a Bot API URL and returns the raw body, mapping
// transport/parse failures to *telegramAPIError{telegramErrUnreachable}. The
// bot token is in the PATH, so redactURLErr scrubs it out of any *url.Error.
func (h *ConnectHandler) telegramGet(ctx context.Context, apiURL string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, &telegramAPIError{Kind: telegramErrUnreachable, Err: redactURLErr(err)}
	}
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, &telegramAPIError{Kind: telegramErrUnreachable, Err: redactURLErr(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &telegramAPIError{Kind: telegramErrUnreachable, Err: err}
	}
	return body, nil
}

// hasForbiddenPrefix reports whether a Bot API description is a Forbidden-style
// rejection (bot kicked / not a member / blocked).
func hasForbiddenPrefix(desc string) bool {
	return strings.HasPrefix(desc, "Forbidden")
}

// EvaluateTelegramHealth probes a Telegram channel's bot-membership and maps it
// to a connection-health verdict. It is the SINGLE implementation shared by the
// connect-time admin check (ConnectTelegram) and the verify/worker paths.
//
// Fail-soft: any transport-level failure (telegramErrUnreachable) yields
// StatusUnknown so a flaky probe never demotes a working channel or blocks a
// real connect. Forbidden-style rejections and member/left/kicked statuses are
// conclusive => broken. A creator, or an administrator with can_post_messages,
// is active.
func (h *ConnectHandler) EvaluateTelegramHealth(ctx context.Context, botToken, chatID string) connhealth.Result {
	now := time.Now().UTC()
	botID, err := h.telegramGetMe(ctx, botToken)
	if err != nil {
		return inconclusiveOrBroken(err, now)
	}
	member, err := h.telegramGetChatMember(ctx, botToken, chatID, botID)
	if err != nil {
		return inconclusiveOrBroken(err, now)
	}
	return evaluateTelegramMember(member.Result.Status, member.Result.CanPostMessages, now)
}

// evaluateTelegramMember maps a getChatMember (status, can_post_messages) pair
// to a health Result. Kept pure so the mapping is unit-testable without HTTP.
func evaluateTelegramMember(status string, canPost bool, now time.Time) connhealth.Result {
	switch status {
	case "creator":
		return connhealth.Result{Status: connhealth.StatusActive, ReasonCode: connhealth.ReasonOK, CheckedAt: now}
	case "administrator":
		if canPost {
			return connhealth.Result{Status: connhealth.StatusActive, ReasonCode: connhealth.ReasonOK, CheckedAt: now}
		}
		return connhealth.Result{Status: connhealth.StatusBroken, ReasonCode: connhealth.ReasonTelegramNoPostRight, CheckedAt: now}
	default:
		return connhealth.Result{Status: connhealth.StatusBroken, ReasonCode: connhealth.ReasonTelegramNotAdmin, CheckedAt: now}
	}
}

// inconclusiveOrBroken maps a *telegramAPIError to a fail-soft verdict:
// transport failures are inconclusive (unknown), forbidden/API rejections are
// conclusive (broken/not-admin).
func inconclusiveOrBroken(err error, now time.Time) connhealth.Result {
	var apiErr *telegramAPIError
	if errors.As(err, &apiErr) && apiErr.Kind == telegramErrUnreachable {
		return connhealth.Result{Status: connhealth.StatusUnknown, ReasonCode: connhealth.ReasonInconclusive, CheckedAt: now}
	}
	return connhealth.Result{Status: connhealth.StatusBroken, ReasonCode: connhealth.ReasonTelegramNotAdmin, CheckedAt: now}
}

// EvaluateVKHealth probes a VK community token: first that it still resolves a
// community (token valid), then that it retains the `wall` scope. Fail-soft on
// rate-limit / transport (unknown); conclusive on a genuine auth failure
// (broken/token-invalid) or a definitively-missing wall scope
// (broken/wall-scope-missing).
func (h *ConnectHandler) EvaluateVKHealth(ctx context.Context, accessToken string) connhealth.Result {
	now := time.Now().UTC()

	_, vkErr, transportErr := h.probeVKCommunityToken(ctx, accessToken, "")
	if transportErr != nil {
		return connhealth.Result{Status: connhealth.StatusUnknown, ReasonCode: connhealth.ReasonInconclusive, CheckedAt: now}
	}
	if vkErr != "" {
		return connhealth.Result{Status: connhealth.StatusBroken, ReasonCode: connhealth.ReasonVKTokenInvalid, CheckedAt: now}
	}

	hasWall, conclusive, err := h.checkVKWallScopeDetailed(ctx, accessToken)
	if err != nil || !conclusive {
		return connhealth.Result{Status: connhealth.StatusUnknown, ReasonCode: connhealth.ReasonInconclusive, CheckedAt: now}
	}
	if !hasWall {
		return connhealth.Result{Status: connhealth.StatusBroken, ReasonCode: connhealth.ReasonVKWallScopeMissing, CheckedAt: now}
	}
	return connhealth.Result{Status: connhealth.StatusActive, ReasonCode: connhealth.ReasonOK, CheckedAt: now}
}

// checkVKWallScopeDetailed is the authoritative three-outcome variant of
// checkVKWallScope used by the health path. It distinguishes:
//   - (true, true, nil):   token conclusively HAS the wall scope,
//   - (false, true, nil):  token conclusively LACKS the wall scope,
//   - (false, false, nil): inconclusive (VK rate-limit / API-error envelope),
//   - (false, false, err): inconclusive (transport / decode failure).
//
// The best-effort checkVKWallScope (connect-time) is kept unchanged so its
// existing behavior — never blocking a connect on a flaky check — is preserved.
func (h *ConnectHandler) checkVKWallScopeDetailed(ctx context.Context, accessToken string) (hasWall, conclusive bool, err error) {
	apiURL := fmt.Sprintf(
		"%s/method/groups.getTokenPermissions?access_token=%s&v="+vkapi.APIVersion,
		h.vkAPIBase(), url.QueryEscape(accessToken),
	)
	httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if reqErr != nil {
		return false, false, reqErr
	}
	resp, doErr := h.httpClient.Do(httpReq)
	if doErr != nil {
		return false, false, redactURLErr(doErr)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var permResp struct {
		Response struct {
			Permissions []struct {
				Name string `json:"name"`
			} `json:"permissions"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal(body, &permResp); jsonErr != nil {
		return false, false, jsonErr
	}
	if permResp.Error != nil {
		return false, false, nil
	}
	for _, p := range permResp.Response.Permissions {
		if p.Name == "wall" {
			return true, true, nil
		}
	}
	return false, true, nil
}
