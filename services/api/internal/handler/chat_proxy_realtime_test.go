package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// spyAgentTaskRepo records Create/Update/GetByID calls for assertions.
type spyAgentTaskRepo struct {
	mu      sync.Mutex
	created []domain.AgentTask
	updated []domain.AgentTask
	nextID  int
	byID    map[string]domain.AgentTask
}

func newSpyAgentTaskRepo() *spyAgentTaskRepo {
	return &spyAgentTaskRepo{byID: make(map[string]domain.AgentTask)}
}

func (r *spyAgentTaskRepo) Create(_ context.Context, t *domain.AgentTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	t.ID = "task-" + uuidShort(r.nextID)
	r.created = append(r.created, *t)
	r.byID[t.ID] = *t
	return nil
}

func (r *spyAgentTaskRepo) Update(_ context.Context, t *domain.AgentTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.byID[t.ID]
	if !ok {
		return domain.ErrAgentTaskNotFound
	}
	cur.Status = t.Status
	if t.CompletedAt != nil {
		cur.CompletedAt = t.CompletedAt
	}
	if t.Output != nil {
		cur.Output = t.Output
	}
	if t.Error != "" {
		cur.Error = t.Error
	}
	r.byID[t.ID] = cur
	r.updated = append(r.updated, cur)
	return nil
}

func (r *spyAgentTaskRepo) GetByID(_ context.Context, _businessID, taskID string) (*domain.AgentTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.byID[taskID]
	if !ok {
		return nil, domain.ErrAgentTaskNotFound
	}
	return &t, nil
}

func (r *spyAgentTaskRepo) ListByBusinessID(_ context.Context, _ string, _ domain.TaskFilter) ([]domain.AgentTask, int, error) {
	return nil, 0, nil
}

func uuidShort(n int) string {
	return string(rune('a'+(n-1))) + "01"
}

// TestChatProxy_Realtime_CreatesRunningThenUpdatesDone verifies that a
// tool_call SSE event produces a "running" AgentTask + task.created hub
// event, and the following tool_result flips it to "done" + task.updated.
func TestChatProxy_Realtime_CreatesRunningThenUpdatesDone(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	business := &domain.Business{ID: businessID, UserID: userID, Name: "Кофейня"}
	integrations := []domain.Integration{
		{ID: uuid.New(), BusinessID: businessID, Platform: "telegram", Status: "active"},
	}

	// Orchestrator that emits a tool_call then a tool_result with the same ID.
	orchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"call_abc","tool_name":"telegram__send_channel_post","tool_display_name":"Отправить пост","tool_args":{"text":"hi"}}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"call_abc","tool_name":"telegram__send_channel_post","result":{"ok":true}}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		flusher.Flush()
	}))
	defer orchServer.Close()

	mockBiz := new(MockBusinessService)
	mockBiz.On("GetByUserID", mock.Anything, userID).Return(business, nil)
	mockInteg := new(MockIntegrationService)
	mockInteg.On("ListByBusinessID", mock.Anything, businessID).Return(integrations, nil)

	hub := taskhub.New()
	events, unsub := hub.Subscribe(businessID.String())
	defer unsub()

	spy := newSpyAgentTaskRepo()

	h := NewChatProxyHandler(
		mockBiz,
		mockInteg,
		&noopProjectService{},
		&MockConversationRepository{
			GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
				return &domain.Conversation{ID: id, UserID: "any", ProjectID: nil}, nil
			},
		},
		&MockMessageRepository{},
		&MockPendingToolCallRepository{},
		nil, nil, spy, hub, orchServer.URL, nil,
	)

	body := `{"message":"post please","model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conv-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("conversationID", "conv-1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Chat(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	// Assert repo interactions.
	spy.mu.Lock()
	defer spy.mu.Unlock()
	require.Len(t, spy.created, 1, "expected one Create call")
	assert.Equal(t, "running", spy.created[0].Status)
	assert.Equal(t, "send_channel_post", spy.created[0].Type)
	assert.Equal(t, "telegram", spy.created[0].Platform)
	assert.Equal(t, "Отправить пост", spy.created[0].DisplayName)
	require.NotNil(t, spy.created[0].StartedAt)
	assert.Nil(t, spy.created[0].CompletedAt)

	require.Len(t, spy.updated, 1, "expected one Update call")
	assert.Equal(t, "done", spy.updated[0].Status)
	require.NotNil(t, spy.updated[0].CompletedAt)

	// Assert hub events: created then updated.
	got := make([]taskhub.Event, 0, 2)
	for len(got) < 2 {
		select {
		case ev := <-events:
			got = append(got, ev)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for hub events, got %d so far", len(got))
		}
	}
	assert.Equal(t, taskhub.KindCreated, got[0].Kind)
	assert.Equal(t, "running", got[0].Task.Status)
	assert.Equal(t, taskhub.KindUpdated, got[1].Kind)
	assert.Equal(t, "done", got[1].Task.Status)
	require.NotNil(t, got[1].Task.CompletedAt)
}
