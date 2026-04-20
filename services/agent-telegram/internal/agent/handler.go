package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// TokenInfo holds the resolved access token and the integration's external ID.
type TokenInfo struct {
	AccessToken string
	ExternalID  string
}

// TokenFetcher retrieves token info for a given business/platform/externalID combination.
// When externalID is empty, the first active integration for the platform is used.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// Sender abstracts Telegram message sending for testability.
type Sender interface {
	SendMessage(chatID int64, text string) error
	SendPhoto(chatID int64, photoURL, caption string) error
	SendReply(chatID int64, messageID int, text string) error
	GetReviews(limit int) ([]map[string]interface{}, error)
}

// SenderFactory creates a Sender from a bot token.
type SenderFactory func(botToken string) (Sender, error)

// Handler implements a2a.Handler for the Telegram agent.
type Handler struct {
	tokens        TokenFetcher
	senderFactory SenderFactory
	dedupe        *hitldedupe.DedupeClient // optional; nil skips the HITL dedupe gate
}

// NewHandler creates a Handler with the given TokenFetcher, SenderFactory, and
// optional dedupe client. Passing nil for dedupe disables the HITL dedupe
// gate — used by unit tests and dev-local environments without Redis.
func NewHandler(tokens TokenFetcher, factory SenderFactory, dedupe *hitldedupe.DedupeClient) *Handler {
	return &Handler{tokens: tokens, senderFactory: factory, dedupe: dedupe}
}

// Handle routes the ToolRequest to the appropriate Telegram operation.
// Before dispatching, if a dedupe client is configured AND req.ApprovalID is
// non-empty, the HITL dedupe gate is consulted. See dedupeGate for semantics.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	if resp, stop := h.dedupeGate(ctx, req); stop {
		return resp, nil
	}

	var (
		resp *a2a.ToolResponse
		err  error
	)
	switch req.Tool {
	case "telegram__send_channel_post":
		resp, err = h.sendChannelPost(ctx, req)
	case "telegram__send_channel_photo":
		resp, err = h.sendChannelPhoto(ctx, req)
	case "telegram__send_notification":
		resp, err = h.sendNotification(ctx, req)
	case "telegram__get_reviews":
		resp, err = h.getReviews(ctx, req)
	case "telegram__reply_to_comment":
		resp, err = h.replyToComment(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}

	h.dedupeStore(ctx, req, resp, err)
	return resp, err
}

// dedupeGate consults the Redis dedupe cache BEFORE tool dispatch when a HITL
// approval is in play. It returns (resp, true) when the caller should stop
// executing (in-flight elsewhere, or already-completed duplicate). On any
// error the gate is best-effort — we log and fall through rather than fail
// a turn because Redis blinked.
func (h *Handler) dedupeGate(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, bool) {
	if h.dedupe == nil || req.ApprovalID == "" {
		return nil, false
	}
	outcome, cached, err := h.dedupe.Claim(ctx, req.BusinessID, req.ApprovalID)
	if err != nil {
		slog.WarnContext(ctx, "hitl dedupe claim failed; proceeding without dedupe",
			"error", err, "business_id", req.BusinessID, "approval_id", req.ApprovalID)
		return nil, false
	}
	switch outcome {
	case hitldedupe.ClaimOutcomeInFlight:
		return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: already in flight"}, true
	case hitldedupe.ClaimOutcomeDuplicate:
		var cachedResp a2a.ToolResponse
		if uerr := json.Unmarshal([]byte(cached), &cachedResp); uerr != nil {
			slog.WarnContext(ctx, "hitl dedupe cached result malformed; returning generic duplicate",
				"error", uerr)
			return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: cached result unavailable"}, true
		}
		// The cached response was stored for the original TaskID; rewrite to
		// this replay's TaskID so the orchestrator correlates correctly.
		cachedResp.TaskID = req.TaskID
		return &cachedResp, true
	case hitldedupe.ClaimOutcomeClaimed, hitldedupe.ClaimOutcomeSkip:
		// Proceed with execution — no cached response.
	}
	return nil, false
}

// dedupeStore persists a successful ToolResponse under the HITL dedupe key so
// replays see ClaimOutcomeDuplicate. Errors and nil responses are NOT cached
// (a replay should be free to retry when the original failed).
func (h *Handler) dedupeStore(ctx context.Context, req a2a.ToolRequest, resp *a2a.ToolResponse, err error) {
	if h.dedupe == nil || req.ApprovalID == "" || err != nil || resp == nil {
		return
	}
	if serr := h.dedupe.Store(ctx, req.BusinessID, req.ApprovalID, resp); serr != nil {
		slog.WarnContext(ctx, "hitl dedupe store failed; result returned but not cached",
			"error", serr, "approval_id", req.ApprovalID)
	}
}

// classifyTelegramError wraps permanent Telegram API errors as NonRetryableError.
// Checks error message strings since tgbotapi returns errors with descriptions.
func classifyTelegramError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Permanent: unauthorized, forbidden
	if strings.Contains(msg, "Unauthorized") || strings.Contains(msg, "Forbidden") {
		return a2a.NewNonRetryableError(err)
	}
	// Rate-limited: too many requests — non-retryable, surface to user
	if strings.Contains(msg, "Too Many Requests") || strings.Contains(msg, "retry after") {
		return a2a.NewNonRetryableError(fmt.Errorf("telegram rate limit: %w", err))
	}
	// Chat/channel not found — permanent
	if strings.Contains(msg, "chat not found") || strings.Contains(msg, "Bad Request: chat_id is empty") {
		return a2a.NewNonRetryableError(err)
	}
	return err // transient (network, 5xx, etc.)
}

// getSender retrieves a Sender and the resolved externalID for a tool request.
// When externalID is empty, the first active integration for the business is used.
func (h *Handler) getSender(ctx context.Context, req a2a.ToolRequest, externalID string) (Sender, string, error) {
	info, err := h.tokens.GetToken(ctx, req.BusinessID, "telegram", externalID)
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("fetch token: %w", err))
	}
	sender, err := h.senderFactory(info.AccessToken)
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("create sender: %w", err))
	}
	return sender, info.ExternalID, nil
}

func (h *Handler) sendChannelPost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	sender, resolvedID, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}
	if channelIDStr == "" {
		channelIDStr = resolvedID
	}

	chatID, parseErr := strconv.ParseInt(channelIDStr, 10, 64)
	if parseErr != nil {
		chatID, err = strconv.ParseInt(resolvedID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, parseErr)
		}
	}

	if err := sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send message: %w", classifyTelegramError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}

func (h *Handler) sendChannelPhoto(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	photoURL, _ := req.Args["photo_url"].(string)
	caption, _ := req.Args["caption"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	sender, resolvedID, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}
	if channelIDStr == "" {
		channelIDStr = resolvedID
	}

	chatID, parseErr := strconv.ParseInt(channelIDStr, 10, 64)
	if parseErr != nil {
		chatID, err = strconv.ParseInt(resolvedID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, parseErr)
		}
	}

	if err := sender.SendPhoto(chatID, photoURL, caption); err != nil {
		return nil, fmt.Errorf("telegram: send photo: %w", classifyTelegramError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}

func (h *Handler) sendNotification(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	chatIDStr, _ := req.Args["chat_id"].(string)

	sender, resolvedID, err := h.getSender(ctx, req, chatIDStr)
	if err != nil {
		return nil, err
	}
	if chatIDStr == "" {
		chatIDStr = resolvedID
	}

	chatID, parseErr := strconv.ParseInt(chatIDStr, 10, 64)
	if parseErr != nil {
		chatID, err = strconv.ParseInt(resolvedID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("telegram: invalid chat_id %q: %w", chatIDStr, parseErr)
		}
	}

	if err := sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send notification: %w", classifyTelegramError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	limitF, _ := req.Args["limit"].(float64)
	limit := int(limitF)
	if limit == 0 {
		limit = 20
	}

	sender, _, err := h.getSender(ctx, req, "")
	if err != nil {
		return nil, err
	}

	reviews, err := sender.GetReviews(limit)
	if err != nil {
		return nil, fmt.Errorf("telegram: get reviews: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"reviews": reviews, "count": len(reviews)},
	}, nil
}

func (h *Handler) replyToComment(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	chatIDStr, _ := req.Args["chat_id"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	var messageID int
	switch v := req.Args["message_id"].(type) {
	case float64:
		messageID = int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("telegram: invalid message_id %q: %w", v, err)
		}
		messageID = parsed
	}

	slog.Info("telegram agent: reply_to_comment", "chat_id", chatIDStr, "message_id", messageID, "text_len", len(text))

	sender, resolvedID, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}

	if chatIDStr == "" {
		chatIDStr = resolvedID
	}
	chatID, parseErr := strconv.ParseInt(chatIDStr, 10, 64)
	if parseErr != nil {
		chatID, err = strconv.ParseInt(resolvedID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("telegram: invalid chat_id %q: %w", chatIDStr, parseErr)
		}
	}

	if err := sender.SendReply(chatID, messageID, text); err != nil {
		return nil, fmt.Errorf("telegram: reply to comment: %w", classifyTelegramError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "replied"},
	}, nil
}
