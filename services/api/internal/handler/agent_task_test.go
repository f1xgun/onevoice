package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// mockAgentTaskService implements AgentTaskService for tests.
type mockAgentTaskService struct {
	listFn     func(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
	resolveFn  func(ctx context.Context, userID uuid.UUID) (string, error)
}

func (m *mockAgentTaskService) List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error) {
	return m.listFn(ctx, userID, filter)
}

func (m *mockAgentTaskService) ResolveBusinessID(ctx context.Context, userID uuid.UUID) (string, error) {
	if m.resolveFn == nil {
		return "biz-test", nil
	}
	return m.resolveFn(ctx, userID)
}

func TestNewAgentTaskHandler_NilService(t *testing.T) {
	_, err := NewAgentTaskHandler(nil, taskhub.New())
	require.Error(t, err)
}

func TestListTasks_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, uid uuid.UUID, f domain.TaskFilter) ([]domain.AgentTask, int, error) {
			assert.Equal(t, userID, uid)
			return []domain.AgentTask{
				{ID: "t1", Type: "send_post", Status: "completed", Platform: "telegram"},
				{ID: "t2", Type: "send_post", Status: "pending", Platform: "vk"},
			}, 2, nil
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp TaskListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Tasks, 2)
	assert.Equal(t, 2, resp.Total)
}

func TestListTasks_WithFilters(t *testing.T) {
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.TaskFilter) ([]domain.AgentTask, int, error) {
			assert.Equal(t, "telegram", f.Platform)
			assert.Equal(t, "completed", f.Status)
			assert.Equal(t, "send_post", f.Type)
			assert.Equal(t, 30, f.Limit)
			assert.Equal(t, 5, f.Offset)
			return nil, 0, nil
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?platform=telegram&status=completed&type=send_post&limit=30&offset=5", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListTasks_LimitClamped(t *testing.T) {
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.TaskFilter) ([]domain.AgentTask, int, error) {
			assert.Equal(t, MaxTaskLimit, f.Limit)
			return nil, 0, nil
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?limit=999", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListTasks_Unauthorized(t *testing.T) {
	h, _ := NewAgentTaskHandler(&mockAgentTaskService{}, taskhub.New())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestListTasks_BusinessNotFound(t *testing.T) {
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, _ uuid.UUID, _ domain.TaskFilter) ([]domain.AgentTask, int, error) {
			return nil, 0, domain.ErrBusinessNotFound
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
