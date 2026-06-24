package streamlimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/streamlimit"
)

func TestMiddleware_RejectsOverCapAndReleases(t *testing.T) {
	release := make(chan struct{})
	inHandler := make(chan struct{}, 1)
	h := streamlimit.Middleware(1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		inHandler <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	// First request acquires the single slot and blocks inside the handler.
	rec1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		h.ServeHTTP(rec1, httptest.NewRequest(http.MethodPost, "/chat/x", http.NoBody))
		close(done1)
	}()
	<-inHandler // req1 now holds the slot

	// Second request while the slot is held is rejected with 503 immediately.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/chat/x", http.NoBody))
	require.Equal(t, http.StatusServiceUnavailable, rec2.Code)
	require.Contains(t, rec2.Body.String(), "stream_capacity_exceeded")
	require.Equal(t, "1", rec2.Header().Get("Retry-After"))
	require.Equal(t, "application/json", rec2.Header().Get("Content-Type"))

	// Releasing req1 frees the slot.
	close(release)
	<-done1
	require.Equal(t, http.StatusOK, rec1.Code)

	// A subsequent request now acquires the freed slot (release is already
	// closed, so the handler no longer blocks).
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest(http.MethodPost, "/chat/x", http.NoBody))
	require.Equal(t, http.StatusOK, rec3.Code)
}

func TestMiddleware_DisabledWhenMaxNonPositive(t *testing.T) {
	for _, maxStreams := range []int{0, -1} {
		called := 0
		h := streamlimit.Middleware(maxStreams)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called++
			w.WriteHeader(http.StatusOK)
		}))
		for i := 0; i < 5; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/chat/x", http.NoBody))
			require.Equal(t, http.StatusOK, rec.Code)
		}
		require.Equal(t, 5, called, "max=%d must be an unbounded pass-through", maxStreams)
	}
}

func TestMiddleware_SharedAcrossRoutes(t *testing.T) {
	// One Middleware reused for two routes shares a single budget of 1: while
	// route A holds the slot, route B is rejected.
	mw := streamlimit.Middleware(1)
	release := make(chan struct{})
	inA := make(chan struct{}, 1)
	routeA := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		inA <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	routeB := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	doneA := make(chan struct{})
	go func() {
		routeA.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/chat/x", http.NoBody))
		close(doneA)
	}()
	<-inA

	recB := httptest.NewRecorder()
	routeB.ServeHTTP(recB, httptest.NewRequest(http.MethodPost, "/chat/x/resume", http.NoBody))
	require.Equal(t, http.StatusServiceUnavailable, recB.Code, "the two routes must share one global slot")

	close(release)
	<-doneA
}
