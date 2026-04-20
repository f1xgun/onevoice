package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// --- mocks -----------------------------------------------------------------

type mockProjectService struct {
	createFunc             func(ctx context.Context, businessID uuid.UUID, input service.CreateProjectInput) (*domain.Project, error)
	getByIDFunc            func(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
	listByBusinessIDFunc   func(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error)
	updateFunc             func(ctx context.Context, businessID, id uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error)
	deleteCascadeFunc      func(ctx context.Context, businessID, id uuid.UUID) (int, int, error)
	countConversationsFunc func(ctx context.Context, businessID, id uuid.UUID) (int, error)
}

func (m *mockProjectService) Create(ctx context.Context, businessID uuid.UUID, input service.CreateProjectInput) (*domain.Project, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, businessID, input)
	}
	return nil, nil
}
func (m *mockProjectService) GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, businessID, id)
	}
	return nil, domain.ErrProjectNotFound
}
func (m *mockProjectService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error) {
	if m.listByBusinessIDFunc != nil {
		return m.listByBusinessIDFunc(ctx, businessID)
	}
	return []domain.Project{}, nil
}
func (m *mockProjectService) Update(ctx context.Context, businessID, id uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, businessID, id, input)
	}
	return nil, nil
}
func (m *mockProjectService) DeleteCascade(ctx context.Context, businessID, id uuid.UUID) (deletedConversations, deletedMessages int, err error) {
	if m.deleteCascadeFunc != nil {
		return m.deleteCascadeFunc(ctx, businessID, id)
	}
	return 0, 0, nil
}
func (m *mockProjectService) CountConversations(ctx context.Context, businessID, id uuid.UUID) (int, error) {
	if m.countConversationsFunc != nil {
		return m.countConversationsFunc(ctx, businessID, id)
	}
	return 0, nil
}

var _ ProjectService = (*mockProjectService)(nil)

// mockProjectBusinessService is a minimal BusinessService used by the handler
// only to resolve userID → businessID. The other methods return zero values.
type mockProjectBusinessService struct {
	getByUserIDFunc func(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
}

func (m *mockProjectBusinessService) Create(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (m *mockProjectBusinessService) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	if m.getByUserIDFunc != nil {
		return m.getByUserIDFunc(ctx, userID)
	}
	return nil, domain.ErrBusinessNotFound
}
func (m *mockProjectBusinessService) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	return nil, nil
}
func (m *mockProjectBusinessService) Update(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (m *mockProjectBusinessService) GetToolApprovals(_ context.Context, _, _ uuid.UUID) (map[string]domain.ToolFloor, error) {
	return map[string]domain.ToolFloor{}, nil
}
func (m *mockProjectBusinessService) UpdateToolApprovals(_ context.Context, _, _ uuid.UUID, _ map[string]domain.ToolFloor) error {
	return nil
}

var _ BusinessService = (*mockProjectBusinessService)(nil)

// --- helpers ---------------------------------------------------------------

// withAuthedUser builds a request with a userID in the auth context and a
// businessService stub that resolves it to the given businessID.
func withAuthedUser(method, path string, body []byte, userID, businessID uuid.UUID) (*http.Request, *mockProjectBusinessService) {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, http.NoBody)
	}
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	r = r.WithContext(ctx)

	bs := &mockProjectBusinessService{
		getByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID}, nil
		},
	}
	return r, bs
}

// chiRouteRequest wraps the handler in a chi.Router so URLParam("id")
// resolves against the URL template. httptest.NewRequest alone does not
// populate chi's route context.
func chiRouteRequest(pattern, method, url string, body []byte, h http.HandlerFunc, userID, businessID uuid.UUID) (*httptest.ResponseRecorder, *mockProjectBusinessService) {
	req, bs := withAuthedUser(method, url, body, userID, businessID)
	router := chi.NewRouter()
	router.Method(method, pattern, h)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, bs
}

// --- tests -----------------------------------------------------------------

func TestProjectHandler_Create_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	ps := &mockProjectService{
		createFunc: func(_ context.Context, bid uuid.UUID, input service.CreateProjectInput) (*domain.Project, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, "Reviews", input.Name)
			assert.Equal(t, domain.WhitelistModeAll, input.WhitelistMode)
			return &domain.Project{
				ID:            projectID,
				BusinessID:    bid,
				Name:          input.Name,
				WhitelistMode: input.WhitelistMode,
				AllowedTools:  []string{},
				QuickActions:  []string{},
			}, nil
		},
	}

	body, _ := json.Marshal(map[string]any{
		"name":          "Reviews",
		"whitelistMode": "all",
	})
	req, bs := withAuthedUser(http.MethodPost, "/api/v1/projects", body, userID, businessID)
	h, err := NewProjectHandler(ps, bs)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var got domain.Project
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, projectID, got.ID)
	assert.Equal(t, businessID, got.BusinessID)
	assert.Equal(t, "Reviews", got.Name)
}

func TestProjectHandler_Create_ValidationErrors(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	tests := []struct {
		name          string
		body          map[string]any
		serviceErr    error
		expectStatus  int
		expectMessage string
	}{
		{
			name:          "empty name",
			body:          map[string]any{"name": "", "whitelistMode": "all"},
			serviceErr:    domain.ErrProjectNameRequired,
			expectStatus:  http.StatusBadRequest,
			expectMessage: "project name required",
		},
		{
			name:          "empty explicit whitelist",
			body:          map[string]any{"name": "x", "whitelistMode": "explicit", "allowedTools": []string{}},
			serviceErr:    domain.ErrProjectWhitelistEmpty,
			expectStatus:  http.StatusBadRequest,
			expectMessage: "explicit whitelist must contain at least one tool",
		},
		{
			name:          "invalid mode",
			body:          map[string]any{"name": "x", "whitelistMode": "bogus"},
			serviceErr:    domain.ErrProjectWhitelistMode,
			expectStatus:  http.StatusBadRequest,
			expectMessage: "invalid whitelist mode",
		},
		{
			name:          "duplicate name",
			body:          map[string]any{"name": "Dup", "whitelistMode": "all"},
			serviceErr:    domain.ErrProjectExists,
			expectStatus:  http.StatusConflict,
			expectMessage: "project already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := &mockProjectService{
				createFunc: func(_ context.Context, _ uuid.UUID, _ service.CreateProjectInput) (*domain.Project, error) {
					return nil, tt.serviceErr
				},
			}

			body, _ := json.Marshal(tt.body)
			req, bs := withAuthedUser(http.MethodPost, "/api/v1/projects", body, userID, businessID)
			h, err := NewProjectHandler(ps, bs)
			require.NoError(t, err)

			rec := httptest.NewRecorder()
			h.Create(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			var resp ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, tt.expectMessage, resp.Error)
		})
	}
}

func TestProjectHandler_List_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	ps := &mockProjectService{
		listByBusinessIDFunc: func(_ context.Context, bid uuid.UUID) ([]domain.Project, error) {
			assert.Equal(t, businessID, bid)
			return []domain.Project{
				{ID: uuid.New(), BusinessID: bid, Name: "P1"},
				{ID: uuid.New(), BusinessID: bid, Name: "P2"},
			}, nil
		},
	}

	req, bs := withAuthedUser(http.MethodGet, "/api/v1/projects", nil, userID, businessID)
	h, err := NewProjectHandler(ps, bs)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	h.List(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var projects []domain.Project
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &projects))
	assert.Len(t, projects, 2)
}

func TestProjectHandler_Get_CrossBusinessReturns404(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	ps := &mockProjectService{
		getByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			// Service returns ErrProjectNotFound for cross-business access.
			return nil, domain.ErrProjectNotFound
		},
	}

	path := "/api/v1/projects/" + projectID.String()
	h, err := NewProjectHandler(ps, &mockProjectBusinessService{
		getByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID}, nil
		},
	})
	require.NoError(t, err)

	rec, _ := chiRouteRequest("/api/v1/projects/{id}", http.MethodGet, path, nil, h.Get, userID, businessID)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "project not found", resp.Error)
}

func TestProjectHandler_Update_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	ps := &mockProjectService{
		updateFunc: func(_ context.Context, bid uuid.UUID, id uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, projectID, id)
			assert.Equal(t, "NewName", input.Name)
			return &domain.Project{ID: id, BusinessID: bid, Name: input.Name, WhitelistMode: input.WhitelistMode}, nil
		},
	}

	body, _ := json.Marshal(map[string]any{
		"name":          "NewName",
		"whitelistMode": "all",
	})
	path := "/api/v1/projects/" + projectID.String()
	h, err := NewProjectHandler(ps, &mockProjectBusinessService{
		getByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID}, nil
		},
	})
	require.NoError(t, err)

	rec, _ := chiRouteRequest("/api/v1/projects/{id}", http.MethodPut, path, body, h.Update, userID, businessID)

	assert.Equal(t, http.StatusOK, rec.Code)
	var got domain.Project
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "NewName", got.Name)
}

func TestProjectHandler_Delete_ReturnsCounts(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	ps := &mockProjectService{
		deleteCascadeFunc: func(_ context.Context, _, _ uuid.UUID) (int, int, error) {
			return 5, 42, nil
		},
	}

	path := "/api/v1/projects/" + projectID.String()
	h, err := NewProjectHandler(ps, &mockProjectBusinessService{
		getByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID}, nil
		},
	})
	require.NoError(t, err)

	rec, _ := chiRouteRequest("/api/v1/projects/{id}", http.MethodDelete, path, nil, h.Delete, userID, businessID)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]int
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 5, resp["deletedConversations"])
	assert.Equal(t, 42, resp["deletedMessages"])
}

func TestProjectHandler_ConversationCount(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	ps := &mockProjectService{
		countConversationsFunc: func(_ context.Context, _, _ uuid.UUID) (int, error) {
			return 7, nil
		},
	}

	path := "/api/v1/projects/" + projectID.String() + "/conversation-count"
	h, err := NewProjectHandler(ps, &mockProjectBusinessService{
		getByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID}, nil
		},
	})
	require.NoError(t, err)

	rec, _ := chiRouteRequest("/api/v1/projects/{id}/conversation-count", http.MethodGet, path, nil, h.ConversationCount, userID, businessID)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]int
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 7, resp["count"])
}

func TestProjectHandler_Endpoints_Require401WhenJWTMissing(t *testing.T) {
	// No userID in context -> 401 from middleware.GetUserID path.
	ps := &mockProjectService{}
	bs := &mockProjectBusinessService{}
	h, err := NewProjectHandler(ps, bs)
	require.NoError(t, err)

	endpoints := []struct {
		name    string
		method  string
		path    string
		pattern string
		handler http.HandlerFunc
	}{
		{"list", http.MethodGet, "/api/v1/projects", "/api/v1/projects", h.List},
		{"create", http.MethodPost, "/api/v1/projects", "/api/v1/projects", h.Create},
		{"get", http.MethodGet, "/api/v1/projects/" + uuid.New().String(), "/api/v1/projects/{id}", h.Get},
		{"update", http.MethodPut, "/api/v1/projects/" + uuid.New().String(), "/api/v1/projects/{id}", h.Update},
		{"delete", http.MethodDelete, "/api/v1/projects/" + uuid.New().String(), "/api/v1/projects/{id}", h.Delete},
		{"count", http.MethodGet, "/api/v1/projects/" + uuid.New().String() + "/conversation-count", "/api/v1/projects/{id}/conversation-count", h.ConversationCount},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			var body []byte
			if ep.method == http.MethodPost || ep.method == http.MethodPut {
				body = []byte(`{}`)
			}
			var req *http.Request
			if body != nil {
				req = httptest.NewRequest(ep.method, ep.path, bytes.NewReader(body))
			} else {
				req = httptest.NewRequest(ep.method, ep.path, http.NoBody)
			}
			// No userID in context intentionally.

			router := chi.NewRouter()
			router.Method(ep.method, ep.pattern, ep.handler)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code, "endpoint %s should require auth", ep.name)
		})
	}
}

func TestProjectHandler_InvalidUUID_Returns400(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	ps := &mockProjectService{}
	h, err := NewProjectHandler(ps, &mockProjectBusinessService{
		getByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID}, nil
		},
	})
	require.NoError(t, err)

	path := "/api/v1/projects/not-a-uuid"
	rec, _ := chiRouteRequest("/api/v1/projects/{id}", http.MethodGet, path, nil, h.Get, userID, businessID)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid project id", resp.Error)
}

func TestProjectHandler_Create_InvalidBody_Returns400(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	ps := &mockProjectService{}
	req, bs := withAuthedUser(http.MethodPost, "/api/v1/projects", []byte("not-json"), userID, businessID)
	h, err := NewProjectHandler(ps, bs)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid request body", resp.Error)
}

func TestNewProjectHandler_NilArgs(t *testing.T) {
	_, err := NewProjectHandler(nil, &mockProjectBusinessService{})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "projectService"))

	_, err = NewProjectHandler(&mockProjectService{}, nil)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "businessService"))
}
