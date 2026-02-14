package agent

import (
	"context"
	"fmt"
	"strconv"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// Sender abstracts Telegram message sending for testability.
type Sender interface {
	SendMessage(chatID int64, text string) error
}

// Handler implements a2a.Handler for the Telegram agent.
type Handler struct {
	sender Sender
}

// NewHandler creates a Handler with the given Telegram sender.
func NewHandler(sender Sender) *Handler {
	return &Handler{sender: sender}
}

// Handle routes the ToolRequest to the appropriate Telegram operation.
func (h *Handler) Handle(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "telegram__send_channel_post":
		return h.sendChannelPost(req)
	case "telegram__send_notification":
		return h.sendNotification(req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) sendChannelPost(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	chatID, err := strconv.ParseInt(channelIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, err)
	}

	if err := h.sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send message: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}

func (h *Handler) sendNotification(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	chatIDStr, _ := req.Args["chat_id"].(string)

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid chat_id %q: %w", chatIDStr, err)
	}

	if err := h.sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send notification: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}
