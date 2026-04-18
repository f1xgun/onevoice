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

// newChatProxyNoProject returns a ChatProxyHandler wired with a stub
// projectService that returns ErrProjectNotFound and a stub conversation repo
// that returns a conversation with ProjectID = nil. Used by legacy tests that
// do not exercise project enrichment. The conversationID URL param is bound
// via chi.RouteCtxKey in each test.
func newChatProxyNoProject(
	biz BusinessService,
	integ IntegrationService,
	msgRepo domain.MessageRepository,
	orchURL string,
) *ChatProxyHandler {
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any", ProjectID: nil}, nil
		},
	}
	proj := &noopProjectService{}
	return NewChatProxyHandler(biz, integ, proj, convRepo, msgRepo, nil, nil, nil, orchURL, nil)
}

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

	h := newChatProxyNoProject(mockBiz, mockInteg, &MockMessageRepository{}, orchServer.URL)

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

	h := newChatProxyNoProject(mockBiz, mockInteg, &MockMessageRepository{}, orchServer.URL)

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

	h := newChatProxyNoProject(mockBiz, mockInteg, &MockMessageRepository{}, "http://unused")

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
	h := newChatProxyNoProject(mockBiz, mockInteg, &MockMessageRepository{}, "http://127.0.0.1:1")

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

// TestChatProxy_ProjectEnrichment_WithoutProject covers Plan 15-04 Task 3
// Behavior 1: when conversation.project_id is null, orchestrator request
// contains empty project_* fields and an empty allowed_tools slice.
func TestChatProxy_ProjectEnrichment_WithoutProject(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	conversationID := "conv-np-1"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	var captured map[string]interface{}
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			assert.Equal(t, conversationID, id)
			return &domain.Conversation{ID: id, UserID: userID.String(), ProjectID: nil}, nil
		},
	}
	// projectService never called because ProjectID is nil.
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			t.Fatal("projectService.GetByID should not be called when conversation.ProjectID is nil")
			return nil, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, nil, nil, nil, orch.URL, nil)

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+conversationID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, captured, "orchestrator request body should be captured")

	assert.Equal(t, "", captured["project_id"])
	assert.Equal(t, "", captured["project_name"])
	assert.Equal(t, "", captured["project_system_prompt"])
	assert.Equal(t, "", captured["project_whitelist_mode"])
	// Empty (not nil) so the JSON wire shape is `[]`, not `null`.
	tools, ok := captured["project_allowed_tools"].([]interface{})
	require.True(t, ok, "project_allowed_tools must be a JSON array; got: %T", captured["project_allowed_tools"])
	assert.Empty(t, tools)
}

// TestChatProxy_ProjectEnrichment_WithProjectExplicitWhitelist covers Behavior 2:
// the project's name, system prompt, whitelist_mode and allowed_tools are
// forwarded to the orchestrator verbatim.
func TestChatProxy_ProjectEnrichment_WithProjectExplicitWhitelist(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	conversationID := "conv-p-1"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	var captured map[string]interface{}
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	projIDStr := projectID.String()
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: conversationID, UserID: userID.String(), ProjectID: &projIDStr}, nil
		},
	}
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, bizID, pid uuid.UUID) (*domain.Project, error) {
			assert.Equal(t, businessID, bizID)
			assert.Equal(t, projectID, pid)
			return &domain.Project{
				ID:            projectID,
				BusinessID:    businessID,
				Name:          "Отзывы",
				SystemPrompt:  "Отвечай вежливо",
				WhitelistMode: domain.WhitelistModeExplicit,
				AllowedTools:  []string{"telegram__send_channel_post"},
			}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, nil, nil, nil, orch.URL, nil)

	body := `{"message":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+conversationID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, captured)

	assert.Equal(t, projectID.String(), captured["project_id"])
	assert.Equal(t, "Отзывы", captured["project_name"])
	assert.Equal(t, "Отвечай вежливо", captured["project_system_prompt"])
	assert.Equal(t, "explicit", captured["project_whitelist_mode"])
	tools, ok := captured["project_allowed_tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	assert.Equal(t, "telegram__send_channel_post", tools[0])
}

// TestChatProxy_ProjectEnrichment_StaleProjectID covers Behavior 3: when the
// project_id on the conversation points to a deleted / missing project, the
// proxy logs a warning and falls back to the no-project path — the chat still
// succeeds and the orchestrator sees empty project_* fields.
func TestChatProxy_ProjectEnrichment_StaleProjectID(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	conversationID := "conv-stale-1"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	var captured map[string]interface{}
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	projIDStr := projectID.String()
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: conversationID, UserID: userID.String(), ProjectID: &projIDStr}, nil
		},
	}
	// Project was deleted or never existed — service returns ErrProjectNotFound.
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			return nil, domain.ErrProjectNotFound
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, nil, nil, nil, orch.URL, nil)

	body := `{"message":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+conversationID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	// Best-effort: chat still succeeds.
	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, captured)
	// Fell through to no-project path.
	assert.Equal(t, "", captured["project_id"])
	assert.Equal(t, "", captured["project_name"])
	assert.Equal(t, "", captured["project_whitelist_mode"])
}
