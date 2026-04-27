package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// captureSearchLogs swaps slog.Default for a TextHandler-backed buffer
// for the duration of the test. Mirrors the captureLogs pattern from
// service/titler_test.go (Phase 18 Pitfall 8 / SEARCH-07 log-shape
// regression test).
func captureSearchLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// fakeSearchBizLookup implements searchBusinessLookup for handler tests.
// Lookup returns a domain.Business with a stable ID + the configured
// error (or nil).
type fakeSearchBizLookup struct {
	biz *domain.Business
	err error
}

func (f *fakeSearchBizLookup) GetByUserID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.biz, nil
}

// stubConvRepoSearchHandler — minimal nil-embedded fake for the handler
// test. Returns empty results so the search succeeds end-to-end.
type stubConvRepoSearchHandler struct{ domain.ConversationRepository }

func (s *stubConvRepoSearchHandler) SearchTitles(_ context.Context, _, _, _ string, _ *string, _ int) ([]domain.ConversationTitleHit, []string, error) {
	return nil, nil, nil
}
func (s *stubConvRepoSearchHandler) ScopedConversationIDs(_ context.Context, _, _ string, _ *string) ([]string, error) {
	return nil, nil
}

type stubMsgRepoSearchHandler struct{ domain.MessageRepository }

func (s *stubMsgRepoSearchHandler) SearchByConversationIDs(_ context.Context, _ string, _ []string, _ int) ([]domain.MessageSearchHit, error) {
	return nil, nil
}

// newSearchHandlerForTest builds a SearchHandler with a real Searcher
// driven by stub repos. ready=true flips the readiness flag so search
// requests return 200 instead of 503.
func newSearchHandlerForTest(t *testing.T, ready bool) (*SearchHandler, *fakeSearchBizLookup) {
	t.Helper()
	bizID := uuid.New()
	biz := &domain.Business{ID: bizID, Name: "Test"}
	lookup := &fakeSearchBizLookup{biz: biz}
	searcher := service.NewSearcher(&stubConvRepoSearchHandler{}, &stubMsgRepoSearchHandler{})
	if ready {
		searcher.MarkIndexesReady()
	}
	h, err := NewSearchHandler(searcher, lookup)
	require.NoError(t, err)
	return h, lookup
}

// requestWithUser injects a userID into the chi-style auth context the
// real handler reads via middleware.GetUserID.
func requestWithUser(method, target string, userID uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	return req.WithContext(ctx)
}

// TestNewSearchHandler_NilGuards — startup-time wiring bugs surface as
// non-nil error returns from the constructor.
func TestNewSearchHandler_NilGuards(t *testing.T) {
	t.Run("nil searcher", func(t *testing.T) {
		h, err := NewSearchHandler(nil, &fakeSearchBizLookup{})
		assert.Error(t, err)
		assert.Nil(t, h)
	})
	t.Run("nil businessLookup", func(t *testing.T) {
		searcher := service.NewSearcher(&stubConvRepoSearchHandler{}, &stubMsgRepoSearchHandler{})
		h, err := NewSearchHandler(searcher, nil)
		assert.Error(t, err)
		assert.Nil(t, h)
	})
}

// TestSearchHandler_400OnShortQuery — q="" or q="a" must surface as 400.
func TestSearchHandler_400OnShortQuery(t *testing.T) {
	h, _ := newSearchHandlerForTest(t, true)
	userID := uuid.New()

	t.Run("empty q", func(t *testing.T) {
		req := requestWithUser(http.MethodGet, "/api/v1/search?q=", userID)
		rec := httptest.NewRecorder()
		h.Search(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("single char q", func(t *testing.T) {
		req := requestWithUser(http.MethodGet, "/api/v1/search?q=a", userID)
		rec := httptest.NewRecorder()
		h.Search(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestSearchHandler_401OnMissingBearer — without middleware injecting a
// userID, GetUserID returns an error and the handler responds 401.
func TestSearchHandler_401OnMissingBearer(t *testing.T) {
	h, _ := newSearchHandlerForTest(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=инвойс", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestSearchHandler_503BeforeReady — readiness flag false → 503 +
// Retry-After: 5 header. T-19-INDEX-503 mitigation.
func TestSearchHandler_503BeforeReady(t *testing.T) {
	h, _ := newSearchHandlerForTest(t, false /* not ready */)
	userID := uuid.New()

	req := requestWithUser(http.MethodGet, "/api/v1/search?q=инвойс", userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "5", rec.Header().Get("Retry-After"),
		"503 must carry Retry-After: 5 (SEARCH-06)")
}

// TestSearchHandler_HappyPath — ready + q ≥ 2 chars + valid bearer →
// 200 OK with a JSON array body (possibly empty).
func TestSearchHandler_HappyPath(t *testing.T) {
	h, _ := newSearchHandlerForTest(t, true)
	userID := uuid.New()

	req := requestWithUser(http.MethodGet, "/api/v1/search?q=инвойс&limit=10", userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var results []service.SearchResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&results))
	assert.NotNil(t, results, "response must be a JSON array, never null")
}

// TestSearchHandler_ProjectIDQuery — handler extracts project_id query
// param and passes it through. No assertion on the searcher's behavior;
// asserts only the 200 path with the param present.
func TestSearchHandler_ProjectIDQuery(t *testing.T) {
	h, _ := newSearchHandlerForTest(t, true)
	userID := uuid.New()

	req := requestWithUser(http.MethodGet, "/api/v1/search?q=test&project_id=proj-X", userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestSearchHandler_LogShape — SEARCH-07 / T-19-LOG-LEAK regression.
// Captured logs MUST contain `query_length` and MUST NOT contain the
// literal query bytes. Asserted on the stricter forms used by slog's
// TextHandler so a future log shape that smuggles q via `error=...`
// would still be caught (the query never appears in any field).
func TestSearchHandler_LogShape(t *testing.T) {
	buf := captureSearchLogs(t)

	h, _ := newSearchHandlerForTest(t, true)
	userID := uuid.New()
	const literalQuery = "тайныйзапрос9000"

	req := requestWithUser(http.MethodGet, "/api/v1/search?q="+literalQuery, userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	logs := buf.String()
	// Service-layer log line carries query_length on every search. We
	// only assert the negative (no leak) here; the service unit test
	// covers the positive presence.
	assert.NotContains(t, logs, literalQuery,
		"query text leaked into logs (T-19-LOG-LEAK)")
}

// TestSearchHandler_BusinessNotFound_401 — domain.ErrBusinessNotFound
// from the lookup surfaces as 401 «no business» (the user is
// authenticated but has no business — they cannot search).
func TestSearchHandler_BusinessNotFound_401(t *testing.T) {
	searcher := service.NewSearcher(&stubConvRepoSearchHandler{}, &stubMsgRepoSearchHandler{})
	searcher.MarkIndexesReady()
	lookup := &fakeSearchBizLookup{err: domain.ErrBusinessNotFound}
	h, err := NewSearchHandler(searcher, lookup)
	require.NoError(t, err)

	userID := uuid.New()
	req := requestWithUser(http.MethodGet, "/api/v1/search?q=инвойс", userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestSearchHandler_BusinessLookupError_500 — generic lookup error →
// 500. Log line metadata-only.
func TestSearchHandler_BusinessLookupError_500(t *testing.T) {
	buf := captureSearchLogs(t)
	searcher := service.NewSearcher(&stubConvRepoSearchHandler{}, &stubMsgRepoSearchHandler{})
	searcher.MarkIndexesReady()
	lookup := &fakeSearchBizLookup{err: errors.New("postgres down")}
	h, err := NewSearchHandler(searcher, lookup)
	require.NoError(t, err)

	userID := uuid.New()
	const literalQuery = "тайныйзапрос9001"
	req := requestWithUser(http.MethodGet, "/api/v1/search?q="+literalQuery, userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, buf.String(), literalQuery, "query leaked into error log")
}
