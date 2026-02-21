package agent

import (
	"context"
	"fmt"
	"strconv"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// TokenFetcher retrieves an access token for a given business/platform/externalID combination.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (accessToken string, err error)
}

// Sender abstracts Telegram message sending for testability.
type Sender interface {
	SendMessage(chatID int64, text string) error
}

// SenderFactory creates a Sender from a bot token.
type SenderFactory func(botToken string) (Sender, error)

// Handler implements a2a.Handler for the Telegram agent.
type Handler struct {
	tokens        TokenFetcher
	senderFactory SenderFactory
}

// NewHandler creates a Handler with the given TokenFetcher and SenderFactory.
func NewHandler(tokens TokenFetcher, factory SenderFactory) *Handler {
	return &Handler{tokens: tokens, senderFactory: factory}
}

// Handle routes the ToolRequest to the appropriate Telegram operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "telegram__send_channel_post":
		return h.sendChannelPost(ctx, req)
	case "telegram__send_notification":
		return h.sendNotification(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) getSender(ctx context.Context, req a2a.ToolRequest, externalID string) (Sender, error) {
	token, err := h.tokens.GetToken(ctx, req.BusinessID, "telegram", externalID)
	if err != nil {
		return nil, fmt.Errorf("fetch token: %w", err)
	}
	sender, err := h.senderFactory(token)
	if err != nil {
		return nil, fmt.Errorf("create sender: %w", err)
	}
	return sender, nil
}

func (h *Handler) sendChannelPost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	sender, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}

	chatID, err := strconv.ParseInt(channelIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, err)
	}

	if err := sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send message: %w", err)
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

	sender, err := h.getSender(ctx, req, chatIDStr)
	if err != nil {
		return nil, err
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid chat_id %q: %w", chatIDStr, err)
	}

	if err := sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send notification: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}
