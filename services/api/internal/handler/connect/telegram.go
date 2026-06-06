package connect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// telegramChatInfo holds the fields we care about from Telegram's getChat
// response: title, and — for channels — the linked discussion group's chat
// id. A non-zero LinkedChatID means subscribers' comments on channel posts
// are routed into that group, and the bot needs to be a member of that
// group (admin, ideally) to see them via getUpdates.
type telegramChatInfo struct {
	Title        string
	LinkedChatID int64
}

// telegramGetChatResponse mirrors the external Telegram Bot API getChat
// envelope; it is NOT part of OneVoice's wire format (api.telegram.org
// owns this shape), so it stays a local struct rather than coming from
// the spec.
type telegramGetChatResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Title        string `json:"title"`
		LinkedChatID int64  `json:"linked_chat_id"`
	} `json:"result"`
	Description string `json:"description"`
}

// VerifyTelegramLogin verifies a Telegram Login Widget callback (JWT required).
func (h *ConnectHandler) VerifyTelegramLogin(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hash, _ := req["hash"].(string)
	if hash == "" {
		writeJSONError(w, http.StatusBadRequest, "hash is required")
		return
	}

	authDateStr, _ := req["auth_date"].(string)
	if authDateStr == "" {
		if authDateF, ok := req["auth_date"].(float64); ok {
			authDateStr = strconv.FormatInt(int64(authDateF), 10)
		}
	}
	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil || time.Since(time.Unix(authDate, 0)) > 5*time.Minute {
		writeJSONError(w, http.StatusUnauthorized, "auth_date expired")
		return
	}

	delete(req, "hash")
	keys := make([]string, 0, len(req))
	for k := range req {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, req[k]))
	}
	checkString := strings.Join(parts, "\n")

	secretKey := sha256.Sum256([]byte(h.cfg.TelegramBotToken))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(checkString))
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	if hash != expectedHash {
		writeJSONError(w, http.StatusUnauthorized, "invalid hash")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "user": req})
}

// telegramErrKind classifies why a getChat call failed, so HTTP handlers
// can map upstream failure modes to the right status instead of collapsing
// everything into a generic 400. See feedback memory
// `distinguish-external-api-errors`.
type telegramErrKind int

const (
	// telegramErrUnreachable: transport-level failure (timeout, dial error,
	// EOF, malformed response). Telegram never returned a parseable answer,
	// so we do not know whether the bot has access. Map to 502/504.
	telegramErrUnreachable telegramErrKind = iota
	// telegramErrForbidden: Telegram returned ok:false with a Forbidden-
	// style description (bot kicked, not a member, blocked). Map to 403.
	telegramErrForbidden
	// telegramErrAPIRejected: Telegram returned ok:false with any other
	// description (chat not found, bad chat_id, etc.). Map to 400 and
	// surface the description to the user.
	telegramErrAPIRejected
)

type telegramAPIError struct {
	Kind        telegramErrKind
	Description string
	Err         error
}

func (e *telegramAPIError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "telegram api error"
}

func (e *telegramAPIError) Unwrap() error { return e.Err }

func telegramErrKindName(k telegramErrKind) string {
	switch k {
	case telegramErrUnreachable:
		return "unreachable"
	case telegramErrForbidden:
		return "forbidden"
	case telegramErrAPIRejected:
		return "api_rejected"
	default:
		return "unknown"
	}
}

// telegramGetChat calls the Telegram Bot API to validate bot access and
// fetch channel title + linked discussion chat id. Errors are returned as
// *telegramAPIError so callers can distinguish transport failures from
// API-level rejections from forbidden-style auth errors.
func (h *ConnectHandler) telegramGetChat(ctx context.Context, botToken, chatID string) (telegramChatInfo, error) {
	apiURL := fmt.Sprintf("%s/bot%s/getChat?chat_id=%s",
		h.telegramAPIBase(), botToken, url.QueryEscape(chatID))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return telegramChatInfo{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: err}
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return telegramChatInfo{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return telegramChatInfo{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: err}
	}

	var chatResp telegramGetChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return telegramChatInfo{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: err}
	}

	if !chatResp.OK {
		kind := telegramErrAPIRejected
		if strings.HasPrefix(chatResp.Description, "Forbidden") {
			kind = telegramErrForbidden
		}
		return telegramChatInfo{}, &telegramAPIError{Kind: kind, Description: chatResp.Description}
	}

	return telegramChatInfo{
		Title:        chatResp.Result.Title,
		LinkedChatID: chatResp.Result.LinkedChatID,
	}, nil
}

// probeTelegramLinkedGroup determines the linked-group membership status of
// the bot for the given channel. It returns one of: "no_linked_group" (the
// channel has no discussion group configured), "ok" (linked group exists
// and the bot can read it — implied by getChat succeeding), or
// "bot_not_member" (linked group exists but the bot is not in it, so
// comment collection will be empty).
func (h *ConnectHandler) probeTelegramLinkedGroup(ctx context.Context, botToken string, linkedChatID int64) string {
	if linkedChatID == 0 {
		return "no_linked_group"
	}
	if _, err := h.telegramGetChat(ctx, botToken, strconv.FormatInt(linkedChatID, 10)); err != nil {
		return "bot_not_member"
	}
	return "ok"
}

// classifyAndWriteTelegramError maps a *telegramAPIError into an HTTP
// response. Network/parse failures → 502 (upstream broken, not the
// user's data); Forbidden-style API rejections → 403 with the upstream
// description; everything else (chat not found, etc.) → 400 with the
// description. Non-typed errors fall through to a generic 500 since
// callers should always pass through telegramGetChat.
func writeTelegramAPIError(w http.ResponseWriter, err error, fallback string) {
	var apiErr *telegramAPIError
	if !errors.As(err, &apiErr) {
		writeJSONError(w, http.StatusInternalServerError, fallback)
		return
	}
	switch apiErr.Kind {
	case telegramErrUnreachable:
		writeJSONError(w, http.StatusBadGateway, "telegram api unreachable, please retry")
	case telegramErrForbidden:
		msg := apiErr.Description
		if msg == "" {
			msg = "bot does not have access to this channel"
		}
		writeJSONError(w, http.StatusForbidden, msg)
	case telegramErrAPIRejected:
		msg := apiErr.Description
		if msg == "" {
			msg = fallback
		}
		writeJSONError(w, http.StatusBadRequest, msg)
	default:
		writeJSONError(w, http.StatusInternalServerError, fallback)
	}
}

// ConnectTelegram stores a Telegram channel integration using the system bot token (PermIntegrationsConnect required).
func (h *ConnectHandler) ConnectTelegram(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ConnectTelegram: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.ConnectTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ChannelId == "" {
		writeJSONError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	channelInfo, err := h.telegramGetChat(r.Context(), h.cfg.TelegramBotToken, req.ChannelId)
	if err != nil {
		var apiErr *telegramAPIError
		if errors.As(err, &apiErr) {
			slog.Warn("telegram getChat failed",
				"error", err,
				"error_kind", telegramErrKindName(apiErr.Kind),
				"channel_id", req.ChannelId)
		} else {
			slog.Warn("telegram getChat failed", "error", err, "channel_id", req.ChannelId)
		}
		writeTelegramAPIError(w, err, "bot does not have access to this channel")
		return
	}

	linkedStatus := h.probeTelegramLinkedGroup(r.Context(), h.cfg.TelegramBotToken, channelInfo.LinkedChatID)

	metadata := map[string]interface{}{
		"channel_title":       channelInfo.Title,
		"linked_group_status": linkedStatus,
	}
	if channelInfo.LinkedChatID != 0 {
		metadata["linked_chat_id"] = channelInfo.LinkedChatID
	}
	if req.TelegramUserId != nil && *req.TelegramUserId != "" {
		metadata["telegram_user_id"] = *req.TelegramUserId
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   bc.BusinessID,
		ActorID:      bc.UserID,
		Platform:     a2a.AgentTelegram,
		ExternalID:   req.ChannelId,
		AccessToken:  h.cfg.TelegramBotToken,
		Metadata:     metadata,
		ActorIP:      middleware.ClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		ParsedFormat: service.ParsedFormatBotToken,
	})
	if err != nil {
		slog.Error("failed to connect Telegram integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
}

// RefreshTelegramLinkedGroup re-probes a Telegram channel's linked
// discussion group and updates integration metadata with the latest
// linked_chat_id and linked_group_status. Used after the user invites
// the bot into the discussion group and wants the UI warning to clear
// without a full disconnect/reconnect cycle.
func (h *ConnectHandler) RefreshTelegramLinkedGroup(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "RefreshTelegramLinkedGroup: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.RefreshTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ChannelId == "" {
		writeJSONError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	integrations, err := h.integrationService.ListByBusinessAndPlatform(r.Context(), bc.BusinessID, a2a.AgentTelegram)
	if err != nil {
		slog.Error("failed to list telegram integrations for refresh", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	var target *domain.Integration
	for i := range integrations {
		if integrations[i].ExternalID == req.ChannelId {
			target = &integrations[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, http.StatusNotFound, "integration not found")
		return
	}

	channelInfo, err := h.telegramGetChat(r.Context(), h.cfg.TelegramBotToken, req.ChannelId)
	if err != nil {
		var apiErr *telegramAPIError
		if errors.As(err, &apiErr) {
			slog.Warn("telegram getChat failed during refresh",
				"error", err,
				"error_kind", telegramErrKindName(apiErr.Kind),
				"channel_id", req.ChannelId)
		} else {
			slog.Warn("telegram getChat failed during refresh", "error", err, "channel_id", req.ChannelId)
		}
		writeTelegramAPIError(w, err, "bot no longer has access to this channel")
		return
	}

	linkedStatus := h.probeTelegramLinkedGroup(r.Context(), h.cfg.TelegramBotToken, channelInfo.LinkedChatID)

	metadata := map[string]interface{}{}
	for k, v := range target.Metadata {
		metadata[k] = v
	}
	metadata["channel_title"] = channelInfo.Title
	metadata["linked_group_status"] = linkedStatus
	if channelInfo.LinkedChatID != 0 {
		metadata["linked_chat_id"] = channelInfo.LinkedChatID
	} else {
		delete(metadata, "linked_chat_id")
	}

	if err := h.integrationService.UpdateMetadata(r.Context(), target.ID, metadata); err != nil {
		slog.Error("failed to update telegram integration metadata", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to persist refresh")
		return
	}

	writeJSON(w, http.StatusOK, openapi.RefreshTelegramResponse{
		ChannelTitle:      channelInfo.Title,
		LinkedChatId:      channelInfo.LinkedChatID,
		LinkedGroupStatus: openapi.RefreshTelegramResponseLinkedGroupStatus(linkedStatus),
	})
}
