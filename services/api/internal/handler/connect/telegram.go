package connect

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// connectTelegramRequest is the request body for ConnectTelegram.
type connectTelegramRequest struct {
	ChannelID      string `json:"channel_id"`
	TelegramUserID string `json:"telegram_user_id"`
}

// telegramChatInfo holds the fields we care about from Telegram's getChat
// response: title, and — for channels — the linked discussion group's chat
// id. A non-zero LinkedChatID means subscribers' comments on channel posts
// are routed into that group, and the bot needs to be a member of that
// group (admin, ideally) to see them via getUpdates.
type telegramChatInfo struct {
	Title        string
	LinkedChatID int64
}

// telegramGetChatResponse represents the Telegram Bot API getChat response.
type telegramGetChatResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Title        string `json:"title"`
		LinkedChatID int64  `json:"linked_chat_id"`
	} `json:"result"`
	Description string `json:"description"`
}

// refreshTelegramRequest is the request body for RefreshTelegramLinkedGroup.
type refreshTelegramRequest struct {
	ChannelID string `json:"channel_id"`
}

// VerifyTelegramLogin verifies a Telegram Login Widget callback (JWT required).
func (h *ConnectHandler) VerifyTelegramLogin(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Extract and remove hash
	hash, _ := req["hash"].(string)
	if hash == "" {
		writeJSONError(w, http.StatusBadRequest, "hash is required")
		return
	}

	// Check auth_date — JSON numbers arrive as float64
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

	// Build check string (exclude hash)
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

	// Verify HMAC-SHA256
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

// telegramGetChat calls the Telegram Bot API to validate bot access and
// fetch channel title + linked discussion chat id.
func (h *ConnectHandler) telegramGetChat(botToken, chatID string) (telegramChatInfo, error) {
	apiURL := fmt.Sprintf("%s/bot%s/getChat?chat_id=%s",
		h.telegramAPIBase(), botToken, url.QueryEscape(chatID))

	resp, err := h.httpClient.Get(apiURL)
	if err != nil {
		return telegramChatInfo{}, fmt.Errorf("telegram API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return telegramChatInfo{}, fmt.Errorf("read response body: %w", err)
	}

	var chatResp telegramGetChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return telegramChatInfo{}, fmt.Errorf("parse telegram response: %w", err)
	}

	if !chatResp.OK {
		return telegramChatInfo{}, fmt.Errorf("telegram API error: %s", chatResp.Description)
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
func (h *ConnectHandler) probeTelegramLinkedGroup(botToken string, linkedChatID int64) string {
	if linkedChatID == 0 {
		return "no_linked_group"
	}
	if _, err := h.telegramGetChat(botToken, strconv.FormatInt(linkedChatID, 10)); err != nil {
		return "bot_not_member"
	}
	return "ok"
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

	var req connectTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ChannelID == "" {
		writeJSONError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	// Validate bot access and fetch channel title + linked discussion chat
	channelInfo, err := h.telegramGetChat(h.cfg.TelegramBotToken, req.ChannelID)
	if err != nil {
		slog.Warn("telegram getChat failed", "error", err, "channel_id", req.ChannelID)
		writeJSONError(w, http.StatusBadRequest, "bot does not have access to this channel")
		return
	}

	linkedStatus := h.probeTelegramLinkedGroup(h.cfg.TelegramBotToken, channelInfo.LinkedChatID)

	metadata := map[string]interface{}{
		"channel_title":       channelInfo.Title,
		"linked_group_status": linkedStatus,
	}
	if channelInfo.LinkedChatID != 0 {
		metadata["linked_chat_id"] = channelInfo.LinkedChatID
	}
	if req.TelegramUserID != "" {
		metadata["telegram_user_id"] = req.TelegramUserID
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  bc.BusinessID,
		Platform:    a2a.AgentTelegram,
		ExternalID:  req.ChannelID,
		AccessToken: h.cfg.TelegramBotToken,
		Metadata:    metadata,
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

	var req refreshTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ChannelID == "" {
		writeJSONError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	// Find the specific integration by external_id.
	integrations, err := h.integrationService.ListByBusinessAndPlatform(r.Context(), bc.BusinessID, a2a.AgentTelegram)
	if err != nil {
		slog.Error("failed to list telegram integrations for refresh", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	var target *domain.Integration
	for i := range integrations {
		if integrations[i].ExternalID == req.ChannelID {
			target = &integrations[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, http.StatusNotFound, "integration not found")
		return
	}

	channelInfo, err := h.telegramGetChat(h.cfg.TelegramBotToken, req.ChannelID)
	if err != nil {
		slog.Warn("telegram getChat failed during refresh", "error", err, "channel_id", req.ChannelID)
		writeJSONError(w, http.StatusBadGateway, "bot no longer has access to this channel")
		return
	}

	linkedStatus := h.probeTelegramLinkedGroup(h.cfg.TelegramBotToken, channelInfo.LinkedChatID)

	// Merge into existing metadata so unrelated keys (telegram_user_id etc.)
	// are preserved.
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"linked_chat_id":      channelInfo.LinkedChatID,
		"linked_group_status": linkedStatus,
		"channel_title":       channelInfo.Title,
	})
}
