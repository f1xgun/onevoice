package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	vkapi "github.com/SevereCloud/vksdk/v3/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
)

// mockTokenFetcher is a test double for TokenFetcher.
type mockTokenFetcher struct {
	token     string
	userToken string
	extID     string
	err       error
}

func (m *mockTokenFetcher) GetToken(_ context.Context, _, _, _ string) (agent.TokenInfo, error) {
	if m.err != nil {
		return agent.TokenInfo{}, m.err
	}
	return agent.TokenInfo{
		AccessToken: m.token,
		UserToken:   m.userToken,
		ExternalID:  m.extID,
	}, nil
}

// mockVKClient is a test double for VKClient.
type mockVKClient struct {
	publishPostFn      func(groupID, text string) (int64, error)
	schedulePostFn     func(groupID, text string, publishDate int64) (int64, error)
	postPhotoFn        func(groupID string, photoURL, caption string) (int64, error)
	updateGroupInfoFn  func(groupID, description string) error
	getCommentsFn      func(groupID string, postID, count int) ([]map[string]interface{}, error)
	replyCommentFn     func(groupID string, postID, commentID int, text string) (int, error)
	deleteCommentFn    func(groupID string, commentID int) error
	getCommunityInfoFn func(groupID string) (map[string]interface{}, error)
	getWallPostsFn     func(groupID string, count int) ([]map[string]interface{}, int, error)
}

func (m *mockVKClient) PublishPost(groupID, text string) (int64, error) {
	return m.publishPostFn(groupID, text)
}
func (m *mockVKClient) SchedulePost(groupID, text string, publishDate int64) (int64, error) {
	return m.schedulePostFn(groupID, text, publishDate)
}
func (m *mockVKClient) PostPhoto(groupID, photoURL, caption string) (int64, error) {
	if m.postPhotoFn != nil {
		return m.postPhotoFn(groupID, photoURL, caption)
	}
	return 0, nil
}
func (m *mockVKClient) UpdateGroupInfo(groupID, description string) error {
	return m.updateGroupInfoFn(groupID, description)
}
func (m *mockVKClient) GetComments(groupID string, postID, count int) ([]map[string]interface{}, error) {
	return m.getCommentsFn(groupID, postID, count)
}
func (m *mockVKClient) ReplyComment(groupID string, postID, commentID int, text string) (int, error) {
	if m.replyCommentFn != nil {
		return m.replyCommentFn(groupID, postID, commentID, text)
	}
	return 0, nil
}
func (m *mockVKClient) DeleteComment(groupID string, commentID int) error {
	if m.deleteCommentFn != nil {
		return m.deleteCommentFn(groupID, commentID)
	}
	return nil
}
func (m *mockVKClient) GetCommunityInfo(groupID string) (map[string]interface{}, error) {
	if m.getCommunityInfoFn != nil {
		return m.getCommunityInfoFn(groupID)
	}
	return nil, nil
}
func (m *mockVKClient) GetWallPosts(groupID string, count int) (posts []map[string]interface{}, total int, err error) {
	if m.getWallPostsFn != nil {
		return m.getWallPostsFn(groupID, count)
	}
	return nil, 0, nil
}

// newFactory returns a factory that always yields the given client.
func newFactory(client agent.VKClient) agent.VKClientFactory {
	return func(_ string) agent.VKClient { return client }
}

func TestHandler_PublishPost(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		publishPostFn: func(groupID, text string) (int64, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, "Hello VK!", text)
			return 123, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t1",
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"text":     "Hello VK!",
			"group_id": "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, float64(123), resp.Result["post_id"])
}

func TestHandler_PublishPost_FetchesToken(t *testing.T) {
	tokens := &mockTokenFetcher{token: "vk-token-123"}
	var capturedToken string
	factory := func(token string) agent.VKClient {
		capturedToken = token
		return &mockVKClient{
			publishPostFn: func(groupID, text string) (int64, error) { return 42, nil },
		}
	}

	h := agent.NewHandler(tokens, factory, "")
	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t2",
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hello", "group_id": "g1"},
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, float64(42), resp.Result["post_id"])
	assert.Equal(t, "vk-token-123", capturedToken)
}

func TestHandler_TokenError(t *testing.T) {
	tokens := &mockTokenFetcher{err: fmt.Errorf("token not found")}
	h := agent.NewHandler(tokens, nil, "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"group_id": "g1"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetch token")
}

func TestHandler_UpdateGroupInfo(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		updateGroupInfoFn: func(groupID, description string) error {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, "Best coffee in town", description)
			return nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t3",
		Tool:       "vk__update_group_info",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id":    "-123456",
			"description": "Best coffee in town",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "updated", resp.Result["status"])
}

func TestHandler_GetComments(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getCommentsFn: func(groupID string, postID, count int) ([]map[string]interface{}, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, 10, count)
			return []map[string]interface{}{{"id": "1", "text": "nice!"}}, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t4",
		Tool:       "vk__get_comments",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
			"post_id":  float64(42),
			"count":    float64(10),
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, resp.Result["count"])
}

func TestHandler_GetComments_DefaultCount(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getCommentsFn: func(groupID string, postID, count int) ([]map[string]interface{}, error) {
			assert.Equal(t, 20, count)
			return nil, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t5",
		Tool:       "vk__get_comments",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"group_id": "-123456"},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestHandler_UnknownTool_ReturnsError(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, nil, "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t6",
		Tool:   "vk__nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// --- Error classification tests ---

func TestClassifyVKError_PermanentCode5(t *testing.T) {
	vkClient := &mockVKClient{
		publishPostFn: func(_, _ string) (int64, error) {
			return 0, &vkapi.Error{Code: 5, Message: "invalid token"}
		},
	}
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "group_id": "-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "permanent code 5 should be NonRetryableError")
}

func TestClassifyVKError_RateLimitCode6(t *testing.T) {
	vkClient := &mockVKClient{
		publishPostFn: func(_, _ string) (int64, error) {
			return 0, &vkapi.Error{Code: 6, Message: "too many requests"}
		},
	}
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "group_id": "-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "rate limit code 6 should be NonRetryableError")
}

func TestClassifyVKError_TransientCode1(t *testing.T) {
	vkClient := &mockVKClient{
		publishPostFn: func(_, _ string) (int64, error) {
			return 0, &vkapi.Error{Code: 1, Message: "unknown error"}
		},
	}
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "group_id": "-1"},
	})
	require.Error(t, err)
	assert.False(t, errors.Is(err, &a2a.NonRetryableError{}), "transient code 1 should NOT be NonRetryableError")
}

func TestClassifyVKError_NetworkError(t *testing.T) {
	vkClient := &mockVKClient{
		publishPostFn: func(_, _ string) (int64, error) {
			return 0, fmt.Errorf("connection refused")
		},
	}
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "group_id": "-1"},
	})
	require.Error(t, err)
	assert.False(t, errors.Is(err, &a2a.NonRetryableError{}), "network error should NOT be NonRetryableError")
}

func TestClassifyVKError_TokenFetchFailure(t *testing.T) {
	tokens := &mockTokenFetcher{err: fmt.Errorf("token not found")}
	h := agent.NewHandler(tokens, nil, "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"group_id": "g1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "token fetch failure should be NonRetryableError")
}

// --- Schedule post tests ---

func TestHandler_SchedulePost(t *testing.T) {
	futureTS := time.Now().Add(24 * time.Hour).Unix()
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		schedulePostFn: func(groupID, text string, publishDate int64) (int64, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, "Scheduled!", text)
			assert.Equal(t, futureTS, publishDate)
			return 999, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-sched",
		Tool:       "vk__schedule_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"text":         "Scheduled!",
			"publish_date": strconv.FormatInt(futureTS, 10),
			"group_id":     "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, float64(999), resp.Result["post_id"])
	assert.Equal(t, true, resp.Result["scheduled"])
}

func TestHandler_SchedulePost_RFC3339(t *testing.T) {
	futureTime := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	rfc3339Str := futureTime.Format(time.RFC3339)
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		schedulePostFn: func(groupID, text string, publishDate int64) (int64, error) {
			assert.Equal(t, futureTime.Unix(), publishDate)
			return 888, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-rfc",
		Tool:       "vk__schedule_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"text":         "RFC post",
			"publish_date": rfc3339Str,
			"group_id":     "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, float64(888), resp.Result["post_id"])
}

func TestHandler_SchedulePost_MissingText(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__schedule_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"publish_date": strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
			"group_id":     "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_SchedulePost_MissingDate(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__schedule_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"text":     "Hello",
			"group_id": "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish_date is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_SchedulePost_PastDate(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	pastTS := time.Now().Add(-1 * time.Hour).Unix()
	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__schedule_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"text":         "Hello",
			"publish_date": strconv.FormatInt(pastTS, 10),
			"group_id":     "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be in the future")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_SchedulePost_InvalidDate(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__schedule_post",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"text":         "Hello",
			"publish_date": "not-a-date",
			"group_id":     "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid publish_date")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

// --- Reply comment tests ---

func TestHandler_ReplyComment(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		replyCommentFn: func(groupID string, postID, commentID int, text string) (int, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, 42, postID)
			assert.Equal(t, 7, commentID)
			assert.Equal(t, "Great point!", text)
			return 99, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-reply",
		Tool:       "vk__reply_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"post_id":    float64(42),
			"comment_id": float64(7),
			"text":       "Great point!",
			"group_id":   "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, float64(99), resp.Result["comment_id"])
}

func TestHandler_ReplyComment_MissingPostID(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__reply_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"comment_id": float64(7),
			"text":       "reply",
			"group_id":   "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post_id is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_ReplyComment_MissingCommentID(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__reply_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"post_id":  float64(42),
			"text":     "reply",
			"group_id": "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "comment_id is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_ReplyComment_MissingText(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__reply_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"post_id":    float64(42),
			"comment_id": float64(7),
			"group_id":   "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is required for reply")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

// --- Delete comment tests ---

func TestHandler_DeleteComment(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		deleteCommentFn: func(groupID string, commentID int) error {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, 55, commentID)
			return nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-del",
		Tool:       "vk__delete_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"comment_id": float64(55),
			"group_id":   "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "deleted", resp.Result["status"])
}

func TestHandler_DeleteComment_MissingCommentID(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__delete_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "comment_id is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_DeleteComment_VKError(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		deleteCommentFn: func(groupID string, commentID int) error {
			return &vkapi.Error{Code: 15, Message: "access denied"}
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__delete_comment",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"comment_id": float64(55),
			"group_id":   "-123456",
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "VK error code 15 should be NonRetryableError")
}

// --- Post photo tests ---

func TestHandler_PostPhoto(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		postPhotoFn: func(groupID string, photoURL, caption string) (int64, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, "https://example.com/image.jpg", photoURL)
			assert.Equal(t, "Nice photo!", caption)
			return 777, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-photo",
		Tool:       "vk__post_photo",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"photo_url": "https://example.com/image.jpg",
			"caption":   "Nice photo!",
			"group_id":  "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, float64(777), resp.Result["post_id"])
}

func TestHandler_PostPhoto_MissingURL(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__post_photo",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"caption":  "No URL",
			"group_id": "-123456",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "photo_url is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_PostPhoto_VKError(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		postPhotoFn: func(groupID string, photoURL, caption string) (int64, error) {
			return 0, &vkapi.Error{Code: 5, Message: "invalid token"}
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__post_photo",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"photo_url": "https://example.com/image.jpg",
			"group_id":  "-123456",
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "VK error code 5 should be NonRetryableError")
}

// --- Community info tests ---

func TestHandler_GetCommunityInfo(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getCommunityInfoFn: func(groupID string) (map[string]interface{}, error) {
			assert.Equal(t, "-123456", groupID)
			return map[string]interface{}{
				"name":          "Test Community",
				"description":   "A test community",
				"members_count": 1500,
			}, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-info",
		Tool:       "vk__get_community_info",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "Test Community", resp.Result["name"])
	assert.Equal(t, "A test community", resp.Result["description"])
	assert.Equal(t, 1500, resp.Result["members_count"])
}

func TestHandler_GetCommunityInfo_MissingGroupID(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_community_info",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group_id is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

func TestHandler_GetCommunityInfo_VKError(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getCommunityInfoFn: func(groupID string) (map[string]interface{}, error) {
			return nil, &vkapi.Error{Code: 100, Message: "invalid param"}
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_community_info",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "VK error code 100 should be NonRetryableError")
}

// --- Wall posts tests ---

func TestHandler_GetWallPosts(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getWallPostsFn: func(groupID string, count int) ([]map[string]interface{}, int, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, 5, count)
			return []map[string]interface{}{
				{"id": 1, "text": "Post 1", "likes": 10},
				{"id": 2, "text": "Post 2", "likes": 20},
			}, 100, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-wall",
		Tool:       "vk__get_wall_posts",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
			"count":    float64(5),
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	posts := resp.Result["posts"].([]map[string]interface{})
	assert.Len(t, posts, 2)
	assert.Equal(t, 100, resp.Result["total"])
}

func TestHandler_GetWallPosts_DefaultCount(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getWallPostsFn: func(groupID string, count int) ([]map[string]interface{}, int, error) {
			assert.Equal(t, 10, count, "default count should be 10")
			return nil, 0, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-wall-def",
		Tool:       "vk__get_wall_posts",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestHandler_GetWallPosts_ClampCount(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	vkClient := &mockVKClient{
		getWallPostsFn: func(groupID string, count int) ([]map[string]interface{}, int, error) {
			assert.Equal(t, 100, count, "count > 100 should be clamped to 100")
			return nil, 0, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient), "")

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-wall-clamp",
		Tool:       "vk__get_wall_posts",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
			"count":    float64(500),
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestHandler_GetWallPosts_MissingGroupID(t *testing.T) {
	tokens := &mockTokenFetcher{token: "tok"}
	h := agent.NewHandler(tokens, newFactory(&mockVKClient{}), "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_wall_posts",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group_id is required")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}))
}

// --- getReadClient token priority tests ---
// Read operations (getComments, getCommunityInfo, getWallPosts) use getReadClient
// which has priority: user token > service key > community token.

func TestReadClient_PrefersUserToken(t *testing.T) {
	tokens := &mockTokenFetcher{token: "community-tok", userToken: "user-tok", extID: "-123456"}
	var capturedToken string
	factory := func(token string) agent.VKClient {
		capturedToken = token
		return &mockVKClient{
			getCommunityInfoFn: func(_ string) (map[string]interface{}, error) {
				return map[string]interface{}{"name": "Test"}, nil
			},
		}
	}
	h := agent.NewHandler(tokens, factory, "service-key-tok")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_community_info",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"group_id": "-123456"},
	})
	require.NoError(t, err)
	assert.Equal(t, "user-tok", capturedToken, "getReadClient should prefer user token over service key and community token")
}

func TestReadClient_FallsBackToServiceKey(t *testing.T) {
	tokens := &mockTokenFetcher{token: "community-tok", userToken: "", extID: "-123456"}
	var capturedToken string
	factory := func(token string) agent.VKClient {
		capturedToken = token
		return &mockVKClient{
			getCommunityInfoFn: func(_ string) (map[string]interface{}, error) {
				return map[string]interface{}{"name": "Test"}, nil
			},
		}
	}
	h := agent.NewHandler(tokens, factory, "service-key-tok")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_community_info",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"group_id": "-123456"},
	})
	require.NoError(t, err)
	assert.Equal(t, "service-key-tok", capturedToken, "getReadClient should use service key when user token is empty")
}

func TestReadClient_FallsBackToCommunityToken(t *testing.T) {
	tokens := &mockTokenFetcher{token: "community-tok", userToken: "", extID: "-123456"}
	var capturedToken string
	factory := func(token string) agent.VKClient {
		capturedToken = token
		return &mockVKClient{
			getWallPostsFn: func(_ string, _ int) ([]map[string]interface{}, int, error) {
				return nil, 0, nil
			},
		}
	}
	// No service key set
	h := agent.NewHandler(tokens, factory, "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_wall_posts",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"group_id": "-123456"},
	})
	require.NoError(t, err)
	assert.Equal(t, "community-tok", capturedToken, "getReadClient should fallback to community token when no user token and no service key")
}

func TestWriteClient_AlwaysUsesCommunityToken(t *testing.T) {
	// Write operations (publishPost, updateGroupInfo, etc.) should always use community token,
	// even when user token and service key are available.
	tokens := &mockTokenFetcher{token: "community-tok", userToken: "user-tok", extID: "-123456"}
	var capturedToken string
	factory := func(token string) agent.VKClient {
		capturedToken = token
		return &mockVKClient{
			publishPostFn: func(_, _ string) (int64, error) { return 1, nil },
		}
	}
	h := agent.NewHandler(tokens, factory, "service-key-tok")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__publish_post",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hello", "group_id": "-123456"},
	})
	require.NoError(t, err)
	assert.Equal(t, "community-tok", capturedToken, "write operations must use community token (AccessToken), not user token or service key")
}

func TestReadClient_ExternalIDFallback(t *testing.T) {
	// When group_id is empty, getReadClient should use ExternalID from TokenInfo.
	// getCommunityInfo properly uses the resolved group_id from getReadClient.
	tokens := &mockTokenFetcher{token: "tok", extID: "-999888"}
	var capturedGroupID string
	factory := func(_ string) agent.VKClient {
		return &mockVKClient{
			getCommunityInfoFn: func(groupID string) (map[string]interface{}, error) {
				capturedGroupID = groupID
				return map[string]interface{}{"name": "Test"}, nil
			},
		}
	}
	h := agent.NewHandler(tokens, factory, "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_community_info",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{}, // no group_id — should resolve from ExternalID
	})
	require.NoError(t, err)
	assert.Equal(t, "-999888", capturedGroupID, "should resolve group_id from TokenInfo.ExternalID")
}

func TestGetComments_UsesResolvedGroupID(t *testing.T) {
	// getComments uses getClient which resolves groupID with negative sign.
	tokens := &mockTokenFetcher{token: "tok", extID: "999888"}
	var capturedGroupID string
	factory := func(_ string) agent.VKClient {
		return &mockVKClient{
			getCommentsFn: func(groupID string, _, _ int) ([]map[string]interface{}, error) {
				capturedGroupID = groupID
				return nil, nil
			},
		}
	}
	h := agent.NewHandler(tokens, factory, "")

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "vk__get_comments",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"post_id": float64(1)}, // provide post_id to skip auto-fetch
	})
	require.NoError(t, err)
	assert.Equal(t, "-999888", capturedGroupID, "getComments should use resolved groupID with negative sign")
}
