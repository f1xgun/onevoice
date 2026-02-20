package agent_test

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVKClient struct {
	lastPostText string
	lastGroupID  string
}

func (f *fakeVKClient) PublishPost(groupID, text string) (int64, error) {
	f.lastPostText = text
	f.lastGroupID = groupID
	return 123, nil
}

func (f *fakeVKClient) UpdateGroupInfo(groupID, description string) error {
	f.lastGroupID = groupID
	return nil
}

func (f *fakeVKClient) GetComments(groupID string, count int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"id": "1", "text": "nice!"}}, nil
}

func TestHandler_PublishPost(t *testing.T) {
	client := &fakeVKClient{}
	h := agent.NewHandler(client)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t1",
		Tool:   "vk__publish_post",
		Args: map[string]interface{}{
			"text":     "Hello VK!",
			"group_id": "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "Hello VK!", client.lastPostText)
	assert.Equal(t, float64(123), resp.Result["post_id"])
}

func TestHandler_UpdateGroupInfo(t *testing.T) {
	client := &fakeVKClient{}
	h := agent.NewHandler(client)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t2",
		Tool:   "vk__update_group_info",
		Args: map[string]interface{}{
			"group_id":    "-123456",
			"description": "Best coffee in town",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "-123456", client.lastGroupID)
}

func TestHandler_GetComments(t *testing.T) {
	client := &fakeVKClient{}
	h := agent.NewHandler(client)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t3",
		Tool:   "vk__get_comments",
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

func TestHandler_UnknownTool_ReturnsError(t *testing.T) {
	h := agent.NewHandler(&fakeVKClient{})
	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t4",
		Tool:   "vk__nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
