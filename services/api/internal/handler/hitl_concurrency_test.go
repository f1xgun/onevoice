package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
)

// resumeWithCounter drives POST /chat/{id}/resume for the given user, mirroring
// the other resume tests' chi wiring but threading a concrete userID into the
// business context so the per-user SSE cap can be exercised.
func resumeWithCounter(h *handler.HITLHandler, bizID, userID uuid.UUID, convID, batchID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID+"/resume?batch_id="+batchID, http.NoBody)
	req = req.WithContext(hitlBizCtx(bizID, userID))
	r := chi.NewRouter()
	r.Post("/api/v1/chat/{id}/resume", h.Resume)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestResume_SSEConcurrencyCap_Rejects_WhenUserAtCap proves Resume routes its
// orchestrator SSE stream through the same per-user concurrency cap as
// ChatProxyHandler.Chat. The user is pinned at the cap before the request, so
// Acquire must fail and the handler must return 429 without ever forwarding to
// the orchestrator.
//
// Fail-on-revert: deleting the Acquire/release block in HITLHandler.Resume lets
// the over-cap request fall straight through to the orchestrator (200 + SSE),
// so this assertion flips to 200 and the test fails.
func TestResume_SSEConcurrencyCap_Rejects_WhenUserAtCap(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
	})

	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		orchHits++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orch.Close()

	h := buildHITLHandler(t, pr, biz, nil, orch.URL)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.SetSSECounter(ssecounter.New(rdb, 1, ratelimit.Policy{}), "free")

	userID := uuid.New()
	if err := mr.Set("sse:user:"+userID.String()+":active", "1"); err != nil {
		t.Fatalf("seed redis cap key: %v", err)
	}

	rec := resumeWithCounter(h, biz.ID, userID, "c1", "b1")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (user already at SSE cap); body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q, want \"1\"", rec.Header().Get("Retry-After"))
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if decoded["code"] != "sse_concurrency_exceeded" {
		t.Errorf("code = %v, want sse_concurrency_exceeded", decoded["code"])
	}
	if orchHits != 0 {
		t.Errorf("orchestrator hits = %d, want 0 (must reject before streaming)", orchHits)
	}
}

// TestResume_SSEConcurrencyCap_Allows_WhenBelowCap is the companion case: with a
// free slot, Resume acquires it and forwards to the orchestrator as usual, so the
// cap gates only over-budget callers rather than blocking the path outright.
func TestResume_SSEConcurrencyCap_Allows_WhenBelowCap(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
	})

	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		orchHits++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orch.Close()

	h := buildHITLHandler(t, pr, biz, nil, orch.URL)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.SetSSECounter(ssecounter.New(rdb, 1, ratelimit.Policy{}), "free")

	rec := resumeWithCounter(h, biz.ID, uuid.New(), "c1", "b1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (slot available); body=%q", rec.Code, rec.Body.String())
	}
	if orchHits != 1 {
		t.Errorf("orchestrator hits = %d, want 1 (must forward when below cap)", orchHits)
	}
}
