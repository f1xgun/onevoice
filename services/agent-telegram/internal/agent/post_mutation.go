package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

func (h *Handler) mutateChannelPost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	target, _ := req.Args["channel_id"].(string)
	id, err := a2a.GetIntParam(req.Args, "message_id", 0)
	number, isNumber := req.Args["message_id"].(float64)
	if err != nil || id <= 0 || (isNumber && number != float64(id)) || !isValidChatTarget(target) {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: explicit channel_id and positive integer message_id are required"))
	}
	text, textOK := req.Args["text"].(string)
	caption, captionOK := req.Args["is_caption"].(bool)
	if _, supplied := req.Args["is_caption"]; supplied && !captionOK {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: is_caption must be a boolean"))
	}
	if req.Tool == tools.TelegramEditChannelPost && (!textOK || (!caption && strings.TrimSpace(text) == "")) {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: text is required (may be empty for a media caption)"))
	}
	sender, resolved, err := h.getSender(ctx, req, target)
	if err != nil {
		return nil, err
	}
	// Never fall back for mutations: the same message ID may exist in another channel.
	if target != resolved {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: channel_id does not match the connected channel"))
	}
	sent := telegram.SentMessage{MessageID: id}
	status := "deleted"
	if req.Tool == tools.TelegramEditChannelPost {
		sent, err = sender.EditMessage(target, id, text, caption)
		status = "edited"
	} else {
		err = sender.DeleteMessage(target, id)
	}
	if err != nil {
		classified := classifyTelegramError(err)
		description := strings.ToLower(err.Error())
		if strings.Contains(description, "message can't be edited") || strings.Contains(description, "message can't be deleted") ||
			strings.Contains(description, "message to edit not found") || strings.Contains(description, "message to delete not found") {
			return &a2a.ToolResponse{TaskID: req.TaskID, Success: false, Error: err.Error(), Code: a2a.CodeOf(classified), Result: map[string]interface{}{"mutation_rejected": true}}, nil
		}
		return nil, fmt.Errorf("telegram: mutate post: %w", classified)
	}
	result := sentPostResult(sent, target)
	result["status"] = status
	return a2a.OK(req, result), nil
}
