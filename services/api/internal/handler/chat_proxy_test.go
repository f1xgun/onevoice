package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// newChatProxyNoProject returns a ChatProxyHandler wired with a stub
// projectService that returns ErrProjectNotFound and a stub conversation repo
// that returns a conversation with ProjectID = nil. Used by legacy tests that
// do not exercise project enrichment. The conversationID URL param is bound
// via chi.RouteCtxKey in each test. Phase 16 also injects an empty
// pendingRepo (no active batches) so the D-04 gate is a pass-through.
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
	return NewChatProxyHandler(biz, integ, proj, convRepo, msgRepo, &MockPendingToolCallRepository{}, nil, nil, nil, nil, orchURL, nil, nil)
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
	h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, &MockPendingToolCallRepository{}, nil, nil, nil, nil, orch.URL, nil, nil)

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
	h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, &MockPendingToolCallRepository{}, nil, nil, nil, nil, orch.URL, nil, nil)

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

// TestChatProxy_ScannerBuffer_HandlesLargeToolResult documents HITL-13: the
// SSE scanner buffer was bumped from 64KB to 1MB so large tool_result / pending
// batch payloads do not trigger a bufio.ErrTooLong during streaming.
// We stream a ~512KB tool_result event and assert the handler consumes it
// without a scanner error and persists the assistant Message with a single
// ToolResult whose payload matches.
func TestChatProxy_ScannerBuffer_HandlesLargeToolResult(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	// Build a ~512KB JSON payload (well above the old 64KB limit).
	bigText := strings.Repeat("a", 500_000)
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// One tool_call, one giant tool_result, one done.
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"toolu_big","tool_name":"telegram__send_channel_post","tool_args":{"text":"x"}}` + "\n\n"))
		payload := map[string]interface{}{"type": "tool_result", "tool_call_id": "toolu_big", "tool_name": "telegram__send_channel_post", "result": map[string]interface{}{"echo": bigText}}
		b, _ := json.Marshal(payload)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	var persistedMsg *domain.Message
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			if m.Role == "assistant" {
				persistedMsg = m
			}
			return nil
		},
	}
	h := newChatProxyNoProject(mockBiz, mockInteg, msgRepo, orch.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-big", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-big")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, persistedMsg, "assistant message should be persisted")
	require.Len(t, persistedMsg.ToolResults, 1, "the large tool_result should be captured without truncation")
	inner, ok := persistedMsg.ToolResults[0].Content["echo"].(string)
	require.True(t, ok)
	assert.Equal(t, 500_000, len(inner), "payload must round-trip the full 500K chars")
}

// TestChatProxy_NoSyntheticToolCallID documents HITL-13 anti-footgun #4: the
// synthetic "tc-N" ID generator was removed; chat_proxy propagates the LLM's
// real tool_call.id verbatim from the orchestrator's SSE event.
func TestChatProxy_NoSyntheticToolCallID(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"toolu_abc123","tool_name":"telegram__send_channel_post","tool_args":{"text":"x"}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"toolu_abc123","tool_name":"telegram__send_channel_post","result":{"ok":true}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	var persistedMsg *domain.Message
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			if m.Role == "assistant" {
				persistedMsg = m
			}
			return nil
		},
	}
	h := newChatProxyNoProject(mockBiz, mockInteg, msgRepo, orch.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-tcn", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-tcn")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, persistedMsg)
	require.Len(t, persistedMsg.ToolCalls, 1)
	assert.Equal(t, "toolu_abc123", persistedMsg.ToolCalls[0].ID, "ToolCall.ID must be the LLM's real id, not a synthetic tc-N")
	require.Len(t, persistedMsg.ToolResults, 1)
	assert.Equal(t, "toolu_abc123", persistedMsg.ToolResults[0].ToolCallID, "ToolResult must correlate by the real call_id, not by name")
}

// TestChatProxy_ToolApprovalRequired_PersistsPendingApprovalMessage covers the
// HITL-01 pause-time persistence branch: on tool_approval_required, chat_proxy
// persists an assistant Message with Status=pending_approval, ToolCalls marked
// Status=pending_approval and ApprovalID=<batchID>-<callID>, and forwards the
// SSE event to the client before closing the stream.
func TestChatProxy_ToolApprovalRequired_PersistsPendingApprovalMessage(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"text","content":"Here I'll post:"}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"toolu_abc","tool_name":"telegram__send_channel_post","tool_args":{"text":"привет"}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"tool_approval_required","batch_id":"batch-1","calls":[{"call_id":"toolu_abc","tool_name":"telegram__send_channel_post","args":{"text":"привет"},"editable_fields":["text"],"floor":"manual"}]}` + "\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	var persistedMsg *domain.Message
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			if m.Role == "assistant" {
				persistedMsg = m
			}
			return nil
		},
	}
	h := newChatProxyNoProject(mockBiz, mockInteg, msgRepo, orch.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-pause", strings.NewReader(`{"message":"post something"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-pause")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Client received the pause event.
	assert.Contains(t, rr.Body.String(), `"type":"tool_approval_required"`)
	assert.Contains(t, rr.Body.String(), `"batch_id":"batch-1"`)

	// Pending-approval Message is persisted.
	require.NotNil(t, persistedMsg, "assistant message must be persisted at pause time")
	assert.Equal(t, domain.MessageStatusPendingApproval, persistedMsg.Status)
	assert.Equal(t, "Here I'll post:", persistedMsg.Content)
	require.Len(t, persistedMsg.ToolCalls, 1)
	assert.Equal(t, "toolu_abc", persistedMsg.ToolCalls[0].ID)
	assert.Equal(t, "batch-1-toolu_abc", persistedMsg.ToolCalls[0].ApprovalID, "ApprovalID format is <batchID>-<callID>")
	assert.Equal(t, domain.ToolCallStatusPending, persistedMsg.ToolCalls[0].Status)
	assert.Empty(t, persistedMsg.ToolResults, "no tool results yet at pause time")
}

// --- Phase 16 Plan 06 Task 2: D-04 implicit-resume + explicit-resume tests ---

// TestChatProxy_Resume_AppendsToExistingMessage covers the happy path for
// explicit resume: client POSTs /chat/{id} with X-Onevoice-Resume-Batch-Id;
// the proxy forwards to /chat/{id}/resume?batch_id=..., the orchestrator
// emits tool_result events and done, the SAME assistant Message is updated
// (not a new one — D-17), and Status transitions to complete.
func TestChatProxy_Resume_AppendsToExistingMessage(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-resume"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	activeMsg := &domain.Message{
		ID:             "msg-1",
		ConversationID: convID,
		Role:           "assistant",
		Content:        "Here I'll post:",
		ToolCalls: []domain.ToolCall{
			{ID: "call-a", Name: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "x"}, Status: domain.ToolCallStatusPending},
			{ID: "call-b", Name: "vk__publish_post", Arguments: map[string]interface{}{"text": "x"}, Status: domain.ToolCallStatusPending},
		},
		Status: domain.MessageStatusPendingApproval,
	}

	// Orchestrator mock — assert resume URL + emit two tool_result events and done.
	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orchHits++
		assert.Contains(t, r.URL.Path, "/chat/"+convID+"/resume")
		assert.Equal(t, "batch-1", r.URL.Query().Get("batch_id"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"call-a","tool_name":"telegram__send_channel_post","result":{"ok":true}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"call-b","tool_name":"vk__publish_post","result":{"ok":true}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"text","content":"Done!"}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	var persistedUpdates []*domain.Message
	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, id string) (*domain.Message, error) {
			assert.Equal(t, convID, id)
			cp := *activeMsg
			return &cp, nil
		},
		UpdateFunc: func(_ context.Context, m *domain.Message) error {
			cp := *m
			cp.ToolCalls = append([]domain.ToolCall{}, m.ToolCalls...)
			cp.ToolResults = append([]domain.ToolResult{}, m.ToolResults...)
			persistedUpdates = append(persistedUpdates, &cp)
			return nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		GetByBatchIDFunc: func(_ context.Context, id string) (*domain.PendingToolCallBatch, error) {
			assert.Equal(t, "batch-1", id)
			return &domain.PendingToolCallBatch{ID: "batch-1", ConversationID: convID, Status: "resolving"}, nil
		},
	}

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any", ProjectID: nil}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ResumeBatchHeader, "batch-1")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 1, orchHits, "orchestrator's resume endpoint must be invoked")

	// The final Update carries the terminal state.
	require.NotEmpty(t, persistedUpdates)
	final := persistedUpdates[len(persistedUpdates)-1]
	assert.Equal(t, "msg-1", final.ID, "same message ID preserved (D-17)")
	assert.Equal(t, domain.MessageStatusComplete, final.Status)
	require.Len(t, final.ToolResults, 2)
	require.Len(t, final.ToolCalls, 2)
	assert.Equal(t, domain.ToolCallStatusApproved, final.ToolCalls[0].Status)
	assert.Equal(t, domain.ToolCallStatusApproved, final.ToolCalls[1].Status)
	assert.Contains(t, final.Content, "Done!", "post-tool text appended to Content")
}

// TestChatProxy_Resume_NoActiveApproval_EmitsInlineError covers the guard in
// streamResume: if the header points at a batch that does not exist (or has
// the wrong conversation_id), we emit an inline SSE error and close — we
// never call the orchestrator.
func TestChatProxy_Resume_NoActiveApproval_EmitsInlineError(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-resume-missing"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	// Message exists but batch does not.
	activeMsg := &domain.Message{
		ID: "msg-1", ConversationID: convID, Role: "assistant",
		Status: domain.MessageStatusPendingApproval,
	}

	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		orchHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, _ string) (*domain.Message, error) {
			return activeMsg, nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		GetByBatchIDFunc: func(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
			return nil, domain.ErrBatchNotFound
		},
	}
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any"}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{}`))
	req.Header.Set(ResumeBatchHeader, "batch-missing")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 0, orchHits, "orchestrator must NOT be invoked when the batch is missing")
	assert.Contains(t, rr.Body.String(), "no_active_approval_for_conversation")
}

// TestChatProxy_Resume_ErrorEvent_TransitionsOffPendingApproval covers the
// resume-path bug where an LLM error / ctx cancellation / max-iterations cap
// during the post-resolve agent loop emitted SSE `error` and closed the stream
// without ever sending `done`. Pre-fix the assistant Message stayed in
// pending_approval forever, so the next POST /chat hit the D-04 gate's
// "turn_already_in_progress" branch and the conversation was permanently stuck
// behind a hanging loader. After the fix: the error event flips Status to
// complete (Content / ToolCall statuses preserved) so the chat unblocks.
func TestChatProxy_Resume_ErrorEvent_TransitionsOffPendingApproval(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-resume-error"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	activeMsg := &domain.Message{
		ID:             "msg-stuck",
		ConversationID: convID,
		Role:           "assistant",
		Content:        "",
		ToolCalls: []domain.ToolCall{
			{ID: "call-a", Name: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "x"}, Status: domain.ToolCallStatusPending},
		},
		Status: domain.MessageStatusPendingApproval,
	}

	// Orchestrator mock — emit a tool_rejected event then an error and close.
	// Mirrors the real production trace: user rejected the call, orchestrator
	// sent the rejection ack, then stepRun's LLM follow-up was canceled.
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"tool_rejected","tool_call_id":"call-a","tool_name":"telegram__send_channel_post","content":"user_rejected"}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"type":"error","content":"context canceled"}` + "\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	var persistedUpdates []*domain.Message
	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, _ string) (*domain.Message, error) {
			cp := *activeMsg
			cp.ToolCalls = append([]domain.ToolCall{}, activeMsg.ToolCalls...)
			return &cp, nil
		},
		UpdateFunc: func(_ context.Context, m *domain.Message) error {
			cp := *m
			cp.ToolCalls = append([]domain.ToolCall{}, m.ToolCalls...)
			cp.ToolResults = append([]domain.ToolResult{}, m.ToolResults...)
			persistedUpdates = append(persistedUpdates, &cp)
			return nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		GetByBatchIDFunc: func(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
			return &domain.PendingToolCallBatch{ID: "batch-1", ConversationID: convID, Status: "resolving"}, nil
		},
	}
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any", ProjectID: nil}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ResumeBatchHeader, "batch-1")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Error event must reach the client — frontend renders it via SSE handler.
	assert.Contains(t, rr.Body.String(), `"type":"error"`)

	// The persistResumeDone path triggered by the error event must have run,
	// transitioning Status off pending_approval. Without this, the D-04 gate
	// keeps blocking new turns.
	require.NotEmpty(t, persistedUpdates, "Update must be called when error event fires")
	final := persistedUpdates[len(persistedUpdates)-1]
	assert.Equal(t, "msg-stuck", final.ID, "same message ID preserved (D-17)")
	assert.Equal(t, domain.MessageStatusComplete, final.Status, "error must clear pending_approval")
	require.Len(t, final.ToolCalls, 1)
	assert.Equal(t, domain.ToolCallStatusRejected, final.ToolCalls[0].Status, "tool_rejected status preserved")
}

// TestChatProxy_Resume_StreamEndedWithoutDone_TransitionsOffPendingApproval
// covers the fall-through path: the orchestrator closes the connection after
// streaming non-terminal events (e.g. tool_rejected) but never emits an error
// or done event. Pre-fix the partial-state Update kept Status=pending_approval
// → conversation stuck. Post-fix: status flips to complete on stream-end too.
func TestChatProxy_Resume_StreamEndedWithoutDone_TransitionsOffPendingApproval(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-resume-no-done"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	activeMsg := &domain.Message{
		ID:             "msg-stuck-2",
		ConversationID: convID,
		Role:           "assistant",
		ToolCalls: []domain.ToolCall{
			{ID: "call-a", Name: "telegram__send_channel_post", Arguments: map[string]interface{}{}, Status: domain.ToolCallStatusPending},
		},
		Status: domain.MessageStatusPendingApproval,
	}

	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"tool_rejected","tool_call_id":"call-a","tool_name":"telegram__send_channel_post","content":"user_rejected"}` + "\n\n"))
		// Connection closes without done or error.
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	var persistedUpdates []*domain.Message
	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, _ string) (*domain.Message, error) {
			cp := *activeMsg
			cp.ToolCalls = append([]domain.ToolCall{}, activeMsg.ToolCalls...)
			return &cp, nil
		},
		UpdateFunc: func(_ context.Context, m *domain.Message) error {
			cp := *m
			cp.ToolCalls = append([]domain.ToolCall{}, m.ToolCalls...)
			persistedUpdates = append(persistedUpdates, &cp)
			return nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		GetByBatchIDFunc: func(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
			return &domain.PendingToolCallBatch{ID: "batch-1", ConversationID: convID, Status: "resolving"}, nil
		},
	}
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any", ProjectID: nil}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ResumeBatchHeader, "batch-1")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotEmpty(t, persistedUpdates, "Update must run on stream-end-without-done")
	final := persistedUpdates[len(persistedUpdates)-1]
	assert.Equal(t, domain.MessageStatusComplete, final.Status, "fall-through must clear pending_approval")
	require.Len(t, final.ToolCalls, 1)
	assert.Equal(t, domain.ToolCallStatusRejected, final.ToolCalls[0].Status, "tool_rejected status preserved on fall-through")
}

// TestChatProxy_ImplicitResume_InProgressMessage_Rejoins covers D-04 case (b):
// no resume header, but the conversation has an in_progress message AND a
// resolving batch → implicit rejoin via the orchestrator's resume endpoint.
func TestChatProxy_ImplicitResume_InProgressMessage_Rejoins(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-implicit"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	activeMsg := &domain.Message{
		ID: "msg-2", ConversationID: convID, Role: "assistant",
		Status: domain.MessageStatusInProgress,
		ToolCalls: []domain.ToolCall{
			{ID: "call-x", Name: "telegram__send_channel_post", Status: domain.ToolCallStatusPending},
		},
	}

	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orchHits++
		assert.Equal(t, "batch-resolving", r.URL.Query().Get("batch_id"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	createCalls := 0
	updateCalls := 0
	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, _ string) (*domain.Message, error) {
			cp := *activeMsg
			return &cp, nil
		},
		CreateFunc: func(_ context.Context, _ *domain.Message) error { createCalls++; return nil },
		UpdateFunc: func(_ context.Context, m *domain.Message) error {
			updateCalls++
			// ID preserved.
			assert.Equal(t, "msg-2", m.ID)
			return nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return []*domain.PendingToolCallBatch{
				{ID: "batch-resolving", ConversationID: convID, Status: "resolving"},
			}, nil
		},
		GetByBatchIDFunc: func(_ context.Context, id string) (*domain.PendingToolCallBatch, error) {
			assert.Equal(t, "batch-resolving", id)
			return &domain.PendingToolCallBatch{ID: "batch-resolving", ConversationID: convID, Status: "resolving"}, nil
		},
	}
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any"}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{"message":"(empty resume body)"}`))
	// NO ResumeBatchHeader here — implicit resume.
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 1, orchHits, "orchestrator resume must be invoked")
	assert.Equal(t, 0, createCalls, "no new Message should be created on implicit resume")
	assert.GreaterOrEqual(t, updateCalls, 1, "Update should be called at least once on done")
}

// TestChatProxy_Reconnect_PendingBatch_ReEmitsApprovalEvent covers D-04 case (c):
// no resume header, an active pending_approval message, and a batch still in
// status="pending" → re-emit the stored tool_approval_required event from the
// batch document so the UI re-hydrates. Orchestrator is NOT invoked.
func TestChatProxy_Reconnect_PendingBatch_ReEmitsApprovalEvent(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-pending-reconnect"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	activeMsg := &domain.Message{
		ID: "msg-3", ConversationID: convID, Role: "assistant",
		Status: domain.MessageStatusPendingApproval,
	}

	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		orchHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, _ string) (*domain.Message, error) {
			return activeMsg, nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return []*domain.PendingToolCallBatch{
				{
					ID: "batch-pending", ConversationID: convID, MessageID: "msg-3", Status: "pending",
					Calls: []domain.PendingCall{
						{CallID: "call-p", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
					},
				},
			}, nil
		},
	}
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any"}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{}`))
	// No resume header.
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 0, orchHits, "orchestrator must NOT be invoked; the event is re-emitted from the stored batch")
	assert.Contains(t, rr.Body.String(), `"type":"tool_approval_required"`)
	assert.Contains(t, rr.Body.String(), `"batch_id":"batch-pending"`)
	assert.Contains(t, rr.Body.String(), `"call_id":"call-p"`)
}

// TestChatProxy_OrphanInProgress_NoBatch_EmitsTurnAlreadyInProgress covers D-04
// case (d): active in_progress Message with NO batch (shouldn't happen in
// healthy flow) → inline error "turn_already_in_progress".
func TestChatProxy_OrphanInProgress_NoBatch_EmitsTurnAlreadyInProgress(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "conv-orphan"

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	activeMsg := &domain.Message{
		ID: "msg-orphan", ConversationID: convID, Role: "assistant",
		Status: domain.MessageStatusInProgress,
	}

	orchHits := 0
	orch := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		orchHits++
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	msgRepo := &MockMessageRepository{
		FindByConversationActiveFunc: func(_ context.Context, _ string) (*domain.Message, error) {
			return activeMsg, nil
		},
	}
	pendingRepo := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return nil, nil // no active batches
		},
	}
	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: id, UserID: "any"}, nil
		},
	}
	h := NewChatProxyHandler(mockBiz, mockInteg, &noopProjectService{}, convRepo, msgRepo, pendingRepo, nil, nil, nil, nil, orch.URL, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+convID, strings.NewReader(`{}`))
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", convID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 0, orchHits)
	assert.Contains(t, rr.Body.String(), `"type":"error"`)
	assert.Contains(t, rr.Body.String(), `"content":"turn_already_in_progress"`)
}

// TestChatProxy_ToolApprovalRequired_NoErrorIfPersistFails covers the
// best-effort fallback: even if the Message insert fails (e.g. duplicate _id
// from a prior crash), the client still receives the tool_approval_required
// event so the approval card hydrates via GET /messages later.
func TestChatProxy_ToolApprovalRequired_NoErrorIfPersistFails(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Biz"}

	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"tool_approval_required","batch_id":"batch-2","calls":[{"call_id":"toolu_fail","tool_name":"vk__publish_post","args":{"text":"x"},"floor":"manual"}]}` + "\n\n"))
	}))
	defer orch.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			if m.Role == "assistant" {
				return fmt.Errorf("simulated persist failure")
			}
			return nil
		},
	}
	h := newChatProxyNoProject(mockBiz, mockInteg, msgRepo, orch.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-fail", strings.NewReader(`{"message":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-fail")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	// Client still got the pause event (no error event).
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"type":"tool_approval_required"`)
	assert.NotContains(t, rr.Body.String(), `"type":"error"`)
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
	h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, &MockPendingToolCallRepository{}, nil, nil, nil, nil, orch.URL, nil, nil)

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

// TestChatProxy_ForwardsPhase16Fields covers Plan 17-07 GAP-03 closure on the
// API side: the proxy MUST forward five Phase-16 keys to the orchestrator on
// every fresh-turn request — user_id (JWT subject), message_id (the just-
// saved userMsg.ID), tier, business_approvals (from Business.ToolApprovals()),
// project_approval_overrides (from project.ApprovalOverrides). Without these,
// the orchestrator persists PendingToolCallBatch with empty IDs and HITL-11
// hydration is impossible. See VERIFICATION.md §GAP-03.
func TestChatProxy_ForwardsPhase16Fields(t *testing.T) {
	t.Run("with project — all five keys present and populated", func(t *testing.T) {
		userID := uuid.New()
		businessID := uuid.New()
		projectID := uuid.New()
		conversationID := "conv-p16-1"

		business := &domain.Business{
			ID:     businessID,
			UserID: userID,
			Name:   "Biz",
			Settings: map[string]interface{}{
				"tool_approvals": map[string]interface{}{
					"telegram__send_channel_post": "manual",
				},
			},
		}

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

		// Capture the userMsg.ID assigned by chat_proxy before Create.
		var capturedUserMsgID string
		msgRepo := &MockMessageRepository{
			CreateFunc: func(_ context.Context, m *domain.Message) error {
				if m.Role == "user" {
					capturedUserMsgID = m.ID
				}
				return nil
			},
		}

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
					ID:         projectID,
					BusinessID: businessID,
					Name:       "Отзывы",
					ApprovalOverrides: map[string]domain.ToolFloor{
						"vk__publish_post": "auto",
					},
				}, nil
			},
		}
		h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, msgRepo, &MockPendingToolCallRepository{}, nil, nil, nil, nil, orch.URL, nil, nil)

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
		require.NotNil(t, captured, "orchestrator request body should be captured")

		// All five Phase-16 keys MUST be present.
		assert.Contains(t, captured, "user_id")
		assert.Contains(t, captured, "message_id")
		assert.Contains(t, captured, "tier")
		assert.Contains(t, captured, "business_approvals")
		assert.Contains(t, captured, "project_approval_overrides")

		// user_id is the JWT subject (uuid).
		assert.Equal(t, userID.String(), captured["user_id"])

		// message_id matches the userMsg.ID set on the wire (non-empty).
		mid, ok := captured["message_id"].(string)
		require.True(t, ok, "message_id must be a string, got %T", captured["message_id"])
		assert.NotEmpty(t, mid, "message_id must be non-empty")
		assert.Equal(t, capturedUserMsgID, mid, "message_id must equal the just-saved userMsg.ID")

		// tier — string (empty acceptable per Plan 17-07; v1.3 has no tier model).
		_, ok = captured["tier"].(string)
		assert.True(t, ok, "tier must be a string, got %T", captured["tier"])

		// business_approvals: non-nil map echoing Business.ToolApprovals().
		ba, ok := captured["business_approvals"].(map[string]interface{})
		require.True(t, ok, "business_approvals must be a JSON object, got %T", captured["business_approvals"])
		assert.Equal(t, "manual", ba["telegram__send_channel_post"])

		// project_approval_overrides: non-nil map echoing project.ApprovalOverrides.
		po, ok := captured["project_approval_overrides"].(map[string]interface{})
		require.True(t, ok, "project_approval_overrides must be a JSON object, got %T", captured["project_approval_overrides"])
		assert.Equal(t, "auto", po["vk__publish_post"])
	})

	t.Run("without project — project_approval_overrides marshals as {} not null", func(t *testing.T) {
		userID := uuid.New()
		businessID := uuid.New()
		conversationID := "conv-p16-noproj"

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
				return &domain.Conversation{ID: id, UserID: userID.String(), ProjectID: nil}, nil
			},
		}
		proj := &noopProjectService{}
		h := NewChatProxyHandler(mockBiz, mockInteg, proj, convRepo, &MockMessageRepository{}, &MockPendingToolCallRepository{}, nil, nil, nil, nil, orch.URL, nil, nil)

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

		// project_approval_overrides MUST be present and a non-nil map (empty
		// object) — never null. The frontend / orchestrator's JSON decode
		// behaves differently for {} vs null.
		require.Contains(t, captured, "project_approval_overrides")
		po, ok := captured["project_approval_overrides"].(map[string]interface{})
		require.True(t, ok, "project_approval_overrides must be a JSON object {}, not null. got %T", captured["project_approval_overrides"])
		assert.Empty(t, po, "without project, project_approval_overrides must be an empty object")

		// business_approvals also non-nil empty map.
		require.Contains(t, captured, "business_approvals")
		ba, ok := captured["business_approvals"].(map[string]interface{})
		require.True(t, ok, "business_approvals must be a JSON object {}, not null. got %T", captured["business_approvals"])
		assert.Empty(t, ba)

		// user_id still populated from JWT subject.
		assert.Equal(t, userID.String(), captured["user_id"])
	})
}

// TestFireAutoTitleIfPending exercises the gate predicate of
// fireAutoTitleIfPending directly — bypasses the SSE machinery so the test
// is fast and deterministic. Covers the full branch matrix that drives
// Plan 18-05's trust contract:
//
//   - status=auto_pending → fires (FakeChatCaller.Calls() ≥ 1)
//   - status=manual       → no-op (D-01 / D-02: manual is sovereign)
//   - status=auto         → no-op (D-01: only auto_pending fires)
//   - GetByID error       → no-op + warn log (graceful degradation)
//   - h.titler == nil     → no-op (graceful disable per A6 / Pitfall 1)
//
// Each subcase constructs its own ChatProxyHandler so the test stays
// isolated from the others' fakes. The persistCtx closure mirrors the
// production helper at chat_proxy.go:166-172 (5s detached ctx).
func TestFireAutoTitleIfPending(t *testing.T) {
	persistCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}
	convID := "507f1f77bcf86cd799439200"
	bizID := "biz-1"
	userText := "помоги опубликовать пост"
	assistantText := "конечно, какая платформа?"

	// Helper: build ChatProxyHandler with a real *service.Titler driven by a
	// FakeChatCaller, and a stub conversation repo with the given snapshot.
	build := func(t *testing.T, conv *domain.Conversation, getByIDErr error, withTitler bool) (*ChatProxyHandler, *service.FakeChatCaller) {
		t.Helper()
		repo := &MockConversationRepository{
			GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
				if getByIDErr != nil {
					return nil, getByIDErr
				}
				cp := *conv
				return &cp, nil
			},
		}
		fc := &service.FakeChatCaller{ReturnContent: "Опубликование поста"}
		var titler *service.Titler
		if withTitler {
			titler = service.NewTitler(fc, repo, "test-model")
		}
		mockBiz := new(MockBusinessService)
		mockInteg := new(MockIntegrationService)
		h := NewChatProxyHandler(
			mockBiz, mockInteg, &noopProjectService{}, repo,
			&MockMessageRepository{}, &MockPendingToolCallRepository{},
			nil, nil, nil, nil, "", nil, titler,
		)
		return h, fc
	}

	// Settle helper: the goroutine spawned inside fireAutoTitleIfPending
	// races us; poll the FakeChatCaller for up to 500ms.
	waitForFire := func(fc *service.FakeChatCaller, want bool) bool {
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if (fc.Calls() > 0) == want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return (fc.Calls() > 0) == want
	}

	t.Run("fires_on_auto_pending", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusAutoPending,
		}
		h, fc := build(t, conv, nil, true)

		h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)

		require.True(t, waitForFire(fc, true), "expected FakeChatCaller to fire on auto_pending; calls=%d", fc.Calls())
		assert.GreaterOrEqual(t, fc.Calls(), 1)
	})

	t.Run("noop_on_manual", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusManual,
		}
		h, fc := build(t, conv, nil, true)

		h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)

		// Negative settle: ensure NO fire even after settle window. D-01 / D-02.
		require.True(t, waitForFire(fc, false), "manual must not fire titler; calls=%d", fc.Calls())
		assert.Equal(t, 0, fc.Calls())
	})

	t.Run("noop_on_auto", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusAuto,
		}
		h, fc := build(t, conv, nil, true)

		h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)

		require.True(t, waitForFire(fc, false), "auto (terminal) must not fire titler; calls=%d", fc.Calls())
		assert.Equal(t, 0, fc.Calls())
	})

	t.Run("noop_when_titler_nil", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusAutoPending,
		}
		h, fc := build(t, conv, nil, false) // titler nil — graceful disable
		assert.NotPanics(t, func() {
			h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)
		})
		// fc isn't wired into the handler since titler is nil; just confirm
		// no panic and no Chat dispatched (counter stays at 0 because no
		// goroutine was spawned).
		assert.Equal(t, 0, fc.Calls())
	})

	t.Run("noop_on_getbyid_error", func(t *testing.T) {
		h, fc := build(t, nil, errors.New("mongo: connection refused"), true)

		h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)

		require.True(t, waitForFire(fc, false), "lookup error must not fire titler; calls=%d", fc.Calls())
		assert.Equal(t, 0, fc.Calls())
	})
}

// TestFireAutoTitleIfPendingResume exercises the resume-path counterpart.
// It uses the assistant message provided directly (resume path doesn't have
// req.Message in scope and walks history backward).
func TestFireAutoTitleIfPendingResume(t *testing.T) {
	persistCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}
	convID := "507f1f77bcf86cd799439210"
	bizID := "biz-resume"

	build := func(t *testing.T, conv *domain.Conversation, msgs []domain.Message, withTitler bool) (*ChatProxyHandler, *service.FakeChatCaller) {
		t.Helper()
		repo := &MockConversationRepository{
			GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
				if conv == nil {
					return nil, domain.ErrConversationNotFound
				}
				cp := *conv
				return &cp, nil
			},
		}
		msgRepo := &MockMessageRepository{
			ListByConversationIDFunc: func(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
				return msgs, nil
			},
		}
		fc := &service.FakeChatCaller{ReturnContent: "Возобновлённая беседа"}
		var titler *service.Titler
		if withTitler {
			titler = service.NewTitler(fc, repo, "test-model")
		}
		mockBiz := new(MockBusinessService)
		mockInteg := new(MockIntegrationService)
		h := NewChatProxyHandler(
			mockBiz, mockInteg, &noopProjectService{}, repo,
			msgRepo, &MockPendingToolCallRepository{},
			nil, nil, nil, nil, "", nil, titler,
		)
		return h, fc
	}

	waitForFire := func(fc *service.FakeChatCaller, want bool) bool {
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if (fc.Calls() > 0) == want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return (fc.Calls() > 0) == want
	}

	t.Run("fires_on_auto_pending_with_user_history", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusAutoPending,
		}
		msgs := []domain.Message{
			{ID: "m1", ConversationID: convID, Role: "user", Content: "проверка статуса"},
			{ID: "m2", ConversationID: convID, Role: "assistant", Content: "выполнено", Status: domain.MessageStatusComplete},
		}
		h, fc := build(t, conv, msgs, true)

		assistantMsg := &domain.Message{ID: "m2", ConversationID: convID, Role: "assistant", Content: "выполнено"}
		h.fireAutoTitleIfPendingResume(persistCtx, convID, assistantMsg)

		require.True(t, waitForFire(fc, true), "resume path must fire on auto_pending; calls=%d", fc.Calls())
		assert.GreaterOrEqual(t, fc.Calls(), 1)
	})

	t.Run("noop_on_manual_resume", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusManual,
		}
		h, fc := build(t, conv, nil, true)

		assistantMsg := &domain.Message{ID: "m1", Role: "assistant", Content: "ok"}
		h.fireAutoTitleIfPendingResume(persistCtx, convID, assistantMsg)

		require.True(t, waitForFire(fc, false), "resume manual must not fire; calls=%d", fc.Calls())
	})

	t.Run("noop_when_titler_nil_resume", func(t *testing.T) {
		conv := &domain.Conversation{
			ID: convID, BusinessID: bizID,
			TitleStatus: domain.TitleStatusAutoPending,
		}
		h, _ := build(t, conv, nil, false)

		assistantMsg := &domain.Message{ID: "m1", Role: "assistant", Content: "ok"}
		assert.NotPanics(t, func() {
			h.fireAutoTitleIfPendingResume(persistCtx, convID, assistantMsg)
		})
	})
}
