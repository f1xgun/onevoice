package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	vkapi "github.com/SevereCloud/vksdk/v3/api"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// TokenInfo holds the resolved tokens for an integration.
type TokenInfo struct {
	AccessToken string // community token (for write operations)
	UserToken   string // user token (for read operations on private data)
	ExternalID  string // resolved external ID (group_id)
}

// TokenFetcher abstracts token retrieval for testability.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// VKClient abstracts VK API operations for testability.
type VKClient interface {
	PublishPost(groupID, text string) (int64, error)
	PostPhoto(groupID, photoURL, caption string) (int64, error)
	SchedulePost(groupID, text string, publishDate int64) (int64, error)
	UpdateGroupInfo(groupID, description string) error
	GetComments(groupID string, postID, count int) ([]map[string]interface{}, error)
	ReplyComment(groupID string, postID, commentID int, text string) (int, error)
	DeleteComment(groupID string, commentID int) error
	GetCommunityInfo(groupID string) (map[string]interface{}, error)
	GetWallPosts(groupID string, count int) ([]map[string]interface{}, int, error)
}

// VKClientFactory creates a VK client from an access token.
type VKClientFactory func(accessToken string) VKClient

// Handler implements a2a.Handler for the VK agent.
type Handler struct {
	tokens        TokenFetcher
	clientFactory VKClientFactory
	serviceKey    string // VK API service key for read-only operations (public data)
}

// NewHandler creates a Handler with per-request token fetching.
// serviceKey is optional — if provided, read operations use it instead of community token.
func NewHandler(tokens TokenFetcher, factory VKClientFactory, serviceKey string) *Handler {
	return &Handler{tokens: tokens, clientFactory: factory, serviceKey: serviceKey}
}

// Handle routes the ToolRequest to the appropriate VK API operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "vk__publish_post":
		return h.publishPost(ctx, req)
	case "vk__post_photo":
		return h.postPhoto(ctx, req)
	case "vk__update_group_info":
		return h.updateGroupInfo(ctx, req)
	case "vk__schedule_post":
		return h.schedulePost(ctx, req)
	case "vk__get_comments":
		return h.getComments(ctx, req)
	case "vk__reply_comment":
		return h.replyComment(ctx, req)
	case "vk__delete_comment":
		return h.deleteComment(ctx, req)
	case "vk__get_community_info":
		return h.getCommunityInfo(ctx, req)
	case "vk__get_wall_posts":
		return h.getWallPosts(ctx, req)
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

// ensureNegativeGroupID normalizes community ID for VK wall.* API methods.
// VK requires negative owner_id for communities (e.g. "-236912172").
func ensureNegativeGroupID(groupID string) string {
	if groupID == "" || strings.HasPrefix(groupID, "-") {
		return groupID
	}
	if _, err := strconv.ParseInt(groupID, 10, 64); err == nil {
		return "-" + groupID
	}
	return groupID
}

func (h *Handler) getClient(ctx context.Context, req a2a.ToolRequest) (VKClient, string, error) {
	groupID, _ := req.Args["group_id"].(string)
	info, err := h.tokens.GetToken(ctx, req.BusinessID, "vk", groupID)
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("fetch token: %w", err))
	}
	if groupID == "" {
		groupID = info.ExternalID
	}
	groupID = ensureNegativeGroupID(groupID)
	return h.clientFactory(info.AccessToken), groupID, nil
}

// getReadClient returns a client for read-only operations.
// Priority: user token > service key (open walls) > community token (limited reads).
// Community wall must be open/limited for service key reads to work.
func (h *Handler) getReadClient(ctx context.Context, req a2a.ToolRequest) (VKClient, string, error) {
	groupID, _ := req.Args["group_id"].(string)
	info, err := h.tokens.GetToken(ctx, req.BusinessID, "vk", groupID)
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("fetch token: %w", err))
	}
	if groupID == "" {
		groupID = info.ExternalID
	}
	groupID = ensureNegativeGroupID(groupID)
	// User token has broadest read access
	if info.UserToken != "" {
		return h.clientFactory(info.UserToken), groupID, nil
	}
	// Service key can read public/open community walls
	if h.serviceKey != "" {
		return h.clientFactory(h.serviceKey), groupID, nil
	}
	// Community token — limited read access (groups.getById works, wall.get does not)
	return h.clientFactory(info.AccessToken), groupID, nil
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

func (h *Handler) postPhoto(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	photoURL, _ := req.Args["photo_url"].(string)
	if photoURL == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: photo_url is required"))
	}
	caption, _ := req.Args["caption"].(string)

	client, groupID, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	postID, err := client.PostPhoto(groupID, photoURL, caption)
	if err != nil {
		return nil, fmt.Errorf("vk: post photo: %w", classifyVKError(err))
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
	client, groupID, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
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
	// wall.getComments is callable with the Mini-App service key (profile
	// type = app). VK ID tokens fail with error 1051; community tokens fail
	// with error 27 "group auth unavailable". We route all reads through
	// getReadClient: it prefers user token if the integration has one,
	// otherwise falls back to service key — which is the supported path.
	client, groupID, err := h.getReadClient(ctx, req)
	if err != nil {
		return nil, err
	}

	postIDf, _ := req.Args["post_id"].(float64)
	postID := int(postIDf)
	countF, _ := req.Args["count"].(float64)
	count := int(countF)
	if count == 0 {
		count = 20
	}

	if postID == 0 {
		posts, _, postsErr := client.GetWallPosts(groupID, 1)
		if postsErr != nil {
			return nil, fmt.Errorf("vk: get latest post: %w", classifyVKError(postsErr))
		}
		if len(posts) == 0 {
			return &a2a.ToolResponse{
				TaskID:  req.TaskID,
				Success: true,
				Result:  map[string]interface{}{"comments": []interface{}{}, "count": 0},
			}, nil
		}
		if id, ok := posts[0]["id"].(int); ok {
			postID = id
		}
	}

	comments, err := client.GetComments(groupID, postID, count)
	if err != nil {
		return nil, fmt.Errorf("vk: get comments: %w", classifyVKError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"comments": comments, "count": len(comments)},
	}, nil
}

func (h *Handler) replyComment(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	postIDf, _ := req.Args["post_id"].(float64)
	postID := int(postIDf)
	if postID == 0 {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: post_id is required and must be > 0"))
	}
	commentIDf, _ := req.Args["comment_id"].(float64)
	commentID := int(commentIDf)
	if commentID == 0 {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: comment_id is required and must be > 0"))
	}
	text, _ := req.Args["text"].(string)
	if text == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: text is required for reply"))
	}

	client, groupID, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	newCommentID, err := client.ReplyComment(groupID, postID, commentID, text)
	if err != nil {
		return nil, fmt.Errorf("vk: reply comment: %w", classifyVKError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"comment_id": float64(newCommentID)},
	}, nil
}

func (h *Handler) deleteComment(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	commentIDf, _ := req.Args["comment_id"].(float64)
	commentID := int(commentIDf)
	if commentID == 0 {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: comment_id is required and must be > 0"))
	}

	client, groupID, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := client.DeleteComment(groupID, commentID); err != nil {
		return nil, fmt.Errorf("vk: delete comment: %w", classifyVKError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "deleted"},
	}, nil
}

func (h *Handler) getCommunityInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	client, groupID, err := h.getReadClient(ctx, req)
	if err != nil {
		return nil, err
	}
	if groupID == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: group_id is required"))
	}

	info, err := client.GetCommunityInfo(groupID)
	if err != nil {
		return nil, fmt.Errorf("vk: get community info: %w", classifyVKError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  info,
	}, nil
}

func (h *Handler) getWallPosts(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	client, groupID, err := h.getReadClient(ctx, req)
	if err != nil {
		return nil, err
	}
	if groupID == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("vk: group_id is required"))
	}

	countF, _ := req.Args["count"].(float64)
	count := int(countF)
	if count <= 0 {
		count = 10
	}
	if count > 100 {
		count = 100
	}

	posts, total, err := client.GetWallPosts(groupID, count)
	if err != nil {
		return nil, fmt.Errorf("vk: get wall posts: %w", classifyVKError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"posts": posts, "total": total},
	}, nil
}
