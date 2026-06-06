package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
)

// newConcurrencyHarness mirrors newChatProxyNoProject but additionally
// attaches a configured ssecounter so the concurrency cap fires. holdCh
// gates the upstream orchestrator response so the caller can pin
// in-flight streams open while extra POSTs race to hit the cap.
func newConcurrencyHarness(
	t *testing.T,
	maxActive int,
	holdCh chan struct{},
) (h *ChatProxyHandler, businessID, userID uuid.UUID, mr *miniredis.Miniredis) {
	t.Helper()
	userID = uuid.New()
	businessID = uuid.New()

	business := &domain.Business{ID: businessID, Name: "Biz"}

	orchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"text\",\"content\":\"hi\"}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if holdCh != nil {
			select {
			case <-holdCh:
			case <-r.Context().Done():
			}
		}
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	t.Cleanup(orchServer.Close)

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByID", mock.Anything, businessID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	h = newChatProxyNoProject(mockBiz, mockInteg, &MockMessageRepository{}, orchServer.URL)

	mr = miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	c := ssecounter.New(rdb, maxActive, ratelimit.Policy{})
	h.SetSSECounter(c, "free")
	return h, businessID, userID, mr
}

func sendChat(h *ChatProxyHandler, businessID, userID uuid.UUID, convID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := chatProxyBizCtx(businessID, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.Chat(rr, req)
	return rr
}

func TestChatProxy_SSEConcurrencyCap_Rejects_BeforeSSEHeaders(t *testing.T) {
	holdCh := make(chan struct{})
	defer close(holdCh)
	h, bizID, userID, mr := newConcurrencyHarness(t, 3, holdCh)

	for i := 0; i < 3; i++ {
		go func() {
			_ = sendChat(h, bizID, userID, "conv-"+uuid.NewString())
		}()
	}

	key := "sse:user:" + userID.String() + ":active"
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := mr.Get(key)
		if err == nil && got == "3" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := mr.Get(key)
	require.NoError(t, err)
	require.Equal(t, "3", got, "all 3 background goroutines must claim a slot before the 4th call")

	rr := sendChat(h, bizID, userID, "conv-rejected")
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Equal(t, "1", rr.Header().Get("Retry-After"))

	body, _ := io.ReadAll(rr.Body)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	assert.Equal(t, "sse_concurrency_exceeded", decoded["code"])
	assert.EqualValues(t, 1, decoded["retry_after_s"])
}

func TestChatProxy_SSEConcurrencyCap_ReleasesOnDefer(t *testing.T) {
	h, bizID, userID, _ := newConcurrencyHarness(t, 1, nil)

	rr1 := sendChat(h, bizID, userID, "conv-1")
	assert.Equal(t, http.StatusOK, rr1.Code)

	rr2 := sendChat(h, bizID, userID, "conv-2")
	assert.Equal(t, http.StatusOK, rr2.Code)
}

func TestChatProxy_SSEConcurrencyCap_RedisDownBlock_Returns503RateLimitUnavailable(t *testing.T) {
	h, bizID, userID, mr := newConcurrencyHarness(t, 3, nil)
	mr.Close()

	rr := sendChat(h, bizID, userID, "conv-redisdown")
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	body, _ := io.ReadAll(rr.Body)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	assert.Equal(t, "rate_limit_unavailable", decoded["code"])
}

func TestChatProxy_SSEConcurrencyCap_DisabledWhenMaxZero(t *testing.T) {
	h, bizID, userID, _ := newConcurrencyHarness(t, 0, nil)

	for i := 0; i < 5; i++ {
		rr := sendChat(h, bizID, userID, "conv-"+uuid.NewString())
		assert.Equal(t, http.StatusOK, rr.Code, "call %d", i+1)
	}
}

func TestChatProxy_SSEConcurrencyCap_NilCounter_NoOp(t *testing.T) {
	h, bizID, userID, _ := newConcurrencyHarness(t, 0, nil)
	h.sseCounter = nil

	rr := sendChat(h, bizID, userID, "conv-nocounter")
	assert.Equal(t, http.StatusOK, rr.Code)
}
