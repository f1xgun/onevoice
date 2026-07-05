package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
)

// fakeTokenFetcher records the last call and returns a preset token.
type fakeTokenFetcher struct {
	token        string
	externalID   string
	err          error
	lastBizID    string
	lastPlatform string
	lastExtID    string
}

func (f *fakeTokenFetcher) GetToken(_ context.Context, businessID, platform, externalID, _ string) (agent.TokenInfo, error) {
	f.lastBizID = businessID
	f.lastPlatform = platform
	f.lastExtID = externalID
	if f.err != nil {
		return agent.TokenInfo{}, f.err
	}
	resolvedExtID := externalID
	if resolvedExtID == "" {
		resolvedExtID = f.externalID
	}
	return agent.TokenInfo{AccessToken: f.token, ExternalID: resolvedExtID}, nil
}

// firstActiveTokenFetcher models the API's first-active fallback: when the
// LLM-supplied externalID matches none of the acting business's integrations,
// GetDecryptedToken silently returns that business's OWN first active
// integration (its token + its ownExternalID), NOT an echo of the unmatched
// supplied value. This is the exact condition that lets a foreign channel_id
// reach the send path, so cross-tenant tests must use this fake — not the
// echoing fakeTokenFetcher, which only models the exact-match path.
type firstActiveTokenFetcher struct {
	token         string
	ownExternalID string
}

func (f *firstActiveTokenFetcher) GetToken(_ context.Context, _, _, _, _ string) (agent.TokenInfo, error) {
	return agent.TokenInfo{AccessToken: f.token, ExternalID: f.ownExternalID}, nil
}

// fakeSender records the last message sent.
type fakeSender struct {
	sentMessage    string
	sentChat       string
	sentPhotoURL   string
	sentCaption    string
	replyCalled    bool
	replyChat      string
	replyMessageID int
	replyText      string
	reviewsLimit   int
	sentMarkup     *tgbotapi.InlineKeyboardMarkup
}

func (f *fakeSender) SendMessage(chat, text string) error {
	f.sentMessage = text
	f.sentChat = chat
	return nil
}

func (f *fakeSender) SendMessageWithMarkup(chat, text string, markup *tgbotapi.InlineKeyboardMarkup) error {
	f.sentMessage = text
	f.sentChat = chat
	f.sentMarkup = markup
	return nil
}

func (f *fakeSender) SendPhoto(chat, photoURL, caption string) error {
	f.sentChat = chat
	f.sentPhotoURL = photoURL
	f.sentCaption = caption
	return nil
}

func (f *fakeSender) SendReply(chat string, messageID int, text string) error {
	f.replyCalled = true
	f.replyChat = chat
	f.replyMessageID = messageID
	f.replyText = text
	return nil
}
func (f *fakeSender) GetReviews(limit int) ([]map[string]interface{}, error) {
	f.reviewsLimit = limit
	return []map[string]interface{}{}, nil
}

func newHandlerWithSender(fetcher agent.TokenFetcher, sender *fakeSender) *agent.Handler {
	factory := func(_ string) (agent.Sender, error) {
		return sender, nil
	}
	return agent.NewHandler(fetcher, factory, nil)
}

func TestHandler_SendChannelPost_FetchesTokenPerRequest(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token-123"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t1",
		BusinessID: "biz-42",
		Tool:       tools.TelegramSendChannelPost,
		Args: map[string]interface{}{
			"text":       "Hello, channel!",
			"channel_id": "-1001234567890",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "Hello, channel!", sender.sentMessage)
	assert.Equal(t, "-1001234567890", sender.sentChat)

	assert.Equal(t, "biz-42", fetcher.lastBizID)
	assert.Equal(t, "telegram", fetcher.lastPlatform)
	assert.Equal(t, "-1001234567890", fetcher.lastExtID)
}

// TestHandler_SendChannelPost_PublicUsername is the regression for the demo
// failure: a public channel is connected as @channelusername, the LLM passes it
// as channel_id, and the agent rejected it with strconv.ParseInt
// (invalid channel_id "@onevoice_test"). Telegram accepts @username as chat_id;
// the agent must forward it untouched.
func TestHandler_SendChannelPost_PublicUsername(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token", externalID: "@onevoice_test"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramSendChannelPost,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "channel_id": "@onevoice_test"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "@onevoice_test", sender.sentChat)
}

// TestHandler_SendChannelPost_EmptyChannelID_FallsBackToResolved proves the
// resolution fallback still holds for the @username form: when the LLM omits
// channel_id, the integration's resolved external_id (here a public @username)
// is used as the chat target.
func TestHandler_SendChannelPost_EmptyChannelID_FallsBackToResolved(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token", externalID: "@onevoice_test"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramSendChannelPost,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "channel_id": ""},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "@onevoice_test", sender.sentChat)
}

// TestHandler_SendChannelPost_ForeignChannelID_ScopedToOwnChannel is the
// cross-tenant regression: one shared OneVoice system bot administers EVERY
// tenant's connected channel. Business A owns integration X; the LLM is steered
// (by hallucination or prompt injection via untrusted review text) to pass a
// channel_id naming a DIFFERENT channel A does not own. The token resolver's
// first-active fallback still returns A's token + resolved=X, so the send target
// must be X (A's own channel), never the foreign supplied value — otherwise the
// admin bot would post A's content onto the victim's channel. Reverting the
// resolveChatTarget ownership check sends to the foreign channel and fails this.
func TestHandler_SendChannelPost_ForeignChannelID_ScopedToOwnChannel(t *testing.T) {
	foreignTargets := []string{"@victim_channel", "-1009999999999"}
	for _, foreign := range foreignTargets {
		t.Run(foreign, func(t *testing.T) {
			fetcher := &firstActiveTokenFetcher{token: "biz-A-token", ownExternalID: "-1001111111111"}
			sender := &fakeSender{}
			h := newHandlerWithSender(fetcher, sender)

			resp, err := h.Handle(context.Background(), a2a.ToolRequest{
				Tool:       tools.TelegramSendChannelPost,
				BusinessID: "biz-A",
				Args:       map[string]interface{}{"text": "A's content", "channel_id": foreign},
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.True(t, resp.Success)
			assert.Equal(t, "-1001111111111", sender.sentChat,
				"send must target the acting business's OWN resolved channel, never an unowned supplied channel_id")
			assert.NotEqual(t, foreign, sender.sentChat,
				"the shared system bot must never post to a channel the acting business does not own")
		})
	}
}

// TestHandler_SendChannelPost_OwnChannelID_HonorsExactMatch is the companion
// regression: when the supplied channel_id exactly equals one of the acting
// business's own integration external_ids (the token resolver returns
// resolved == supplied), that exact id must be honored so legitimate
// multi-channel targeting by external_id keeps working.
func TestHandler_SendChannelPost_OwnChannelID_HonorsExactMatch(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "biz-A-token", externalID: "@onevoice_test"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramSendChannelPost,
		BusinessID: "biz-A",
		Args:       map[string]interface{}{"text": "hi", "channel_id": "@onevoice_test"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "@onevoice_test", sender.sentChat,
		"an exact match on the business's own external_id must be honored verbatim")
}

// TestHandler_SendChannelPhoto_ForeignChannelID_ScopedToOwnChannel proves the
// same cross-tenant scoping holds for the photo send path.
func TestHandler_SendChannelPhoto_ForeignChannelID_ScopedToOwnChannel(t *testing.T) {
	fetcher := &firstActiveTokenFetcher{token: "biz-A-token", ownExternalID: "-1001111111111"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramSendChannelPhoto,
		BusinessID: "biz-A",
		Args:       map[string]interface{}{"photo_url": "http://x/p.jpg", "caption": "c", "channel_id": "@victim_channel"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "-1001111111111", sender.sentChat,
		"photo send must target the acting business's OWN resolved channel, never an unowned supplied channel_id")
}

// TestHandler_ReplyToComment_ForeignChatID_ScopedToOwnChannel proves the reply
// path is likewise scoped: the LLM supplies a foreign chat_id (a channel A does
// not own) while getSender resolves A's own channel; the reply must land on A's
// own channel, not the foreign chat.
func TestHandler_ReplyToComment_ForeignChatID_ScopedToOwnChannel(t *testing.T) {
	fetcher := &firstActiveTokenFetcher{token: "biz-A-token", ownExternalID: "-1001111111111"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramReplyToComment,
		BusinessID: "biz-A",
		Args: map[string]interface{}{
			"text":       "reply",
			"chat_id":    "@victim_channel",
			"channel_id": "@victim_channel",
			"message_id": float64(7),
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.True(t, sender.replyCalled)
	assert.Equal(t, "-1001111111111", sender.replyChat,
		"reply must target the acting business's OWN resolved channel, never an unowned supplied chat_id")
}

func TestHandler_SendNotification_FetchesTokenPerRequest(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token-456"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t2",
		BusinessID: "biz-99",
		Tool:       tools.TelegramSendNotification,
		Args: map[string]interface{}{
			"text":    "You have a new review!",
			"chat_id": "123456789",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "You have a new review!", sender.sentMessage)
	assert.Equal(t, "123456789", sender.sentChat)

	assert.Equal(t, "biz-99", fetcher.lastBizID)
	assert.Equal(t, "telegram", fetcher.lastPlatform)
}

// TestHandler_SendNotification_NoApprovalBatch_NoMarkup proves the inline-button
// support is strictly additive: a plain notification (no approval_batch_id arg)
// is sent with a nil markup, exactly like the pre-existing plain send. No
// regression to the ordinary notification path.
func TestHandler_SendNotification_NoApprovalBatch_NoMarkup(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token"}
	sender := &fakeSender{}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) { return sender, nil }, nil,
		agent.WithApprovalHMACSecret("secret-key"))

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		BusinessID: "biz-1",
		Tool:       tools.TelegramSendNotification,
		Args:       map[string]interface{}{"text": "hi", "chat_id": "123456789"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Nil(t, sender.sentMarkup, "plain notification must carry no inline keyboard")
}

// TestHandler_SendNotification_WithApprovalBatch_AttachesButtons proves the
// [Approve]/[Reject] inline keyboard is attached when an approval_batch_id arg
// is present and the HMAC secret is configured. Each button carries a distinct,
// server-verifiable callback_data (approve vs reject differ) bound to the batch.
func TestHandler_SendNotification_WithApprovalBatch_AttachesButtons(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token"}
	sender := &fakeSender{}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) { return sender, nil }, nil,
		agent.WithApprovalHMACSecret("secret-key"))

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		BusinessID: "biz-1",
		Tool:       tools.TelegramSendNotification,
		Args: map[string]interface{}{
			"text":              "Approve this action?",
			"chat_id":           "123456789",
			"approval_batch_id": "550e8400-e29b-41d4-a716-446655440000",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	require.NotNil(t, sender.sentMarkup, "approval notification must carry the inline keyboard")
	require.Len(t, sender.sentMarkup.InlineKeyboard, 1)
	require.Len(t, sender.sentMarkup.InlineKeyboard[0], 2, "expected [Approve][Reject] buttons")
	approveBtn := sender.sentMarkup.InlineKeyboard[0][0]
	rejectBtn := sender.sentMarkup.InlineKeyboard[0][1]
	require.NotNil(t, approveBtn.CallbackData)
	require.NotNil(t, rejectBtn.CallbackData)
	assert.NotEqual(t, *approveBtn.CallbackData, *rejectBtn.CallbackData, "approve and reject callback_data must differ")
	assert.LessOrEqual(t, len(*approveBtn.CallbackData), 64, "callback_data must fit Telegram's 64-byte cap")
}

// TestHandler_SendNotification_ApprovalBatch_NoSecret_NoMarkup proves the
// fail-closed behavior: when the HMAC secret is unset, an approval_batch_id arg
// does NOT produce buttons (the notification still sends), so no unsigned
// approval surface is ever exposed.
func TestHandler_SendNotification_ApprovalBatch_NoSecret_NoMarkup(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender) // no WithApprovalHMACSecret

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		BusinessID: "biz-1",
		Tool:       tools.TelegramSendNotification,
		Args: map[string]interface{}{
			"text":              "Approve this action?",
			"chat_id":           "123456789",
			"approval_batch_id": "550e8400-e29b-41d4-a716-446655440000",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Nil(t, sender.sentMarkup, "no HMAC secret must fail closed to a plain notification, never an unsigned button")
}

// TestHandler_SendNotification_NoChatID_DoesNotLeakToChannel is the regression
// for the confidentiality leak: telegram__send_notification is advertised to the
// LLM as a private owner notification and its tool spec exposes no chat_id, so in
// production Args carries only {text}. The integration's resolved external_id is
// the connected PUBLIC channel. The notification path must NOT fall back to that
// external_id (which would broadcast owner-private content — and any quoted
// third-party PII — to all channel subscribers); it must fail closed with a
// coded, non-retryable notification_recipient_unconfigured error.
func TestHandler_SendNotification_NoChatID_DoesNotLeakToChannel(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "bot-token", externalID: "@publicchannel"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		BusinessID: "biz-1",
		Tool:       tools.TelegramSendNotification,
		Args:       map[string]interface{}{"text": "private owner alert"},
	})

	require.Error(t, err)
	assert.Equal(t, "notification_recipient_unconfigured", a2a.CodeOf(err))
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}),
		"missing private recipient is permanent until configured — must be NonRetryable")
	assert.Empty(t, sender.sentChat, "notification must NOT be sent anywhere when no private recipient is configured")
	assert.NotEqual(t, "@publicchannel", sender.sentChat,
		"owner-private notification must never be broadcast to the connected public channel")
}

func TestHandler_TokenFetchError_ReturnsError(t *testing.T) {
	fetcher := &fakeTokenFetcher{err: fmt.Errorf("integration not found")}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t3",
		Tool:   tools.TelegramSendChannelPost,
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
	}, nil)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t4",
		Tool:   "telegram__unknown_tool",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// TestHandler_ReplyToComment_MissingMessageID_Rejected proves a missing or
// non-numeric message_id fails fast with a non-retryable error instead of
// silently replying to message 0. Mirrors the VK agent's guard.
func TestHandler_ReplyToComment_MissingMessageID_Rejected(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{name: "missing", args: map[string]interface{}{"text": "hi", "channel_id": "-100123"}},
		{name: "zero", args: map[string]interface{}{"text": "hi", "channel_id": "-100123", "message_id": float64(0)}},
		{name: "negative", args: map[string]interface{}{"text": "hi", "channel_id": "-100123", "message_id": float64(-5)}},
		{name: "non-numeric string", args: map[string]interface{}{"text": "hi", "channel_id": "-100123", "message_id": "abc"}},
		{name: "wrong type", args: map[string]interface{}{"text": "hi", "channel_id": "-100123", "message_id": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &fakeTokenFetcher{token: "tok"}
			sender := &fakeSender{}
			h := newHandlerWithSender(fetcher, sender)

			_, err := h.Handle(context.Background(), a2a.ToolRequest{
				Tool:       tools.TelegramReplyToComment,
				BusinessID: "biz-1",
				Args:       tc.args,
			})

			require.Error(t, err)
			assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "invalid message_id must be non-retryable")
			assert.False(t, sender.replyCalled, "SendReply must NOT be called with an invalid message_id")
		})
	}
}

func TestHandler_ReplyToComment_ValidMessageID_Replies(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "-1009999"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramReplyToComment,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "thanks", "channel_id": "-1009999", "message_id": float64(42)},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.True(t, sender.replyCalled)
	assert.Equal(t, 42, sender.replyMessageID)
	assert.Equal(t, "thanks", sender.replyText)
}

// TestHandler_GetReviews_HugeLimit_ClampedToMax is the resource-amplification
// regression: the limit is an LLM-supplied arg (reachable via prompt injection
// through untrusted review text or a jailbreak), and GetReviews pages
// getUpdates until it has collected `limit` entries and allocates a slice sized
// to the accumulated window. An unbounded huge limit would drain the entire
// pending-update window across many API pages and allocate a large slice. The
// handler must clamp the limit to TelegramReviewLimitMax before it reaches the
// fetch, matching the VK and Yandex agents' upper bounds.
func TestHandler_GetReviews_HugeLimit_ClampedToMax(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "-1009999"}
	sender := &fakeSender{}
	h := newHandlerWithSender(fetcher, sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramGetReviews,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"limit": float64(999999)},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, domain.TelegramReviewLimitMax, sender.reviewsLimit,
		"a huge LLM-supplied limit must be clamped to TelegramReviewLimitMax before reaching the fetch")
}

// errSender is a Sender that always returns a configured error.
type errSender struct {
	err error
}

func (e *errSender) SendMessage(_, _ string) error             { return e.err }
func (e *errSender) SendPhoto(_, _, _ string) error            { return e.err }
func (e *errSender) SendReply(_ string, _ int, _ string) error { return e.err }
func (e *errSender) GetReviews(_ int) ([]map[string]interface{}, error) {
	return nil, e.err
}

func (e *errSender) SendMessageWithMarkup(_, _ string, _ *tgbotapi.InlineKeyboardMarkup) error {
	return e.err
}

func newHandlerWithErrSender(fetcher agent.TokenFetcher, sendErr error) *agent.Handler {
	factory := func(_ string) (agent.Sender, error) {
		return &errSender{err: sendErr}, nil
	}
	return agent.NewHandler(fetcher, factory, nil)
}

func sendPostReq() a2a.ToolRequest {
	return a2a.ToolRequest{
		Tool:       tools.TelegramSendChannelPost,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "channel_id": "123"},
	}
}

func TestClassifyTelegramError_Unauthorized(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "123"}
	h := newHandlerWithErrSender(fetcher, fmt.Errorf("Unauthorized"))

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "Unauthorized should be NonRetryableError")
}

func TestClassifyTelegramError_Forbidden(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "123"}
	h := newHandlerWithErrSender(fetcher, fmt.Errorf("Forbidden: bot was blocked by the user"))

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "Forbidden should be NonRetryableError")
}

func TestClassifyTelegramError_RateLimit(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "123"}
	h := newHandlerWithErrSender(fetcher, fmt.Errorf("Too Many Requests: retry after 30"))

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "rate limit should be NonRetryableError")
}

func TestClassifyTelegramError_NetworkError(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "123"}
	h := newHandlerWithErrSender(fetcher, fmt.Errorf("dial tcp: connection refused"))

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.False(t, errors.Is(err, &a2a.NonRetryableError{}), "network error should NOT be NonRetryableError")
}

func TestClassifyTelegramError_Unauthorized_StampsTokenInvalid(t *testing.T) {
	out := agent.ClassifyTelegramError(fmt.Errorf("Unauthorized"))
	assert.Equal(t, "integration_token_invalid", a2a.CodeOf(out))
	assert.True(t, errors.Is(out, &a2a.NonRetryableError{}))
}

func TestClassifyTelegramError_Forbidden_StampsTokenInvalid(t *testing.T) {
	out := agent.ClassifyTelegramError(fmt.Errorf("Forbidden: bot was kicked"))
	assert.Equal(t, "integration_token_invalid", a2a.CodeOf(out))
}

func TestClassifyTelegramError_TooManyRequests_StampsRateLimit(t *testing.T) {
	out := agent.ClassifyTelegramError(fmt.Errorf("Too Many Requests: retry after 30"))
	assert.Equal(t, "rate_limit_exceeded", a2a.CodeOf(out))
	assert.True(t, errors.Is(out, &a2a.NonRetryableError{}))
}

func TestClassifyTelegramError_PhotoDimensions_StampsMediaTooLarge(t *testing.T) {
	out := agent.ClassifyTelegramError(fmt.Errorf("Bad Request: PHOTO_INVALID_DIMENSIONS"))
	assert.Equal(t, "media_too_large", a2a.CodeOf(out))
	assert.True(t, errors.Is(out, &a2a.NonRetryableError{}))
}

func TestClassifyTelegramError_ChatNotFound_StampsChannelNotFound(t *testing.T) {
	out := agent.ClassifyTelegramError(fmt.Errorf("Bad Request: chat not found"))
	assert.Equal(t, "channel_not_found", a2a.CodeOf(out))
	assert.True(t, errors.Is(out, &a2a.NonRetryableError{}))
}

func TestClassifyTelegramError_Generic_StampsTransient(t *testing.T) {
	out := agent.ClassifyTelegramError(fmt.Errorf("dial tcp: connection refused"))
	assert.Equal(t, "transient", a2a.CodeOf(out))
	assert.False(t, errors.Is(out, &a2a.NonRetryableError{}))
}

func TestClassifyTelegramError_Nil_ReturnsNil(t *testing.T) {
	assert.NoError(t, agent.ClassifyTelegramError(nil))
}

func TestTelegramSendChannelPost_InvalidChannelID_StampsChannelNotFound(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok", externalID: "not-a-number"}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) {
		return &fakeSender{}, nil
	}, nil)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       tools.TelegramSendChannelPost,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "channel_id": "also-not-numeric"},
	})
	require.Error(t, err)
	assert.Equal(t, "channel_not_found", a2a.CodeOf(err))
}

func TestClassifyTelegramError_TokenFetchFailure(t *testing.T) {
	fetcher := &fakeTokenFetcher{err: fmt.Errorf("integration not found")}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) {
		return &fakeSender{}, nil
	}, nil)

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "token fetch failure should be NonRetryableError")
}

func TestClassifyTelegramError_IntegrationNotFound_StampsTokenInvalid(t *testing.T) {
	fetcher := &fakeTokenFetcher{err: tokenclient.ErrIntegrationNotFound}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) {
		return &fakeSender{}, nil
	}, nil)

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.Equal(t, "integration_token_invalid", a2a.CodeOf(err),
		"a deleted integration must reach the FE as integration_token_invalid (reconnect CTA), not transient auto-retry")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}),
		"a deleted integration is permanent until reconnect — must stay NonRetryable")
}

func TestClassifyTelegramError_TokenExpired_StampsTokenInvalid(t *testing.T) {
	fetcher := &fakeTokenFetcher{err: tokenclient.ErrTokenExpired}
	h := agent.NewHandler(fetcher, func(_ string) (agent.Sender, error) {
		return &fakeSender{}, nil
	}, nil)

	_, err := h.Handle(context.Background(), sendPostReq())
	require.Error(t, err)
	assert.Equal(t, "integration_token_invalid", a2a.CodeOf(err),
		"an expired token whose refresh failed must reach the FE as integration_token_invalid (reconnect CTA), not transient")
}

// --- Redis dedupe gate tests ---

// countingSender wraps fakeSender with an atomic call counter so
// second-call-returns-cached tests can prove the tool was NOT re-invoked.
type countingSender struct {
	fakeSender
	sendCalls int64
}

func (c *countingSender) SendMessage(chat, text string) error {
	atomic.AddInt64(&c.sendCalls, 1)
	return c.fakeSender.SendMessage(chat, text)
}

func newDedupeTestHandler(t *testing.T, sender agent.Sender) (*agent.Handler, *miniredis.Miniredis, agent.TokenFetcher) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	dedupe := hitldedupe.New(rdb)
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agent.ClassifyTelegramError))
	fetcher := &fakeTokenFetcher{token: "bot-token", externalID: "-1001234567890"}
	factory := func(_ string) (agent.Sender, error) { return sender, nil }
	return agent.NewHandler(fetcher, factory, dispatcher), mr, fetcher
}

func sendPostReqWithApproval(approvalID string) a2a.ToolRequest {
	return a2a.ToolRequest{
		TaskID:     "task-t",
		Tool:       tools.TelegramSendChannelPost,
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "hi", "channel_id": "-1001234567890"},
		ApprovalID: approvalID,
	}
}

func TestHandler_Handle_EmptyApprovalID_SkipsDedupe(t *testing.T) {
	sender := &countingSender{}
	h, mr, _ := newDedupeTestHandler(t, sender)

	resp, err := h.Handle(context.Background(), sendPostReqWithApproval(""))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), atomic.LoadInt64(&sender.sendCalls))
	assert.Equal(t, 0, len(mr.Keys()),
		"empty ApprovalID must NOT touch Redis (anti-footgun #2)")
}

func TestHandler_Handle_FirstCallWithApprovalID_ExecutesAndCaches(t *testing.T) {
	sender := &countingSender{}
	h, mr, _ := newDedupeTestHandler(t, sender)

	resp, err := h.Handle(context.Background(), sendPostReqWithApproval("appr-tg-1"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), atomic.LoadInt64(&sender.sendCalls))

	key := "hitl:approval:biz-1:appr-tg-1"
	require.True(t, mr.Exists(key), "dedupe key must be stored after successful execution")
	val, err := mr.Get(key)
	require.NoError(t, err)
	var cached a2a.ToolResponse
	require.NoError(t, json.Unmarshal([]byte(val), &cached))
	assert.True(t, cached.Success)
}

func TestHandler_Handle_SecondCallWithSameApprovalID_ReturnsCached(t *testing.T) {
	sender := &countingSender{}
	h, _, _ := newDedupeTestHandler(t, sender)

	resp1, err := h.Handle(context.Background(), sendPostReqWithApproval("appr-tg-2"))
	require.NoError(t, err)
	require.NotNil(t, resp1)

	resp2, err := h.Handle(context.Background(), sendPostReqWithApproval("appr-tg-2"))
	require.NoError(t, err)
	require.NotNil(t, resp2)

	assert.Equal(t, int64(1), atomic.LoadInt64(&sender.sendCalls),
		"tool must be invoked exactly once across two Handle calls with the same ApprovalID")
	assert.Equal(t, resp1.Success, resp2.Success, "second call must return the cached response")
	assert.Equal(t, resp1.Result["status"], resp2.Result["status"])
}

func TestHandler_Handle_ApprovalID_InFlight_ReturnsDuplicateError(t *testing.T) {
	sender := &countingSender{}
	h, mr, _ := newDedupeTestHandler(t, sender)

	key := "hitl:approval:biz-1:appr-tg-3"
	require.NoError(t, mr.Set(key, "executing"))
	mr.SetTTL(key, 24*60*60*1e9)

	resp, err := h.Handle(context.Background(), sendPostReqWithApproval("appr-tg-3"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "duplicate: already in flight")
	assert.Equal(t, int64(0), atomic.LoadInt64(&sender.sendCalls),
		"in-flight claim must short-circuit before tool dispatch")
}
