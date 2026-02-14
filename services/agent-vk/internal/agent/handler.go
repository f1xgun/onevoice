package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// VKClient abstracts VK API operations for testability.
type VKClient interface {
	PublishPost(groupID, text string) (int64, error)
	UpdateGroupInfo(groupID, description string) error
	GetComments(groupID string, count int) ([]map[string]interface{}, error)
}

// Handler implements a2a.Handler for the VK agent.
type Handler struct {
	client VKClient
}

// NewHandler creates a Handler with the given VK client.
func NewHandler(client VKClient) *Handler {
	return &Handler{client: client}
}

// Handle routes the ToolRequest to the appropriate VK API operation.
func (h *Handler) Handle(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "vk__publish_post":
		return h.publishPost(req)
	case "vk__update_group_info":
		return h.updateGroupInfo(req)
	case "vk__get_comments":
		return h.getComments(req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) publishPost(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	groupID, _ := req.Args["group_id"].(string)

	postID, err := h.client.PublishPost(groupID, text)
	if err != nil {
		return nil, fmt.Errorf("vk: publish post: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"post_id": float64(postID)},
	}, nil
}

func (h *Handler) updateGroupInfo(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	groupID, _ := req.Args["group_id"].(string)
	description, _ := req.Args["description"].(string)

	if err := h.client.UpdateGroupInfo(groupID, description); err != nil {
		return nil, fmt.Errorf("vk: update group info: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated"},
	}, nil
}

func (h *Handler) getComments(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	groupID, _ := req.Args["group_id"].(string)
	countF, _ := req.Args["count"].(float64)
	count := int(countF)
	if count == 0 {
		count = 20
	}

	comments, err := h.client.GetComments(groupID, count)
	if err != nil {
		return nil, fmt.Errorf("vk: get comments: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"comments": comments, "count": len(comments)},
	}, nil
}
