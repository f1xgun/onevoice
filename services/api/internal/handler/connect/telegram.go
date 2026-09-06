package connect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
)

// telegramChatInfo holds the fields we care about from Telegram's getChat
// response: title, chat type, and — for channels — the linked discussion
// group's chat id. A non-zero LinkedChatID means subscribers' comments on
// channel posts are routed into that group, and the bot needs to be a member
// of that group (admin, ideally) to see them via getUpdates. Type is one of
// "channel" | "supergroup" | "group" | "private" and disambiguates the
// post-rights semantics (can_post_messages is meaningful for channels only).
type telegramChatInfo struct {
	Title        string
	Type         string
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
		Type         string `json:"type"`
		LinkedChatID int64  `json:"linked_chat_id"`
	} `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}

// telegramLoginFields is Telegram's documented Login Widget field set. Only
// these keys take part in the data-check-string; anything else the widget
// sends is ignored so the canonical string matches Telegram's signature.
var telegramLoginFields = []string{
	"auth_date", "first_name", "id", "last_name", "photo_url", "username",
}

// VerifyTelegramLogin verifies a Telegram Login Widget callback (JWT required).
func (h *ConnectHandler) VerifyTelegramLogin(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var req map[string]interface{}
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hash, _ := req["hash"].(string)
	if hash == "" {
		writeJSONError(w, http.StatusBadRequest, "hash is required")
		return
	}

	authDateStr := telegramFieldString(req["auth_date"])
	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil || time.Since(time.Unix(authDate, 0)) > 5*time.Minute {
		writeJSONError(w, http.StatusUnauthorized, "auth_date expired")
		return
	}

	parts := make([]string, 0, len(telegramLoginFields))
	for _, k := range telegramLoginFields {
		v, ok := req[k]
		if !ok {
			continue
		}
		parts = append(parts, k+"="+telegramFieldString(v))
	}
	checkString := strings.Join(parts, "\n")

	secretKey := sha256.Sum256([]byte(h.cfg.TelegramBotToken))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(checkString))
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(hash), []byte(expectedHash)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "invalid hash")
		return
	}

	delete(req, "hash")
	writeJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "user": req})
}

// telegramFieldString renders a Login Widget field as the canonical text
// Telegram itself signed: json.Number keeps integer fields (id, auth_date)
// as their integer literal instead of float64 scientific notation, and
// string fields pass through verbatim.
func telegramFieldString(v interface{}) string {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
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
	// return a safe localized message to the user.
	telegramErrAPIRejected
	// telegramErrRateLimited: Telegram returned a rate-limit / anti-bot
	// envelope (HTTP 429 "Too Many Requests: retry after N"). The single
	// shared system bot token means a fleet-wide verify storm can trip
	// Telegram's global limit, so this is transient and inconclusive — never
	// a signal that a channel is broken. Health fails soft to unknown.
	telegramErrRateLimited
)

// telegramTooManyRequests is the HTTP/error_code Telegram returns on a global
// rate-limit; the description is prefixed "Too Many Requests".
const telegramTooManyRequests = 429

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
	case telegramErrRateLimited:
		return "rate_limited"
	default:
		return "unknown"
	}
}

// classifyTelegramFalseOK maps a Bot API ok:false envelope (error_code,
// description) to a telegramErrKind. A Forbidden-style rejection is a
// conclusive access failure; a 429 / "Too Many Requests" is a transient
// rate-limit; everything else is a generic API rejection. Shared by getMe /
// getChat / getChatMember so all three classify the same envelope identically.
func classifyTelegramFalseOK(errorCode int, description string) telegramErrKind {
	switch {
	case hasForbiddenPrefix(description):
		return telegramErrForbidden
	case errorCode == telegramTooManyRequests || strings.HasPrefix(description, "Too Many Requests"):
		return telegramErrRateLimited
	default:
		return telegramErrAPIRejected
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
		return telegramChatInfo{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: redactURLErr(err)}
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return telegramChatInfo{}, &telegramAPIError{Kind: telegramErrUnreachable, Err: redactURLErr(err)}
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
		kind := classifyTelegramFalseOK(chatResp.ErrorCode, chatResp.Description)
		return telegramChatInfo{}, &telegramAPIError{Kind: kind, Description: chatResp.Description}
	}

	return telegramChatInfo{
		Title:        chatResp.Result.Title,
		Type:         chatResp.Result.Type,
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
		var apiErr *telegramAPIError
		if errors.As(err, &apiErr) && apiErr.Kind == telegramErrForbidden {
			return "bot_not_member"
		}
		return "unknown"
	}
	return "ok"
}

func writeTelegramAPIError(w http.ResponseWriter, r *http.Request, err error, fallbackKey string) {
	var apiErr *telegramAPIError
	if !errors.As(err, &apiErr) {
		writeJSONErrorKey(w, r, http.StatusInternalServerError, fallbackKey)
		return
	}
	switch apiErr.Kind {
	case telegramErrUnreachable:
		writeTelegramConnectError(w, r, http.StatusBadGateway, "unreachable", "connect.telegram.unreachable")
	case telegramErrRateLimited:
		writeTelegramConnectError(w, r, http.StatusTooManyRequests, "rate_limited", "connect.telegram.rate_limited")
	case telegramErrForbidden:
		writeTelegramConnectError(w, r, http.StatusForbidden, "forbidden", "connect.telegram.no_access")
	case telegramErrAPIRejected:
		writeTelegramConnectError(w, r, http.StatusBadRequest, "api_rejected", "connect.telegram.channel_unavailable")
	default:
		writeJSONErrorKey(w, r, http.StatusInternalServerError, fallbackKey)
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
		writeTelegramAPIError(w, r, err, "connect.telegram.no_access")
		return
	}

	health := h.EvaluateTelegramHealth(r.Context(), h.cfg.TelegramBotToken, req.ChannelId)
	switch {
	case health.Status == connhealth.StatusBroken && health.ReasonCode == connhealth.ReasonTelegramNotAdmin:
		writeTelegramConnectError(w, r, http.StatusConflict, "not_admin", "connect.telegram.not_admin")
		return
	case health.Status == connhealth.StatusBroken && health.ReasonCode == connhealth.ReasonTelegramNoPostRight:
		writeTelegramConnectError(w, r, http.StatusConflict, "no_post_rights", "connect.telegram.no_post_rights")
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
	// The body-supplied telegram_user_id is self-asserted — the backend has no
	// proof the caller controls that Telegram account — so it is stored under a
	// clearly-unverified key and NEVER under "telegram_user_id". The latter is the
	// off-app HITL authorization anchor (TelegramApprovalConsumer.tapperIsOwner)
	// and the owner-DM target (OwnerBriefService), and is set only from the
	// VERIFIED message.from.id captured by the /start owner-link handshake
	// (TelegramOwnerLinkService.Bind). Trusting the body value here would let an
	// admin bind an arbitrary owner id and approve off-app batches or receive
	// owner-private DMs as that account.
	if req.TelegramUserId != nil && *req.TelegramUserId != "" {
		metadata["telegram_user_id_unverified"] = *req.TelegramUserId
	}
	metadata = connhealth.MergeIntoMetadata(metadata, health)

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
		if errors.Is(err, domain.ErrIntegrationClaimedByOtherTenant) {
			slog.Warn("telegram connect rejected: channel already claimed by another organization",
				"channel_id", req.ChannelId,
				"business_id", bc.BusinessID)
			writeJSONErrorKey(w, r, http.StatusConflict, "connect.telegram.already_connected")
			return
		}
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to connect Telegram integration", "error", err)
		writeJSONErrorKey(w, r, http.StatusInternalServerError, "connect.telegram.connect_failed")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
}

// ownerLinkTTLSeconds mirrors service.ownerLinkTokenTTL for the response's
// expires_in_seconds field. Kept as a literal here (the handler package does not
// import the service TTL const) — both are 10 minutes; a change to one must
// change the other.
const ownerLinkTTLSeconds = 600

// StartTelegramOwnerLink mints a one-time /start deep link that binds the
// business's VERIFIED Telegram owner user-id. Authz mirrors ConnectTelegram
// exactly: an authenticated admin of THIS business with PermIntegrationsConnect;
// the business id comes from BusinessContext, never request input. The returned
// start_url is rendered by the FE for the admin to open or forward — the FIRST
// authentic tapper within the short TTL becomes the bound owner (documented
// first-tapper-wins residual, mitigated by admin-only mint + short TTL +
// single-use). Fail-closed: when the handshake is unconfigured (no bot username)
// this returns 404 rather than minting a dead link.
func (h *ConnectHandler) StartTelegramOwnerLink(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "StartTelegramOwnerLink: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	if h.ownerLink == nil || !h.ownerLink.Enabled() {
		writeJSONError(w, http.StatusNotFound, "owner-link handshake is not configured")
		return
	}

	startURL, err := h.ownerLink.Mint(r.Context(), bc.BusinessID)
	if err != nil {
		slog.ErrorContext(r.Context(), "StartTelegramOwnerLink: mint failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to create owner link")
		return
	}

	writeJSON(w, http.StatusOK, openapi.TelegramOwnerLinkResponse{
		StartUrl:         startURL,
		ExpiresInSeconds: ownerLinkTTLSeconds,
	})
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
		writeTelegramAPIError(w, r, err, "connect.telegram.no_access")
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

func writeTelegramConnectError(w http.ResponseWriter, r *http.Request, status int, reason, key string) {
	writeJSON(w, status, openapi.TelegramConnectError{Error: i18n.Tr(r.Context(), key), Reason: reason})
}
