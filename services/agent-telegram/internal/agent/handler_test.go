package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
)

type fakeSender struct {
	sentMessage string
	sentChatID  int64
}

func (f *fakeSender) SendMessage(chatID int64, text string) error {
	f.sentMessage = text
	f.sentChatID = chatID
	return nil
}

func TestHandler_SendChannelPost(t *testing.T) {
	sender := &fakeSender{}
	h := agent.NewHandler(sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t1",
		Tool:   "telegram__send_channel_post",
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
}

func TestHandler_SendNotification(t *testing.T) {
	sender := &fakeSender{}
	h := agent.NewHandler(sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t2",
		Tool:   "telegram__send_notification",
		Args: map[string]interface{}{
			"text":    "You have a new review!",
			"chat_id": "123456789",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "You have a new review!", sender.sentMessage)
}

func TestHandler_UnknownTool_ReturnsError(t *testing.T) {
	h := agent.NewHandler(&fakeSender{})

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t3",
		Tool:   "telegram__unknown_tool",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
