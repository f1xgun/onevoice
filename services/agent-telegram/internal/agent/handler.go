package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// TokenInfo aliases agentbase.TokenInfo so test files that construct
// agent.TokenInfo{...} continue to compile after the agentbase migration.
// New callers should use agentbase.TokenInfo directly.
type TokenInfo = agentbase.TokenInfo

// TokenFetcher aliases agentbase.TokenResolver — same interface contract,
// kept as a type alias so test mocks declared against agent.TokenFetcher
// remain byte-identical (import-path-only test changes).
type TokenFetcher = agentbase.TokenResolver

// Sender abstracts Telegram message sending for testability.
type Sender interface {
	SendMessage(chatID int64, text string) error
	SendPhoto(chatID int64, photoURL, caption string) error
	SendReply(chatID int64, messageID int, text string) error
	GetReviews(limit int) ([]map[string]interface{}, error)
}

// SenderFactory creates a Sender from a bot token.
type SenderFactory func(botToken string) (Sender, error)

// Handler is the Telegram agent's per-request processor. Its Handle method
// satisfies a2a.Exec and is wired into a2a.NewAgent from cmd/main.go.
type Handler struct {
	tokens        TokenFetcher
	senderFactory SenderFactory
	dispatcher    agentbase.Dispatcher
}

// NewHandler creates a Handler with the given TokenFetcher, SenderFactory, and
// agentbase.Dispatcher. The dispatcher owns the HITL dedupe gate and error
// classification — see pkg/agentbase. Tests may pass a dispatcher built with
// nil dedupe / nil classifier, or pass nil here to skip HITL entirely (a nil
// dispatcher acts as identity dispatch).
func NewHandler(tokens TokenFetcher, factory SenderFactory, dispatcher agentbase.Dispatcher) *Handler {
	return &Handler{tokens: tokens, senderFactory: factory, dispatcher: dispatcher}
}

// Handle routes the ToolRequest to the appropriate Telegram operation via the
// agentbase.Dispatcher (which runs the HITL dedupe gate, then routeTool, then
// classifies errors, then caches successful responses). When dispatcher is nil
// (legacy unit tests) we route directly through routeTool.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	if h.dispatcher == nil {
		resp, err := h.routeTool(ctx, req)
		return resp, ClassifyTelegramError(err)
	}
	return h.dispatcher.Dispatch(ctx, req, h.routeTool)
}

// routeTool dispatches to the per-tool implementation. The HITL dedupe gate
// and error classification are handled by the dispatcher in Handle; routeTool
// is exec callback shape required by agentbase.Dispatcher.
func (h *Handler) routeTool(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case tools.TelegramSendChannelPost:
		return h.sendChannelPost(ctx, req)
	case tools.TelegramSendChannelPhoto:
		return h.sendChannelPhoto(ctx, req)
	case tools.TelegramSendNotification:
		return h.sendNotification(ctx, req)
	case tools.TelegramGetReviews:
		return h.getReviews(ctx, req)
	case tools.TelegramReplyToComment:
		return h.replyToComment(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

// ClassifyTelegramError wraps permanent Telegram API errors as NonRetryableError.
// Checks error message strings since tgbotapi returns errors with descriptions.
// Exported so cmd/main.go can wire it through agentbase.FuncClassifier; the
// dispatcher invokes it after exec returns.
func ClassifyTelegramError(err error) error {
	return classifyTelegramError(err)
}

// classifyTelegramError is the internal implementation used by per-tool
// handlers (sendChannelPost wraps with %w).
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
	info, err := h.tokens.GetToken(ctx, req.BusinessID, a2a.AgentTelegram, externalID)
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
