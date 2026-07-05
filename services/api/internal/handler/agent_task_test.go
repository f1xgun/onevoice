package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// mockAgentTaskService implements AgentTaskService for tests.
type mockAgentTaskService struct {
	listFn  func(ctx context.Context, businessID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
	retryFn func(ctx context.Context, businessID uuid.UUID, taskID string) (*domain.AgentTask, error)
}

func (m *mockAgentTaskService) List(ctx context.Context, businessID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error) {
	return m.listFn(ctx, businessID, filter)
}

func (m *mockAgentTaskService) Retry(ctx context.Context, businessID uuid.UUID, taskID string) (*domain.AgentTask, error) {
	return m.retryFn(ctx, businessID, taskID)
}

// agentTaskBizCtx seeds a BusinessContext with PermContentRead for agent task handler tests.
func agentTaskBizCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead},
	})
}

func TestNewAgentTaskHandler_NilService(t *testing.T) {
	_, err := NewAgentTaskHandler(nil, taskhub.New())
	require.Error(t, err)
}

func TestListTasks_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, bid uuid.UUID, f domain.TaskFilter) ([]domain.AgentTask, int, error) {
			assert.Equal(t, businessID, bid)
			return []domain.AgentTask{
				{ID: "t1", Type: "send_post", Status: "completed", Platform: "telegram"},
				{ID: "t2", Type: "send_post", Status: "pending", Platform: "vk"},
			}, 2, nil
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	req = req.WithContext(agentTaskBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.AgentTaskListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Tasks, 2)
	assert.Equal(t, 2, resp.Total)
}

func TestListTasks_WithFilters(t *testing.T) {
	businessID := uuid.New()
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
	req = req.WithContext(agentTaskBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListTasks_LimitClamped(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.TaskFilter) ([]domain.AgentTask, int, error) {
			assert.Equal(t, MaxTaskLimit, f.Limit)
			return nil, 0, nil
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?limit=999", http.NoBody)
	req = req.WithContext(agentTaskBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListTasks_NoBusinessContext(t *testing.T) {
	h, _ := NewAgentTaskHandler(&mockAgentTaskService{}, taskhub.New())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestListTasks_BusinessNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{
		listFn: func(_ context.Context, _ uuid.UUID, _ domain.TaskFilter) ([]domain.AgentTask, int, error) {
			return nil, 0, domain.ErrBusinessNotFound
		},
	}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	req = req.WithContext(agentTaskBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListTasks(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// retryTaskCtx seeds a BusinessContext with the given permissions and the
// {taskId} chi URL param for RetryTask handler tests.
func retryTaskCtx(businessID, userID uuid.UUID, taskID string, perms ...authz.Permission) context.Context {
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: perms,
	})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

func retryRequest(t *testing.T, businessID, userID uuid.UUID, taskID string, perms ...authz.Permission) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/retry", http.NoBody)
	return req.WithContext(retryTaskCtx(businessID, userID, taskID, perms...))
}

// TestRetryTask_ForbiddenWithoutWritePerm asserts a read-only viewer cannot
// trigger a retry (it drives external platform work). Reverting the
// PermContentUpdate gate to PermContentRead fails this test.
func TestRetryTask_ForbiddenWithoutWritePerm(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{retryFn: func(context.Context, uuid.UUID, string) (*domain.AgentTask, error) {
		t.Fatal("service must not be called without write permission")
		return nil, nil
	}}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	rr := httptest.NewRecorder()
	h.RetryTask(rr, retryRequest(t, businessID, userID, "task-1", authz.PermContentRead))

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRetryTask_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{retryFn: func(_ context.Context, bid uuid.UUID, id string) (*domain.AgentTask, error) {
		assert.Equal(t, businessID, bid)
		assert.Equal(t, "task-1", id)
		return &domain.AgentTask{ID: id, BusinessID: businessID.String(), Platform: "telegram", Type: "send_channel_post", Status: "done"}, nil
	}}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	rr := httptest.NewRecorder()
	h.RetryTask(rr, retryRequest(t, businessID, userID, "task-1", authz.PermContentUpdate))

	assert.Equal(t, http.StatusOK, rr.Code)
	var got openapi.AgentTask
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, "done", got.Status)
}

func TestRetryTask_NotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{retryFn: func(context.Context, uuid.UUID, string) (*domain.AgentTask, error) {
		return nil, domain.ErrAgentTaskNotFound
	}}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	rr := httptest.NewRecorder()
	h.RetryTask(rr, retryRequest(t, businessID, userID, "missing", authz.PermContentUpdate))

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestRetryTask_NonFailedConflict(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{retryFn: func(context.Context, uuid.UUID, string) (*domain.AgentTask, error) {
		return nil, domain.ErrAgentTaskNotFailed
	}}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	rr := httptest.NewRecorder()
	h.RetryTask(rr, retryRequest(t, businessID, userID, "task-1", authz.PermContentUpdate))

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.Contains(t, rr.Body.String(), "task_not_failed")
}

// TestRetryTask_ReconnectReason asserts a token-invalid rejection surfaces the
// reconnect reason code so the FE can route the user to reconnect the
// integration rather than showing a generic failure.
func TestRetryTask_ReconnectReason(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{retryFn: func(context.Context, uuid.UUID, string) (*domain.AgentTask, error) {
		return nil, &service.RetryRejectedError{Reason: service.RetryReasonReconnect}
	}}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	rr := httptest.NewRecorder()
	h.RetryTask(rr, retryRequest(t, businessID, userID, "task-1", authz.PermContentUpdate))

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.Contains(t, rr.Body.String(), string(service.RetryReasonReconnect))
}

// TestRetryTask_PermanentReason asserts a permanent rejection surfaces the
// not-retryable reason code.
func TestRetryTask_PermanentReason(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockAgentTaskService{retryFn: func(context.Context, uuid.UUID, string) (*domain.AgentTask, error) {
		return nil, &service.RetryRejectedError{Reason: service.RetryReasonPermanent}
	}}
	h, _ := NewAgentTaskHandler(svc, taskhub.New())

	rr := httptest.NewRecorder()
	h.RetryTask(rr, retryRequest(t, businessID, userID, "task-1", authz.PermContentUpdate))

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.Contains(t, rr.Body.String(), string(service.RetryReasonPermanent))
}
