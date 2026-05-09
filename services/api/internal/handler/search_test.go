package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// captureSearchLogs swaps slog.Default for a TextHandler-backed buffer
// for the duration of the test. Mirrors the captureLogs pattern from
// service/titler_test.go (Pitfall 8 / log-shape regression test).
func captureSearchLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
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

// searchBizCtx seeds a BusinessContext with PermContentRead for search handler tests.
func searchBizCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead},
	})
}

// newSearchHandlerForTest builds a SearchHandler with a real Searcher
// driven by stub repos. ready=true flips the readiness flag so search
// requests return 200 instead of 503.
func newSearchHandlerForTest(t *testing.T, ready bool) *SearchHandler {
	t.Helper()
	searcher := service.NewSearcher(&stubConvRepoSearchHandler{}, &stubMsgRepoSearchHandler{})
	if ready {
		searcher.MarkIndexesReady()
	}
	h, err := NewSearchHandler(searcher)
	require.NoError(t, err)
	return h
}

// requestWithBiz builds a request with a BusinessContext already injected.
func requestWithBiz(method, target string, businessID, userID uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, target, http.NoBody)
	return req.WithContext(searchBizCtx(businessID, userID))
}

// TestNewSearchHandler_NilGuards — startup-time wiring bugs surface as
// non-nil error returns from the constructor.
func TestNewSearchHandler_NilGuards(t *testing.T) {
	t.Run("nil searcher", func(t *testing.T) {
		h, err := NewSearchHandler(nil)
		assert.Error(t, err)
		assert.Nil(t, h)
	})
}

// TestSearchHandler_400OnShortQuery — q="" or q="a" must surface as 400.
func TestSearchHandler_400OnShortQuery(t *testing.T) {
	h := newSearchHandlerForTest(t, true)
	businessID := uuid.New()
	userID := uuid.New()

	t.Run("empty q", func(t *testing.T) {
		req := requestWithBiz(http.MethodGet, "/api/v1/search?q=", businessID, userID)
		rec := httptest.NewRecorder()
		h.Search(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("single char q", func(t *testing.T) {
		req := requestWithBiz(http.MethodGet, "/api/v1/search?q=a", businessID, userID)
		rec := httptest.NewRecorder()
		h.Search(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestSearchHandler_500OnMissingBusinessContext — without middleware injecting a
// BusinessContext, the handler responds 500 (middleware misconfiguration).
func TestSearchHandler_500OnMissingBusinessContext(t *testing.T) {
	h := newSearchHandlerForTest(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=инвойс", http.NoBody)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestSearchHandler_503BeforeReady — readiness flag false → 503 +
// Retry-After: 5 header.
func TestSearchHandler_503BeforeReady(t *testing.T) {
	h := newSearchHandlerForTest(t, false /* not ready */)
	businessID := uuid.New()
	userID := uuid.New()

	req := requestWithBiz(http.MethodGet, "/api/v1/search?q=инвойс", businessID, userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "5", rec.Header().Get("Retry-After"),
		"503 must carry Retry-After: 5")
}

// TestSearchHandler_HappyPath — ready + q ≥ 2 chars + valid BusinessContext →
// 200 OK with a JSON array body (possibly empty).
func TestSearchHandler_HappyPath(t *testing.T) {
	h := newSearchHandlerForTest(t, true)
	businessID := uuid.New()
	userID := uuid.New()

	req := requestWithBiz(http.MethodGet, "/api/v1/search?q=инвойс&limit=10", businessID, userID)
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
	h := newSearchHandlerForTest(t, true)
	businessID := uuid.New()
	userID := uuid.New()

	req := requestWithBiz(http.MethodGet, "/api/v1/search?q=test&project_id=proj-X", businessID, userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestSearchHandler_LogShape — log-leak regression.
// Captured logs MUST contain `query_length` and MUST NOT contain the
// literal query bytes. Asserted on the stricter forms used by slog's
// TextHandler so a future log shape that smuggles q via `error=...`
// would still be caught (the query never appears in any field).
func TestSearchHandler_LogShape(t *testing.T) {
	buf := captureSearchLogs(t)

	h := newSearchHandlerForTest(t, true)
	businessID := uuid.New()
	userID := uuid.New()
	const literalQuery = "тайныйзапрос9000"

	req := requestWithBiz(http.MethodGet, "/api/v1/search?q="+literalQuery, businessID, userID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	logs := buf.String()
	// Service-layer log line carries query_length on every search. We
	// only assert the negative (no leak) here; the service unit test
	// covers the positive presence.
	assert.NotContains(t, logs, literalQuery,
		"query text leaked into logs")
}
