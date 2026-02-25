package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// TestChatProxy_EnrichesContext verifies that business and integration context
// is properly enriched and forwarded to the orchestrator.
func TestChatProxy_EnrichesContext(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{
		ID:          businessID,
		UserID:      userID,
		Name:        "Кофейня",
		Category:    "Кафе",
		Address:     "ул. Ленина, 1",
		Phone:       "+7-900-000-0000",
		Description: "Уютная кофейня",
	}

	integrations := []domain.Integration{
		{ID: uuid.New(), BusinessID: businessID, Platform: "telegram", Status: "active"},
		{ID: uuid.New(), BusinessID: businessID, Platform: "vk", Status: "active"},
		{ID: uuid.New(), BusinessID: businessID, Platform: "yandex", Status: "inactive"},
	}

	// Capture the forwarded request body
	var capturedBody map[string]interface{}
	orchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		err = json.Unmarshal(body, &capturedBody)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orchServer.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)

	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return(integrations, nil)

	h := NewChatProxyHandler(mockBiz, mockInteg, &MockMessageRepository{}, nil, nil, nil, orchServer.URL, nil)

	body := `{"message":"hello","model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-123", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Inject user ID via middleware key
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	// Set up chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-123")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedBody)

	assert.Equal(t, businessID.String(), capturedBody["business_id"])
	assert.Equal(t, "Кофейня", capturedBody["business_name"])
	assert.Equal(t, "Кафе", capturedBody["business_category"])

	activeIntegrations, ok := capturedBody["active_integrations"].([]interface{})
	require.True(t, ok, "active_integrations should be a list")
	assert.Len(t, activeIntegrations, 2)
	assert.Contains(t, activeIntegrations, "telegram")
	assert.Contains(t, activeIntegrations, "vk")
}

// TestChatProxy_StreamsSSE verifies that SSE events from the orchestrator
// are streamed back to the client unchanged.
func TestChatProxy_StreamsSSE(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test",
	}

	orchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"text\",\"content\":\"hello\"}\n\ndata: {\"type\":\"done\"}\n\n"))
	}))
	defer orchServer.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)

	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	h := NewChatProxyHandler(mockBiz, mockInteg, &MockMessageRepository{}, nil, nil, nil, orchServer.URL, nil)

	reqBody := `{"message":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-456", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-456")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))

	responseBody := rr.Body.String()
	assert.Contains(t, responseBody, `data: {"type":"text","content":"hello"}`)
	assert.Contains(t, responseBody, `data: {"type":"done"}`)
}

// TestChatProxy_NoBusiness verifies that a 404 is returned when the user
// has no business profile.
func TestChatProxy_NoBusiness(t *testing.T) {
	userID := uuid.New()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(nil, domain.ErrBusinessNotFound)

	mockInteg := new(MockIntegrationService)

	h := NewChatProxyHandler(mockBiz, mockInteg, &MockMessageRepository{}, nil, nil, nil, "http://unused", nil)

	reqBody := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-789", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-789")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)

	var errResp map[string]string
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "business not found")
}

// TestChatProxy_OrchestratorDown verifies that a 502 is returned when the
// orchestrator is unreachable.
func TestChatProxy_OrchestratorDown(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test",
	}

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)

	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	// Use a guaranteed unreachable address
	h := NewChatProxyHandler(mockBiz, mockInteg, &MockMessageRepository{}, nil, nil, nil, "http://127.0.0.1:1", nil)

	reqBody := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-000", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-000")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusBadGateway, rr.Code)

	var errResp map[string]string
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "orchestrator unavailable")
}
