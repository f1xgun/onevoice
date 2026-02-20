package agent_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
)

// fakeTokenFetcher records the last call and returns a preset token.
type fakeTokenFetcher struct {
	token      string
	err        error
	lastBizID  string
	lastPlatform string
	lastExtID  string
}

func (f *fakeTokenFetcher) GetToken(_ context.Context, businessID, platform, externalID string) (string, error) {
	f.lastBizID = businessID
	f.lastPlatform = platform
	f.lastExtID = externalID
	return f.token, f.err
}

// fakeSender records the last message sent.
type fakeSender struct {
	sentMessage string
	sentChatID  int64
}

func (f *fakeSender) SendMessage(chatID int64, text string) error {
	f.sentMessage = text
	f.sentChatID = chatID
	return nil
}

func newHandlerWithSender(fetcher agent.TokenFetcher, sender *fakeSender) *agent.Handler {
	factory := func(_ string) (agent.Sender, error) {
		return sender, nil
	}
	return agent.NewHandler(fetcher, factory)
}

func TestHandler_SendChannelPost_FetchesTokenPerRequest(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token-123"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t1",
		BusinessID: "biz-42",
		Tool:       "telegram__send_channel_post",
		Args: map[string]interface{}{
			"text":       "Hello, channel!",
			"channel_id": "-1001234567890",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "Hello, channel!", sender.sentMessage)
	assert.Equal(t, int64(-1001234567890), sender.sentChatID)

	// Verify the token was fetched with the correct businessID and platform
	assert.Equal(t, "biz-42", fetcher.lastBizID)
	assert.Equal(t, "telegram", fetcher.lastPlatform)
	assert.Equal(t, "-1001234567890", fetcher.lastExtID)
}

func TestHandler_SendNotification_FetchesTokenPerRequest(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token-456"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t2",
		BusinessID: "biz-99",
		Tool:       "telegram__send_notification",
		Args: map[string]interface{}{
			"text":    "You have a new review!",
			"chat_id": "123456789",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "You have a new review!", sender.sentMessage)
	assert.Equal(t, int64(123456789), sender.sentChatID)

	assert.Equal(t, "biz-99", fetcher.lastBizID)
	assert.Equal(t, "telegram", fetcher.lastPlatform)
}

func TestHandler_TokenFetchError_ReturnsError(t *testing.T) {
	fetcher := &fakeTokenFetcher{err: fmt.Errorf("integration not found")}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t3",
		Tool:   "telegram__send_channel_post",
		Args: map[string]interface{}{
			"text":       "Hello",
			"channel_id": "-1001234567890",
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch token")
}

func TestHandler_UnknownTool_ReturnsError(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok"}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) {
		return &fakeSender{}, nil
	})

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t4",
		Tool:   "telegram__unknown_tool",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
