package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// TokenFetcher abstracts token retrieval for testability.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (accessToken string, err error)
}

// VKClient abstracts VK API operations for testability.
type VKClient interface {
	PublishPost(groupID, text string) (int64, error)
	UpdateGroupInfo(groupID, description string) error
	GetComments(groupID string, count int) ([]map[string]interface{}, error)
}

// VKClientFactory creates a VK client from an access token.
type VKClientFactory func(accessToken string) VKClient

// Handler implements a2a.Handler for the VK agent.
type Handler struct {
	tokens        TokenFetcher
	clientFactory VKClientFactory
}

// NewHandler creates a Handler with per-request token fetching.
func NewHandler(tokens TokenFetcher, factory VKClientFactory) *Handler {
	return &Handler{tokens: tokens, clientFactory: factory}
}

// Handle routes the ToolRequest to the appropriate VK API operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "vk__publish_post":
		return h.publishPost(ctx, req)
	case "vk__update_group_info":
		return h.updateGroupInfo(ctx, req)
	case "vk__get_comments":
		return h.getComments(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) getClient(ctx context.Context, req a2a.ToolRequest) (VKClient, string, error) {
	groupID, _ := req.Args["group_id"].(string)
	token, err := h.tokens.GetToken(ctx, req.BusinessID, "vk", groupID)
	if err != nil {
		return nil, "", fmt.Errorf("fetch token: %w", err)
	}
	return h.clientFactory(token), groupID, nil
}

func (h *Handler) publishPost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	client, groupID, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	text, _ := req.Args["text"].(string)

	postID, err := client.PublishPost(groupID, text)
	if err != nil {
		return nil, fmt.Errorf("vk: publish post: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"post_id": float64(postID)},
	}, nil
}

func (h *Handler) updateGroupInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	client, _, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	groupID, _ := req.Args["group_id"].(string)
	description, _ := req.Args["description"].(string)

	if err := client.UpdateGroupInfo(groupID, description); err != nil {
		return nil, fmt.Errorf("vk: update group info: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated"},
	}, nil
}

func (h *Handler) getComments(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	client, _, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	groupID, _ := req.Args["group_id"].(string)
	countF, _ := req.Args["count"].(float64)
	count := int(countF)
	if count == 0 {
		count = 20
	}

	comments, err := client.GetComments(groupID, count)
	if err != nil {
		return nil, fmt.Errorf("vk: get comments: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"comments": comments, "count": len(comments)},
	}, nil
}
