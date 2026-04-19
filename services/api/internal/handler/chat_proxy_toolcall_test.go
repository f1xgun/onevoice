package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// capturingMessageRepo records every Create call so the test can inspect the
// accumulated tool_calls / tool_results.
type capturingMessageRepo struct {
	mu       sync.Mutex
	messages []*domain.Message
}

func (r *capturingMessageRepo) Create(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, m)
	return nil
}

func (r *capturingMessageRepo) ListByConversationID(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
	return []domain.Message{}, nil
}

func (r *capturingMessageRepo) CountByConversationID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

// TestChatProxy_ToolCallIDCorrelation verifies that toolCalls ↔ toolResults are
// correlated by the orchestrator-issued tool_call_id — not by tool name. This
// protects the case where the LLM invokes the same tool twice in one batch:
// under the old name-keyed map only one result would land on a message.
func TestChatProxy_ToolCallIDCorrelation(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Test"}

	// Orchestrator returns two tool_calls with the SAME name but distinct IDs,
	// plus results emitted in a DIFFERENT order than the calls. Correlation by
	// name would misalign — by ID it works.
	sse := strings.Join([]string{
		`data: {"type":"tool_call","tool_call_id":"call_a","tool_name":"telegram__send_channel_post","tool_args":{"text":"первый"}}`,
		`data: {"type":"tool_call","tool_call_id":"call_b","tool_name":"telegram__send_channel_post","tool_args":{"text":"второй"}}`,
		`data: {"type":"tool_result","tool_call_id":"call_a","tool_name":"telegram__send_channel_post","result":{"message_id":1}}`,
		`data: {"type":"tool_result","tool_call_id":"call_b","tool_name":"telegram__send_channel_post","result":{"message_id":2}}`,
		`data: {"type":"done"}`,
		"",
	}, "\n\n")

	orchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	defer orchServer.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)

	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	msgRepo := &capturingMessageRepo{}
	h := NewChatProxyHandler(mockBiz, mockInteg, msgRepo, nil, nil, nil, nil, orchServer.URL, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-1", strings.NewReader(`{"message":"send two"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	msgRepo.mu.Lock()
	defer msgRepo.mu.Unlock()

	// Two messages: the user's input + the assistant response. The assistant
	// message is where tool_calls/tool_results accumulate.
	require.Len(t, msgRepo.messages, 2)
	assistant := msgRepo.messages[1]
	require.Equal(t, "assistant", assistant.Role)
	require.Len(t, assistant.ToolCalls, 2)
	require.Len(t, assistant.ToolResults, 2)

	// IDs are the LLM-issued ones, preserved from SSE.
	assert.Equal(t, "call_a", assistant.ToolCalls[0].ID)
	assert.Equal(t, "call_b", assistant.ToolCalls[1].ID)
	assert.Equal(t, "первый", assistant.ToolCalls[0].Arguments["text"])
	assert.Equal(t, "второй", assistant.ToolCalls[1].Arguments["text"])

	// Each result references its originating call by ID.
	assert.Equal(t, "call_a", assistant.ToolResults[0].ToolCallID)
	assert.Equal(t, "call_b", assistant.ToolResults[1].ToolCallID)
	assert.EqualValues(t, 1, assistant.ToolResults[0].Content["message_id"])
	assert.EqualValues(t, 2, assistant.ToolResults[1].Content["message_id"])
}
