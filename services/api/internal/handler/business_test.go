package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// MockBusinessService is a mock implementation of the business service interface
type MockBusinessService struct {
	mock.Mock
}

func (m *MockBusinessService) Create(ctx context.Context, business *domain.Business, ownerUserID uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, business, ownerUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) Update(ctx context.Context, business *domain.Business, actorUserID uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, business, actorUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) UpdateLogoURL(ctx context.Context, businessID uuid.UUID, url string, actorUserID uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, businessID, url, actorUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) UpdateSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}, actorUserID uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, businessID, keys, actorUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) ListMembershipsByUser(ctx context.Context, userID uuid.UUID) ([]service.MembershipSummary, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]service.MembershipSummary), args.Error(1)
}

// Tool-approval stubs. Default behavior: return nil/empty so existing
// tests that don't exercise these paths keep working unchanged.
func (m *MockBusinessService) GetToolApprovals(ctx context.Context, businessID uuid.UUID) (map[string]domain.ToolFloor, error) {
	if !m.hasExpectation("GetToolApprovals") {
		return map[string]domain.ToolFloor{}, nil
	}
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]domain.ToolFloor), args.Error(1)
}

func (m *MockBusinessService) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	if !m.hasExpectation("UpdateToolApprovals") {
		return nil
	}
	args := m.Called(ctx, businessID, approvals)
	return args.Error(0)
}

// hasExpectation reports whether a method has a configured .On() expectation.
// Used so new interface methods don't break existing tests that didn't
// explicitly stub them.
func (m *MockBusinessService) hasExpectation(method string) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method {
			return true
		}
	}
	return false
}

// allToolsCache is a ToolsCache that accepts every tool name, so a decoded
// toolApprovals entry always validates.
type allToolsCache struct{}

func (allToolsCache) Has(string) bool { return true }

// bizPerms returns a BusinessContext with all business + content permissions
// — enough to pass every Can() gate in the business handler.
func bizPerms(businessID, userID uuid.UUID) authz.BusinessContext {
	return authz.BusinessContext{
		BusinessID: businessID,
		UserID:     userID,
		RoleID:     uuid.New(),
		Permissions: []authz.Permission{
			authz.PermBusinessRead,
			authz.PermBusinessUpdate,
			authz.PermContentRead,
			authz.PermContentCreate,
		},
	}
}

// withBizCtx injects both a JWT userID and an authz.BusinessContext into r.
func withBizCtx(r *http.Request, bc authz.BusinessContext) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, bc.UserID)
	ctx = authz.WithBusinessContext(ctx, bc)
	return r.WithContext(ctx)
}

// ----- ListUserBusinesses -----

func TestBusinessHandler_ListUserBusinesses(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	biz1ID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174001")
	biz2ID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174002")
	role1ID := uuid.MustParse("333e4567-e89b-12d3-a456-426614174001")
	now := time.Now()

	t.Run("0 memberships returns 200 with empty array", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("ListMembershipsByUser", mock.Anything, testUserID).
			Return([]service.MembershipSummary{}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.ListUserBusinesses(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got []interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Empty(t, got)
		mockSvc.AssertExpectations(t)
	})

	t.Run("2 memberships returns 200 with len==2 and correct shape", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("ListMembershipsByUser", mock.Anything, testUserID).
			Return([]service.MembershipSummary{
				{BusinessID: biz1ID, BusinessName: "Biz One", RoleID: role1ID, RoleName: "Owner", Status: "active", JoinedAt: now},
				{BusinessID: biz2ID, BusinessName: "Biz Two", RoleID: role1ID, RoleName: "Editor", Status: "active", JoinedAt: now},
			}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.ListUserBusinesses(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got []map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, biz1ID.String(), got[0]["id"])
		assert.Equal(t, "Biz One", got[0]["name"])
		assert.NotNil(t, got[0]["role"])
		assert.Equal(t, "active", got[0]["status"])
		assert.NotEmpty(t, got[0]["joined_at"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("suspended status included in response", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("ListMembershipsByUser", mock.Anything, testUserID).
			Return([]service.MembershipSummary{
				{BusinessID: biz1ID, BusinessName: "Biz One", RoleID: role1ID, RoleName: "Owner", Status: "suspended", JoinedAt: now},
			}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.ListUserBusinesses(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got []map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "suspended", got[0]["status"])
	})

	t.Run("missing JWT context returns 401", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
		w := httptest.NewRecorder()
		h.ListUserBusinesses(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// ----- CreateBusiness -----

func TestBusinessHandler_CreateBusiness(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("happy path returns 201 with created business", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("Create", mock.Anything, mock.MatchedBy(func(b *domain.Business) bool {
			return b.Name == "Acme Corp" && b.Category == "retail"
		}), testUserID).Return(&domain.Business{
			ID:       testBusinessID,
			Name:     "Acme Corp",
			Category: "retail",
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		body := `{"name":"Acme Corp","category":"retail","address":"1 Main St","phone":"+1","website":null,"description":"desc"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/businesses", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.CreateBusiness(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var got domain.Business
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "Acme Corp", got.Name)
		mockSvc.AssertExpectations(t)
	})

	t.Run("empty name returns 400 validation_failed", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		body := `{"name":"","category":"retail"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/businesses", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.CreateBusiness(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Проверка не пройдена")
	})

	t.Run("service error returns 500 internal_server_error", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("Create", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, errors.New("db exploded"))

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		body := `{"name":"Test Biz"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/businesses", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.CreateBusiness(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "internal_server_error")
		mockSvc.AssertExpectations(t)
	})
}

// ----- GetBusiness (refactored to BusinessContextFromCtx) -----

func TestBusinessHandler_GetBusiness(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("happy path returns business", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).
			Return(&domain.Business{
				ID:   testBusinessID,
				Name: "My Coffee Shop",
			}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses/"+testBusinessID.String(), http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetBusiness(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got domain.Business
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "My Coffee Shop", got.Name)
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing PermBusinessRead returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{},
		}
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.GetBusiness(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing BusinessContext returns 500", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		w := httptest.NewRecorder()

		h.GetBusiness(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("business not found returns 404", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).
			Return(nil, domain.ErrBusinessNotFound)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetBusiness(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockSvc.AssertExpectations(t)
	})
}

// ----- UpdateBusiness (refactored) -----

func TestBusinessHandler_UpdateBusiness(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("happy path updates and returns business", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).
			Return(&domain.Business{ID: testBusinessID, Name: "Old Name"}, nil)
		mockSvc.On("Update", mock.Anything, mock.MatchedBy(func(b *domain.Business) bool {
			return b.Name == "New Name"
		}), mock.Anything).Return(&domain.Business{ID: testBusinessID, Name: "New Name"}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		body := `{"name":"New Name"}`
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateBusiness(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		body := `{"name":"New Name"}`
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UpdateBusiness(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing BusinessContext returns 500", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		body := `{"name":"New Name"}`
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UpdateBusiness(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("validation error on empty name returns 400", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		body := `{"name":""}`
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateBusiness(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// ----- UpdateSchedule (refactored) -----

// fakeScheduleSyncer captures SyncBusiness calls for UpdateSchedule tests.
// SyncBusiness runs in a goroutine in the handler, so the test waits on the
// `called` channel before asserting.
type fakeScheduleSyncer struct {
	called chan *domain.Business
}

func (f *fakeScheduleSyncer) SyncBusiness(b *domain.Business) {
	f.called <- b
}

func TestBusinessHandler_UpdateSchedule(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	existing := func() *domain.Business {
		return &domain.Business{
			ID:       testBusinessID,
			Name:     "Cafe",
			Settings: map[string]interface{}{},
		}
	}

	t.Run("persists schedule and specialDates and triggers syncer", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		syncer := &fakeScheduleSyncer{called: make(chan *domain.Business, 1)}

		var captured map[string]interface{}
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
			captured = keys
			return true
		}), mock.Anything).Return(&domain.Business{
			ID:       testBusinessID,
			Name:     "Cafe",
			Settings: map[string]interface{}{},
		}, nil)

		h, err := NewBusinessHandler(mockSvc, syncer, nil)
		require.NoError(t, err)

		body := `{"schedule":[{"day":"mon","open":"09:00","close":"21:00","closed":false}],"specialDates":[{"date":"2026-01-01","closed":true}]}`
		req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(body))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateSchedule(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, captured)
		require.Contains(t, captured, "schedule")
		require.Contains(t, captured, "specialDates")

		select {
		case b := <-syncer.called:
			assert.Equal(t, testBusinessID, b.ID)
		case <-time.After(2 * time.Second):
			t.Fatal("syncer was not called within 2s")
		}
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{"schedule":[]}`))
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UpdateSchedule(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing BusinessContext returns 500", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{"schedule":[]}`))
		w := httptest.NewRecorder()

		h.UpdateSchedule(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("returns 400 on invalid json", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{not json`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()
		h.UpdateSchedule(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("nil syncer is allowed (skip dispatch)", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.Anything, mock.Anything).Return(existing(), nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{"schedule":[]}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()
		h.UpdateSchedule(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ----- UpdateVoiceTone (refactored) -----

func TestBusinessHandler_UpdateVoiceTone(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("happy path persists tones", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		existing := &domain.Business{ID: testBusinessID, Settings: map[string]interface{}{}}
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
			return keys["voiceTone"] != nil
		}), mock.Anything).Return(existing, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/voice-tone", bytes.NewBufferString(`{"tones":["Warm","Friendly"]}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateVoiceTone(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		req := httptest.NewRequest(http.MethodPut, "/voice-tone", bytes.NewBufferString(`{"tones":[]}`))
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UpdateVoiceTone(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// ----- DescriptionTemplate -----

func TestBusinessHandler_GetDescriptionTemplate(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("returns stored template and placeholder list", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).Return(&domain.Business{
			ID:       testBusinessID,
			Settings: map[string]interface{}{"descriptionTemplate": "{name} — {phone}"},
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/description-template", http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			DescriptionTemplate string   `json:"descriptionTemplate"`
			Placeholders        []string `json:"placeholders"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "{name} — {phone}", body.DescriptionTemplate)
		assert.Contains(t, body.Placeholders, "name")
		assert.Contains(t, body.Placeholders, "hours")
	})

	t.Run("returns empty template when unset", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).Return(&domain.Business{
			ID:       testBusinessID,
			Settings: map[string]interface{}{},
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/description-template", http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			DescriptionTemplate string `json:"descriptionTemplate"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "", body.DescriptionTemplate)
	})

	t.Run("missing PermBusinessRead returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessUpdate},
		}
		req := httptest.NewRequest(http.MethodGet, "/description-template", http.NoBody)
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.GetDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestBusinessHandler_UpdateDescriptionTemplate(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("valid template persists and fans out to syncer", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		syncer := &fakeScheduleSyncer{called: make(chan *domain.Business, 1)}

		var captured map[string]interface{}
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
			captured = keys
			return true
		}), mock.Anything).Return(&domain.Business{ID: testBusinessID, Settings: map[string]interface{}{}}, nil)

		h, err := NewBusinessHandler(mockSvc, syncer, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/description-template", bytes.NewBufferString(`{"descriptionTemplate":"{name} · {phone}"}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, captured)
		assert.Equal(t, "{name} · {phone}", captured["descriptionTemplate"])

		select {
		case b := <-syncer.called:
			assert.Equal(t, testBusinessID, b.ID)
		case <-time.After(2 * time.Second):
			t.Fatal("syncer was not called within 2s")
		}
		mockSvc.AssertExpectations(t)
	})

	t.Run("unknown placeholder returns 400 naming the token", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/description-template", bytes.NewBufferString(`{"descriptionTemplate":"{name} {foo}"}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "{foo}")
		mockSvc.AssertNotCalled(t, "UpdateSettingsKeys")
	})

	t.Run("empty string clears override", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		var captured map[string]interface{}
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
			captured = keys
			return true
		}), mock.Anything).Return(&domain.Business{ID: testBusinessID, Settings: map[string]interface{}{}}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/description-template", bytes.NewBufferString(`{"descriptionTemplate":""}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, captured)
		assert.Equal(t, "", captured["descriptionTemplate"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		req := httptest.NewRequest(http.MethodPut, "/description-template", bytes.NewBufferString(`{"descriptionTemplate":"{name}"}`))
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UpdateDescriptionTemplate(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestBusinessHandler_GetVoiceProfile(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("returns stored profile", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).Return(&domain.Business{
			ID:       testBusinessID,
			Settings: map[string]interface{}{"voiceProfile": "Пиши тепло, без эмодзи."},
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/voice-profile", http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetVoiceProfile(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			VoiceProfile string `json:"voiceProfile"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "Пиши тепло, без эмодзи.", body.VoiceProfile)
	})

	t.Run("returns empty profile when unset", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetByID", mock.Anything, testBusinessID).Return(&domain.Business{
			ID:       testBusinessID,
			Settings: map[string]interface{}{},
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/voice-profile", http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetVoiceProfile(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			VoiceProfile string `json:"voiceProfile"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "", body.VoiceProfile)
	})

	t.Run("missing PermBusinessRead returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessUpdate},
		}
		req := httptest.NewRequest(http.MethodGet, "/voice-profile", http.NoBody)
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.GetVoiceProfile(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestBusinessHandler_UpdateVoiceProfile(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("valid profile persists via UpdateSettingsKeys and does not sync", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		syncer := &fakeScheduleSyncer{called: make(chan *domain.Business, 1)}

		var captured map[string]interface{}
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
			captured = keys
			return true
		}), mock.Anything).Return(&domain.Business{ID: testBusinessID, Settings: map[string]interface{}{}}, nil)

		h, err := NewBusinessHandler(mockSvc, syncer, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/voice-profile", bytes.NewBufferString(`{"voiceProfile":"Пиши тепло, без эмодзи."}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateVoiceProfile(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, captured)
		assert.Equal(t, "Пиши тепло, без эмодзи.", captured["voiceProfile"])

		select {
		case <-syncer.called:
			t.Fatal("voice-profile edit must NOT fan out to the platform syncer")
		case <-time.After(200 * time.Millisecond):
		}
		mockSvc.AssertExpectations(t)
	})

	t.Run("empty string clears override", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		var captured map[string]interface{}
		mockSvc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
			captured = keys
			return true
		}), mock.Anything).Return(&domain.Business{ID: testBusinessID, Settings: map[string]interface{}{}}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/voice-profile", bytes.NewBufferString(`{"voiceProfile":""}`))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateVoiceProfile(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, captured)
		assert.Equal(t, "", captured["voiceProfile"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("over-cap profile returns 400 and does not persist", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		oversized := strings.Repeat("я", 401)
		payload := `{"voiceProfile":"` + oversized + `"}`
		req := httptest.NewRequest(http.MethodPut, "/voice-profile", bytes.NewBufferString(payload))
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateVoiceProfile(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockSvc.AssertNotCalled(t, "UpdateSettingsKeys")
	})

	t.Run("missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		req := httptest.NewRequest(http.MethodPut, "/voice-profile", bytes.NewBufferString(`{"voiceProfile":"hi"}`))
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UpdateVoiceProfile(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// ----- UploadLogo (refactored) -----

// mockUploader is a test double for storage.Uploader.
type mockUploader struct {
	mock.Mock
}

func (m *mockUploader) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	body, _ := io.ReadAll(reader)
	args := m.Called(ctx, key, body, size, contentType)
	return args.Error(0)
}

func (m *mockUploader) PublicURL(key string) string {
	args := m.Called(key)
	return args.String(0)
}

func (m *mockUploader) KeyFromPublicURL(url string) string {
	args := m.Called(url)
	return args.String(0)
}

func (m *mockUploader) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *mockUploader) DeletePrefix(ctx context.Context, prefix string) error {
	args := m.Called(ctx, prefix)
	return args.Error(0)
}

// pngMagic is the 8-byte PNG signature — enough for http.DetectContentType to identify image/png.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func buildLogoMultipart(t *testing.T, body []byte) (buf *bytes.Buffer, contentType string) {
	t.Helper()
	buf = &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	fw, err := w.CreateFormFile("logo", "logo.png")
	require.NoError(t, err)
	_, err = fw.Write(body)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf, w.FormDataContentType()
}

func TestBusinessHandler_UploadLogo(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("happy path writes to storage and updates business", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockUp := new(mockUploader)

		existing := &domain.Business{
			ID:        testBusinessID,
			Name:      "Cafe",
			CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		mockSvc.On("GetByID", mock.Anything, testBusinessID).Return(existing, nil)

		prefix := "businesses/" + testBusinessID.String()
		mockUp.On("Upload",
			mock.Anything,
			mock.MatchedBy(func(key string) bool {
				return len(key) >= len(prefix) && key[:len(prefix)] == prefix
			}),
			pngMagic,
			int64(len(pngMagic)),
			"image/png",
		).Return(nil)
		mockUp.On("PublicURL", mock.Anything).Return("/media/businesses/x/logo.png")
		mockUp.On("KeyFromPublicURL", "").Return("")

		mockSvc.On("UpdateLogoURL", mock.Anything, testBusinessID, "/media/businesses/x/logo.png", mock.Anything).Return(&domain.Business{
			ID:      testBusinessID,
			Name:    "Cafe",
			LogoURL: "/media/businesses/x/logo.png",
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, mockUp)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, pngMagic)
		req := httptest.NewRequest(http.MethodPut, "/logo", body)
		req.Header.Set("Content-Type", contentType)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got domain.Business
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "/media/businesses/x/logo.png", got.LogoURL)

		mockSvc.AssertExpectations(t)
		mockUp.AssertExpectations(t)
	})

	t.Run("replacing an existing logo deletes the prior object", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockUp := new(mockUploader)

		const priorURL = "/media/businesses/old/logo-111.png"
		const priorKey = "businesses/old/logo-111.png"
		existing := &domain.Business{
			ID:      testBusinessID,
			Name:    "Cafe",
			LogoURL: priorURL,
		}
		mockSvc.On("GetByID", mock.Anything, testBusinessID).Return(existing, nil)
		mockUp.On("Upload", mock.Anything, mock.Anything, pngMagic, int64(len(pngMagic)), "image/png").Return(nil)
		mockUp.On("PublicURL", mock.Anything).Return("/media/businesses/x/logo-222.png")
		mockUp.On("KeyFromPublicURL", priorURL).Return(priorKey)
		mockUp.On("Delete", mock.Anything, priorKey).Return(nil)
		mockSvc.On("UpdateLogoURL", mock.Anything, testBusinessID, "/media/businesses/x/logo-222.png", mock.Anything).Return(&domain.Business{
			ID:      testBusinessID,
			Name:    "Cafe",
			LogoURL: "/media/businesses/x/logo-222.png",
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, mockUp)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, pngMagic)
		req := httptest.NewRequest(http.MethodPut, "/logo", body)
		req.Header.Set("Content-Type", contentType)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockUp.AssertCalled(t, "Delete", mock.Anything, priorKey)
		mockSvc.AssertExpectations(t)
		mockUp.AssertExpectations(t)
	})

	t.Run("nil storage returns 500", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, pngMagic)
		req := httptest.NewRequest(http.MethodPut, "/logo", body)
		req.Header.Set("Content-Type", contentType)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage unavailable")
	})

	t.Run("missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, new(mockUploader))
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		body, contentType := buildLogoMultipart(t, pngMagic)
		req := httptest.NewRequest(http.MethodPut, "/logo", body)
		req.Header.Set("Content-Type", contentType)
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unsupported mime type rejected", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockUp := new(mockUploader)
		h, err := NewBusinessHandler(mockSvc, nil, mockUp)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, []byte("this is not an image at all"))
		req := httptest.NewRequest(http.MethodPut, "/logo", body)
		req.Header.Set("Content-Type", contentType)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported file type")
		mockUp.AssertNotCalled(t, "Upload", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}

// ----- ToolApprovals (refactored) -----

func TestBusinessHandler_ToolApprovals(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("GetBusinessToolApprovals happy path returns approvals", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("GetToolApprovals", mock.Anything, testBusinessID).
			Return(map[string]domain.ToolFloor{"tool_a": domain.ToolFloorAuto}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/tool-approvals", http.NoBody)
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.GetBusinessToolApprovals(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("GetBusinessToolApprovals missing PermBusinessRead returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessUpdate},
		}
		req := httptest.NewRequest(http.MethodGet, "/tool-approvals", http.NoBody)
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.GetBusinessToolApprovals(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("UpdateBusinessToolApprovals missing PermBusinessUpdate returns 403", func(t *testing.T) {
		h, err := NewBusinessHandler(new(MockBusinessService), nil, nil)
		require.NoError(t, err)

		bc := authz.BusinessContext{
			BusinessID:  testBusinessID,
			Permissions: []authz.Permission{authz.PermBusinessRead},
		}
		body := `{"toolApprovals":{"tool_a":"auto"}}`
		req := httptest.NewRequest(http.MethodPut, "/tool-approvals", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bc)
		w := httptest.NewRecorder()

		h.UpdateBusinessToolApprovals(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// ----- Body / field-size hardening -----

func TestBusinessHandler_BodyAndFieldLimits(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	// over-long Name (>200) must be rejected by the max= validator before any
	// service call. Revert the `max=200` on UpdateBusinessRequest.Name and this
	// flips to 200 (the over-long value reaches Update).
	t.Run("CreateBusiness rejects over-long name with 400", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		longName := strings.Repeat("a", 201)
		body := `{"name":"` + longName + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/businesses", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.CreateBusiness(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockSvc.AssertNotCalled(t, "Create")
	})

	// over-long Description (>2000) must be rejected. Revert the
	// `max=2000` on Description → flips to 200 (Update gets the bloated value).
	t.Run("UpdateBusiness rejects over-long description with 400", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		longDesc := strings.Repeat("b", 2001)
		body := `{"name":"Valid Name","description":"` + longDesc + `"}`
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateBusiness(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockSvc.AssertNotCalled(t, "GetByID")
		mockSvc.AssertNotCalled(t, "Update")
	})

	// over-large body must be rejected by MaxBytesReader while the decoder scans
	// the object. The bulk lives in an unknown field (so the decoder must read
	// past maxBusinessBodyBytes to finish the value, yet no per-field validator
	// catches it) — a pass here requires the byte cap, not the field validators.
	// Revert the `r.Body = http.MaxBytesReader(...)` line in UpdateBusiness and
	// this flips to a 200 (the oversized body decodes and Update runs).
	t.Run("UpdateBusiness rejects over-large body with 400", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		// Stubbed so the fail-on-revert path (cap removed) yields a clean 200
		// rather than a mock panic — the byte cap is what keeps it at 400.
		mockSvc.On("GetByID", mock.Anything, testBusinessID).
			Return(&domain.Business{ID: testBusinessID, Name: "Valid Name"}, nil)
		mockSvc.On("Update", mock.Anything, mock.Anything, mock.Anything).
			Return(&domain.Business{ID: testBusinessID, Name: "Valid Name"}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		filler := strings.Repeat("z", maxBusinessBodyBytes+1)
		body := `{"name":"Valid Name","_pad":"` + filler + `"}`
		req := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateBusiness(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockSvc.AssertNotCalled(t, "Update")
	})

	// over-large body must be rejected by MaxBytesReader before the decoder
	// finishes the object. The bulk lives in an unknown field while a valid
	// toolApprovals entry keeps the fail-on-revert path clean: remove the
	// `r.Body = http.MaxBytesReader(...)` line in UpdateBusinessToolApprovals
	// and the oversized body decodes, the known tool passes, and the handler
	// returns 200 (test fails). Restore → 400.
	t.Run("UpdateBusinessToolApprovals rejects over-large body with 400", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockSvc.On("UpdateToolApprovals", mock.Anything, testBusinessID, mock.Anything).
			Return(nil)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)
		h.SetToolsCache(allToolsCache{})

		filler := strings.Repeat("z", maxBusinessBodyBytes+1)
		body := `{"_pad":"` + filler + `","toolApprovals":{"telegram__send_channel_post":"auto"}}`
		req := httptest.NewRequest(http.MethodPut, "/tool-approvals", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateBusinessToolApprovals(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockSvc.AssertNotCalled(t, "UpdateToolApprovals")
	})

	// over-large schedule blob must be rejected by the settings cap. Revert the
	// settingsBlobWithinCap guard in UpdateSchedule and this flips to 200
	// (the multi-KB blob is persisted into Settings).
	t.Run("UpdateSchedule rejects over-large schedule blob with 400", func(t *testing.T) {
		mockSvc := new(MockBusinessService)

		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		bigString := strings.Repeat("x", maxSettingsBlobBytes+1)
		body := `{"schedule":{"note":"` + bigString + `"}}`
		req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
		w := httptest.NewRecorder()

		h.UpdateSchedule(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockSvc.AssertNotCalled(t, "UpdateSettingsKeys")
	})
}
