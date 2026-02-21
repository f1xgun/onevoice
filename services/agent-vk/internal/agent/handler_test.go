package agent_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
)

// mockTokenFetcher is a test double for TokenFetcher.
type mockTokenFetcher struct {
	token string
	err   error
}

func (m *mockTokenFetcher) GetToken(_ context.Context, _, _, _ string) (string, error) {
	return m.token, m.err
}

// mockVKClient is a test double for VKClient.
type mockVKClient struct {
	publishPostFn     func(groupID, text string) (int64, error)
	updateGroupInfoFn func(groupID, description string) error
	getCommentsFn     func(groupID string, count int) ([]map[string]interface{}, error)
}

func (m *mockVKClient) PublishPost(groupID, text string) (int64, error) {
	return m.publishPostFn(groupID, text)
}
func (m *mockVKClient) UpdateGroupInfo(groupID, description string) error {
	return m.updateGroupInfoFn(groupID, description)
}
func (m *mockVKClient) GetComments(groupID string, count int) ([]map[string]interface{}, error) {
	return m.getCommentsFn(groupID, count)
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
	h := agent.NewHandler(tokens, newFactory(vkClient))

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

	h := agent.NewHandler(tokens, factory)
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
	h := agent.NewHandler(tokens, nil)

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
	h := agent.NewHandler(tokens, newFactory(vkClient))

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
		getCommentsFn: func(groupID string, count int) ([]map[string]interface{}, error) {
			assert.Equal(t, "-123456", groupID)
			assert.Equal(t, 10, count)
			return []map[string]interface{}{{"id": "1", "text": "nice!"}}, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient))

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t4",
		Tool:       "vk__get_comments",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"group_id": "-123456",
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
		getCommentsFn: func(groupID string, count int) ([]map[string]interface{}, error) {
			assert.Equal(t, 20, count)
			return nil, nil
		},
	}
	h := agent.NewHandler(tokens, newFactory(vkClient))

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
	h := agent.NewHandler(tokens, nil)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t6",
		Tool:   "vk__nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
