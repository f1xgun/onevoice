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
// satisfies a2a.Exec and is wired into a2a.NewAgent from cmd/main.go. The
// dispatch chain (dispatcher fallback + per-tool routing + "unknown tool"
// error) lives in agentbase.NewRouter; this struct only owns the per-tool
// methods + their dependencies.
type Handler struct {
	tokens        TokenFetcher
	senderFactory SenderFactory
	exec          agentbase.ToolExec
}

// NewHandler creates a Handler with the given TokenFetcher, SenderFactory, and
// agentbase.Dispatcher. The dispatcher owns the HITL dedupe gate and error
// classification — see pkg/agentbase. Tests may pass a dispatcher built with
// nil dedupe / nil classifier, or pass nil here to skip HITL entirely. On the
// nil-dispatcher path the router applies ClassifyTelegramError as the fallback
// classifier so the contract matches the legacy Handle implementation.
func NewHandler(tokens TokenFetcher, factory SenderFactory, dispatcher agentbase.Dispatcher) *Handler {
	h := &Handler{tokens: tokens, senderFactory: factory}
	h.exec = agentbase.NewRouter(h.routes(), dispatcher, agentbase.FuncClassifier(ClassifyTelegramError))
	return h
}

// Handle is the a2a.Exec entry point — a thin shim over the router built in
// NewHandler. Kept as a method on Handler so the existing test surface
// (h.Handle(ctx, req)) continues to work byte-identically.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	return h.exec(ctx, req)
}

// routes binds the Telegram tool catalog to the Handler's per-tool methods.
// Called once from NewHandler to seed the agentbase.Router.
func (h *Handler) routes() map[string]agentbase.ToolExec {
	return map[string]agentbase.ToolExec{
		tools.TelegramSendChannelPost:  h.sendChannelPost,
		tools.TelegramSendChannelPhoto: h.sendChannelPhoto,
		tools.TelegramSendNotification: h.sendNotification,
		tools.TelegramGetReviews:       h.getReviews,
		tools.TelegramReplyToComment:   h.replyToComment,
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
		return nil, "", agentbase.WrapTokenFetchError(fmt.Errorf("fetch token: %w", err))
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
