package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
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

// fakeSender records the last message sent.
type fakeSender struct {
	sentMessage  string
	sentChat     string
	sentPhotoURL string
	sentCaption  string
}

func (f *fakeSender) SendMessage(chat, text string) error {
	f.sentMessage = text
	f.sentChat = chat
	return nil
}

func (f *fakeSender) SendPhoto(chat, photoURL, caption string) error {
	f.sentChat = chat
	f.sentPhotoURL = photoURL
	f.sentCaption = caption
	return nil
}

func (f *fakeSender) SendReply(_ string, _ int, _ string) error { return nil }
func (f *fakeSender) GetReviews(_ int) ([]map[string]interface{}, error) {
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
