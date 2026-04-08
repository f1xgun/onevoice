package agent

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTokenFetcher struct{}

func (m *mockTokenFetcher) GetToken(_ context.Context, _, _, _ string) (TokenInfo, error) {
	return TokenInfo{AccessToken: "test-token", ExternalID: "locations/123"}, nil
}

type mockGBPClient struct{}

func TestHandler_Handle_UnknownTool(t *testing.T) {
	h := NewHandler(&mockTokenFetcher{}, func(token string) GBPClient {
		return &mockGBPClient{}
	})

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__nonexistent",
		BusinessID: "biz-1",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
	assert.Nil(t, resp)
}
