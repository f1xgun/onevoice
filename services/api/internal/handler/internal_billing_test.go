package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
)

// mockBilling records calls to LogUsage and returns the canned err for the
// next call (errs is FIFO; once empty all subsequent calls return nil).
type mockBilling struct {
	mu    sync.Mutex
	Calls []llm.UsageLog
	errs  []error
}

func (m *mockBilling) LogUsage(_ context.Context, log *llm.UsageLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, *log)
	if len(m.errs) == 0 {
		return nil
	}
	err := m.errs[0]
	m.errs = m.errs[1:]
	return err
}

func (m *mockBilling) failNext(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs = append(m.errs, err)
}

func (m *mockBilling) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

// validUsageLogJSON returns a populated UsageLog with business_id + model set.
func validUsageLogJSON(t *testing.T, log llm.UsageLog) []byte {
	t.Helper()
	if log.BusinessID == uuid.Nil {
		log.BusinessID = uuid.New()
	}
	if log.Model == "" {
		log.Model = "anthropic/claude-3-5-sonnet"
	}
	if log.Provider == "" {
		log.Provider = "anthropic"
	}
	b, err := json.Marshal(log)
	require.NoError(t, err)
	return b
}

func doRequest(t *testing.T, h *handler.InternalBillingHandler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/billing/usage_logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.LogUsage(rec, req)
	return rec
}

// Test 1: happy path — 204.
func TestLogUsage_Success_204(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	body := validUsageLogJSON(t, llm.UsageLog{})
	rec := doRequest(t, h, body)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String(), "204 must have empty body")
	assert.Equal(t, 1, repo.callCount())
}

// Test 2: malformed JSON — 400, repo not called.
func TestLogUsage_InvalidJSON_400(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	rec := doRequest(t, h, []byte("{not json"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_payload")
	assert.Equal(t, 0, repo.callCount())
}

// Test 3: missing business_id — 400.
func TestLogUsage_MissingBusinessID_400(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	// Construct body without business_id at all — uuid.Nil unmarshals.
	body := []byte(`{"model":"x","provider":"y"}`)
	rec := doRequest(t, h, body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_payload")
	assert.Equal(t, 0, repo.callCount())
}

// Test 4: negative input_tokens — 400 (defense in depth; DB CHECK also rejects).
func TestLogUsage_NegativeTokens_400(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	body := validUsageLogJSON(t, llm.UsageLog{InputTokens: -1})
	rec := doRequest(t, h, body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_payload")
	assert.Equal(t, 0, repo.callCount())
}

// Test 5: empty model — 400.
func TestLogUsage_EmptyModel_400(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	body := []byte(`{"business_id":"` + uuid.New().String() + `","provider":"x","model":""}`)
	rec := doRequest(t, h, body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_payload")
	assert.Equal(t, 0, repo.callCount())
}

// Test 6: repo error → 500, generic error body (no internals leaked).
func TestLogUsage_RepoError_500(t *testing.T) {
	repo := &mockBilling{}
	repo.failNext(errors.New("db connection refused"))
	h := handler.NewInternalBillingHandler(repo, nil)

	body := validUsageLogJSON(t, llm.UsageLog{})
	rec := doRequest(t, h, body)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "db connection refused",
		"server-side error must not leak through public body")
	assert.Equal(t, 1, repo.callCount(), "repo must be invoked exactly once")
}

// Test 7: GET to the handler → 405.
func TestLogUsage_WrongMethod_405(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/billing/usage_logs", http.NoBody)
	rec := httptest.NewRecorder()
	h.LogUsage(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, 0, repo.callCount())
}

// Test 10: conversation_id + cache token columns flow through.
func TestLogUsage_PersistsConversationIDAndCacheTokens(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	bizID := uuid.New()
	body, err := json.Marshal(llm.UsageLog{
		BusinessID:          bizID,
		ConversationID:      "65f1a2b3c4d5e6f7a8b9c0d1",
		Model:               "anthropic/claude-3-5-sonnet",
		Provider:            "anthropic",
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     200,
		CacheCreationTokens: 75,
	})
	require.NoError(t, err)

	rec := doRequest(t, h, body)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, 1, repo.callCount())

	got := repo.Calls[0]
	assert.Equal(t, bizID, got.BusinessID)
	assert.Equal(t, "65f1a2b3c4d5e6f7a8b9c0d1", got.ConversationID)
	assert.Equal(t, 100, got.InputTokens)
	assert.Equal(t, 50, got.OutputTokens)
	assert.Equal(t, 200, got.CacheReadTokens)
	assert.Equal(t, 75, got.CacheCreationTokens)
}

// Defensive extra: body exceeding MaxBytes (>64KB) is rejected as invalid payload.
func TestLogUsage_OversizedBody_400(t *testing.T) {
	repo := &mockBilling{}
	h := handler.NewInternalBillingHandler(repo, nil)

	// 128KB of JSON-ish payload (long string field) — past the 64KB cap.
	big := strings.Repeat("a", 128*1024)
	body := []byte(`{"business_id":"` + uuid.New().String() + `","model":"x","provider":"y","user_tier":"` + big + `"}`)
	rec := doRequest(t, h, body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, 0, repo.callCount())
}
