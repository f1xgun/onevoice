package agent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

func (f *fakeSender) EditMessage(chat string, id int, text string, caption bool) (telegram.SentMessage, error) {
	f.sentChat = chat
	f.replyMessageID = id
	f.sentMessage = text
	if caption {
		f.sentCaption = text
	}
	return f.receipt, nil
}
func (f *fakeSender) DeleteMessage(chat string, id int) error {
	f.sentChat = chat
	f.replyMessageID = id
	return nil
}

func TestPostMutationsOwnedChannel(t *testing.T) {
	for _, tool := range []string{tools.TelegramEditChannelPost, tools.TelegramDeleteChannelPost} {
		for _, target := range []string{"@own", "@foreign", ""} {
			t.Run(tool+target, func(t *testing.T) {
				sender := &fakeSender{receipt: telegram.SentMessage{MessageID: 42, ChatUsername: "own"}}
				h := newHandlerWithSender(&firstActiveTokenFetcher{token: "business-token", ownExternalID: "@own"}, sender)
				resp, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: tool, BusinessID: "biz", Args: map[string]any{"channel_id": target, "message_id": float64(42), "text": "caption", "is_caption": true}})
				if target != "@own" {
					require.Error(t, err)
					require.Empty(t, sender.sentChat)
					return
				}
				require.NoError(t, err)
				require.Equal(t, "@own", sender.sentChat)
				require.Equal(t, 42, sender.replyMessageID)
				require.Equal(t, 42, resp.Result["message_id"])
				require.Equal(t, "https://t.me/own/42", resp.Result["url"])
				if tool == tools.TelegramEditChannelPost {
					require.Equal(t, "caption", sender.sentCaption)
				}
			})
		}
	}
}

func TestPostMutationRejectsFractionalID(t *testing.T) {
	sender := &fakeSender{}
	_, err := newHandlerWithSender(&fakeTokenFetcher{}, sender).Handle(context.Background(), a2a.ToolRequest{Tool: tools.TelegramDeleteChannelPost, Args: map[string]any{"channel_id": "@own", "message_id": 42.5}})
	require.Error(t, err)
	require.Empty(t, sender.sentChat)
}

func TestPostMutationPermanentTelegramErrors(t *testing.T) {
	for _, description := range []string{"Bad Request: message can't be edited", "Bad Request: message can't be deleted", "Bad Request: message to edit not found", "Bad Request: message to delete not found"} {
		t.Run(description, func(t *testing.T) {
			err := agent.ClassifyTelegramError(fmt.Errorf("%s", description))
			require.True(t, errors.Is(err, &a2a.NonRetryableError{}))
		})
	}
}

func TestRetrySendDoesNotFallBackToAnotherChannel(t *testing.T) {
	sender := &fakeSender{}
	h := newHandlerWithSender(&firstActiveTokenFetcher{token: "token", ownExternalID: "@own"}, sender)
	_, err := h.Handle(context.Background(), a2a.ToolRequest{BusinessID: "biz", Tool: tools.TelegramSendChannelPost, Args: map[string]interface{}{"channel_id": "@old", "text": "retry", "strict_target": true}})
	require.Equal(t, "channel_not_found", a2a.CodeOf(err))
	require.Empty(t, sender.sentChat)
}

func TestRetrySendDoesNotAutomaticallyRetryAmbiguousFailure(t *testing.T) {
	h := newHandlerWithErrSender(&fakeTokenFetcher{token: "token"}, errors.New("connection reset after send"))
	_, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: tools.TelegramSendChannelPost, Args: map[string]interface{}{"channel_id": "@own", "text": "retry", "strict_target": true}})
	require.ErrorIs(t, err, &a2a.NonRetryableError{})
}

func (e *errSender) EditMessage(_ string, _ int, _ string, _ bool) (telegram.SentMessage, error) {
	return telegram.SentMessage{}, e.err
}
func (e *errSender) DeleteMessage(_ string, _ int) error { return e.err }

func TestPostMutationRejectionReceipt(t *testing.T) {
	for _, tc := range []struct {
		description string
		rejected    bool
	}{
		{"Bad Request: message can't be deleted", true},
		{"Bad Request: message can't be edited", true},
		{"Bad Request: message to delete not found", true},
		{"connection reset after send", false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			h := newHandlerWithErrSender(&fakeTokenFetcher{token: "token"}, errors.New(tc.description))
			response, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: tools.TelegramDeleteChannelPost, Args: map[string]interface{}{"channel_id": "@own", "message_id": 42}})
			if !tc.rejected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.False(t, response.Success)
			require.Equal(t, "transient", response.Code)
			require.Equal(t, true, response.Result["mutation_rejected"])
		})
	}
}
