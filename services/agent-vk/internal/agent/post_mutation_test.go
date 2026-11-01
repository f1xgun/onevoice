package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
)

func (m *mockVKClient) EditPost(group string, id int, text string) error {
	return m.editPostFn(group, id, text)
}
func (m *mockVKClient) DeletePost(group string, id int) error { return m.deletePostFn(group, id) }

func TestPostMutationsOwnedCommunity(t *testing.T) {
	for _, tool := range []string{tools.VKEditPost, tools.VKDeletePost} {
		for _, target := range []string{"123", "999", ""} {
			t.Run(tool+target, func(t *testing.T) {
				called := false
				mutation := func(group string, id int) error {
					called = true
					require.Equal(t, "-123", group)
					require.Equal(t, 42, id)
					return nil
				}
				client := &mockVKClient{deletePostFn: mutation, editPostFn: func(group string, id int, text string) error {
					require.Equal(t, "caption", text)
					return mutation(group, id)
				}}
				factory := func(token string) agent.VKClient { require.Equal(t, "user-token", token); return client }
				h := agent.NewHandler(&mockTokenFetcher{token: "community-token", userToken: "user-token", extID: "123"}, factory, "service-token", nil)
				resp, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: tool, BusinessID: "biz", Args: map[string]any{"group_id": target, "post_id": float64(42), "text": "caption"}})
				if target != "123" {
					require.Error(t, err)
					require.False(t, called)
					return
				}
				require.NoError(t, err)
				require.True(t, called)
				require.Equal(t, float64(42), resp.Result["post_id"])
				require.Equal(t, "https://vk.com/wall-123_42", resp.Result["url"])
			})
		}
	}
}

func TestPostMutationNeedsUserToken(t *testing.T) {
	factory := func(_ string) agent.VKClient { t.Fatal("must not use service or community token"); return nil }
	h := agent.NewHandler(&mockTokenFetcher{token: "community-token", extID: "123"}, factory, "service-token", nil)
	_, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: tools.VKDeletePost, Args: map[string]any{"group_id": "123", "post_id": 42}})
	require.Equal(t, "integration_token_invalid", a2a.CodeOf(err))
}

func TestRetrySendDoesNotFallBackToAnotherCommunity(t *testing.T) {
	factory := func(string) agent.VKClient { t.Fatal("must not dispatch with fallback target"); return nil }
	h := agent.NewHandler(&mockTokenFetcher{token: "token", extID: "123"}, factory, "", nil)
	_, err := h.Handle(context.Background(), a2a.ToolRequest{BusinessID: "biz", Tool: tools.VKPublishPost, Args: map[string]interface{}{"group_id": "999", "text": "retry", "strict_target": true}})
	require.Equal(t, "channel_not_found", a2a.CodeOf(err))
}
