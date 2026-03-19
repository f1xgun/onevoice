package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	vkapi "github.com/SevereCloud/vksdk/v3/api"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// TokenFetcher abstracts token retrieval for testability.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (accessToken string, err error)
}

// VKClient abstracts VK API operations for testability.
type VKClient interface {
	PublishPost(groupID, text string) (int64, error)
	SchedulePost(groupID, text string, publishDate int64) (int64, error)
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
	case "vk__schedule_post":
		return h.schedulePost(ctx, req)
	case "vk__get_comments":
		return h.getComments(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

// classifyVKError wraps permanent VK API errors as NonRetryableError.
// VK error codes: 5=invalid token, 15=access denied, 100=invalid param, 113=invalid user,
// 6=too many requests, 9=flood control (rate-limited, also non-retryable).
func classifyVKError(err error) error {
	var vkErr *vkapi.Error
	if !errors.As(err, &vkErr) {
		return err // network or non-VK error — transient, retryable
	}
	switch int(vkErr.Code) {
	case 5, 15, 100, 113: // permanent
		return a2a.NewNonRetryableError(err)
	case 6, 9: // rate-limited — don't retry, surface to user
		return a2a.NewNonRetryableError(fmt.Errorf("vk rate limit (code %d): %w", int(vkErr.Code), err))
	default:
		return err // transient
	}
}

func (h *Handler) getClient(ctx context.Context, req a2a.ToolRequest) (VKClient, string, error) {
	groupID, _ := req.Args["group_id"].(string)
	token, err := h.tokens.GetToken(ctx, req.BusinessID, "vk", groupID)
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("fetch token: %w", err))
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
		return nil, fmt.Errorf("vk: publish post: %w", classifyVKError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"post_id": float64(postID)},
	}, nil
}

func (h *Handler) schedulePost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	if text == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: text is required for scheduled post"))
	}
	publishDateStr, _ := req.Args["publish_date"].(string)
	if publishDateStr == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: publish_date is required"))
	}

	publishDate, err := parsePublishDate(publishDateStr)
	if err != nil {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: invalid publish_date: %w", err))
	}
	if publishDate <= time.Now().Unix() {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: publish_date must be in the future"))
	}

	client, groupID, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	postID, err := client.SchedulePost(groupID, text, publishDate)
	if err != nil {
		return nil, fmt.Errorf("vk: schedule post: %w", classifyVKError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"post_id": float64(postID), "scheduled": true},
	}, nil
}

// parsePublishDate accepts a Unix timestamp string or RFC3339 formatted date.
func parsePublishDate(s string) (int64, error) {
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ts, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, fmt.Errorf("expected Unix timestamp or RFC3339 format, got %q", s)
	}
	return t.Unix(), nil
}

func (h *Handler) updateGroupInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	client, _, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	groupID, _ := req.Args["group_id"].(string)
	description, _ := req.Args["description"].(string)

	if err := client.UpdateGroupInfo(groupID, description); err != nil {
		return nil, fmt.Errorf("vk: update group info: %w", classifyVKError(err))
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
		return nil, fmt.Errorf("vk: get comments: %w", classifyVKError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"comments": comments, "count": len(comments)},
	}, nil
}
