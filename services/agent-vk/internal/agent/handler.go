package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	vkapi "github.com/SevereCloud/vksdk/v3/api"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// TokenInfo aliases agentbase.TokenInfo so existing tests compile after the
// agentbase migration. AccessToken is the community token (write operations);
// UserToken is the user token (read operations on private data); ExternalID is
// the resolved group_id. agentbase.TokenInfo carries the same three fields.
type TokenInfo = agentbase.TokenInfo

// TokenFetcher aliases agentbase.TokenResolver — kept for test
// compatibility (import-path/wiring-only changes in handler_test.go).
type TokenFetcher = agentbase.TokenResolver

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

// Handler is the VK agent's per-request processor. Its Handle method
// satisfies a2a.Exec and is wired into a2a.NewAgent from cmd/main.go. The
// dispatch chain (dispatcher fallback + per-tool routing + "unknown tool"
// error) lives in agentbase.NewRouter.
type Handler struct {
	tokens        TokenFetcher
	clientFactory VKClientFactory
	serviceKey    string // VK API service key for read-only operations (public data)
	exec          agentbase.ToolExec
}

// NewHandler creates a Handler with per-request token fetching.
// serviceKey is optional — if provided, read operations use it instead of
// community token. dispatcher is optional — passing nil disables HITL dedupe;
// on that path the router applies ClassifyVKError as the fallback classifier.
func NewHandler(tokens TokenFetcher, factory VKClientFactory, serviceKey string, dispatcher agentbase.Dispatcher) *Handler {
	h := &Handler{tokens: tokens, clientFactory: factory, serviceKey: serviceKey}
	h.exec = agentbase.NewRouter(h.routes(), dispatcher, agentbase.FuncClassifier(ClassifyVKError))
	return h
}

// Handle is the a2a.Exec entry point — a thin shim over the router built in
// NewHandler.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	return h.exec(ctx, req)
}

// routes binds the VK tool catalog to the Handler's per-tool methods.
func (h *Handler) routes() map[string]agentbase.ToolExec {
	return map[string]agentbase.ToolExec{
		tools.VKPublishPost:      h.publishPost,
		tools.VKPostPhoto:        h.postPhoto,
		tools.VKUpdateGroupInfo:  h.updateGroupInfo,
		tools.VKSchedulePost:     h.schedulePost,
		tools.VKGetComments:      h.getComments,
		tools.VKReplyComment:     h.replyComment,
		tools.VKDeleteComment:    h.deleteComment,
		tools.VKGetCommunityInfo: h.getCommunityInfo,
		tools.VKGetWallPosts:     h.getWallPosts,
	}
}

// ClassifyVKError is the exported entry point used by cmd/main.go to wire the
// dispatcher's classifier via agentbase.FuncClassifier.
func ClassifyVKError(err error) error {
	return classifyVKError(err)
}

// classifyVKError wraps permanent VK API errors as NonRetryableError.
// VK error codes: 5=invalid token, 15=access denied, 100=invalid param, 113=invalid user,
// 6=too many requests, 9=flood control (rate-limited, also non-retryable).
func classifyVKError(err error) error {
	if err == nil {
		return nil
	}
	// Preserve an already-typed code from upstream wrapping sites.
	if a2a.CodeOf(err) != "" {
		return err
	}
	var vkErr *vkapi.Error
	if !errors.As(err, &vkErr) {
		// network or non-VK error — transient, retryable; carry the code.
		return a2a.NewCodedError("transient", err)
	}
	switch int(vkErr.Code) {
	case vkErrInvalidToken, vkErrAccessDenied, vkErrInvalidUser:
		return a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(err))
	case vkErrTooManyReqs, vkErrFloodControl:
		return a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(fmt.Errorf("vk rate limit (code %d): %w", int(vkErr.Code), err)))
	case vkErrInvalidParam:
		lower := strings.ToLower(vkErr.Message)
		if strings.Contains(lower, "community") || strings.Contains(lower, "group") {
			return a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(err))
		}
		// Generic code 100 — treat as transient (retryable) to preserve
		// the legacy "retry on unknown VK 100" semantics.
		return a2a.NewCodedError("transient", err)
	default:
		return a2a.NewCodedError("transient", err)
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
	info, err := agentbase.FetchToken(ctx, h.tokens, req.BusinessID, a2a.AgentVK, groupID)
	if err != nil {
		return nil, "", err
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
	info, err := agentbase.FetchToken(ctx, h.tokens, req.BusinessID, a2a.AgentVK, groupID)
	if err != nil {
		return nil, "", err
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
	count, err := a2a.GetIntParam(req.Args, "count", defaultCommentCount)
	if err != nil {
		slog.Warn("vk agent: invalid count param, using default", "error", err)
		count = defaultCommentCount
	}
	if count == 0 {
		count = defaultCommentCount
	}

	if postID == 0 {
		// Sync mode: walk recent posts and gather comments from any that
		// have at least one. Capping at 20 keeps the call bounded; the
		// review UI only paginates back so far anyway. We previously took
		// just the latest post, which silently dropped reviews as soon as
		// a newer post (with no comments) appeared on the wall.
		posts, _, postsErr := client.GetWallPosts(groupID, recentPostsBatchSize)
		if postsErr != nil {
			return nil, fmt.Errorf("vk: get latest posts: %w", classifyVKError(postsErr))
		}
		merged := make([]map[string]interface{}, 0)
		for _, p := range posts {
			cmtCount, _ := p["comments"].(int)
			if cmtCount == 0 {
				continue
			}
			id, ok := p["id"].(int)
			if !ok {
				continue
			}
			cs, cErr := client.GetComments(groupID, id, count)
			if cErr != nil {
				return nil, fmt.Errorf("vk: get comments for post %d: %w", id, classifyVKError(cErr))
			}
			merged = append(merged, cs...)
		}
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result:  map[string]interface{}{"comments": merged, "count": len(merged)},
		}, nil
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

	count, err := a2a.GetIntParam(req.Args, "count", defaultWallPostCount)
	if err != nil {
		slog.Warn("vk agent: invalid count param, using default", "error", err)
		count = defaultWallPostCount
	}
	if count <= 0 {
		count = defaultWallPostCount
	}
	if count > 100 {
		count = maxWallPostCount
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
