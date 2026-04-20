package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ptr is a helper for building *string literals in test tables.
func ptr[T any](v T) *T { return &v }

// MockConversationRepository is a mock implementation of ConversationRepository for testing
type MockConversationRepository struct {
	CreateFunc                  func(ctx context.Context, conv *domain.Conversation) error
	GetByIDFunc                 func(ctx context.Context, id string) (*domain.Conversation, error)
	ListByUserIDFunc            func(ctx context.Context, userID string, limit, offset int) ([]domain.Conversation, error)
	UpdateFunc                  func(ctx context.Context, conv *domain.Conversation) error
	DeleteFunc                  func(ctx context.Context, id string) error
	UpdateProjectAssignmentFunc func(ctx context.Context, id string, projectID *string) error
}

func (m *MockConversationRepository) Create(ctx context.Context, conv *domain.Conversation) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, conv)
	}
	return nil
}

func (m *MockConversationRepository) GetByID(ctx context.Context, id string) (*domain.Conversation, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(ctx, id)
	}
	return nil, domain.ErrConversationNotFound
}

func (m *MockConversationRepository) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Conversation, error) {
	if m.ListByUserIDFunc != nil {
		return m.ListByUserIDFunc(ctx, userID, limit, offset)
	}
	return []domain.Conversation{}, nil
}

func (m *MockConversationRepository) Update(ctx context.Context, conv *domain.Conversation) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, conv)
	}
	return nil
}

func (m *MockConversationRepository) Delete(ctx context.Context, id string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, id)
	}
	return nil
}

func (m *MockConversationRepository) UpdateProjectAssignment(ctx context.Context, id string, projectID *string) error {
	if m.UpdateProjectAssignmentFunc != nil {
		return m.UpdateProjectAssignmentFunc(ctx, id, projectID)
	}
	return nil
}

// MockPendingToolCallRepository is a minimal test double for Phase 16's
// PendingToolCallRepository. Only the methods actually called by the handler
// under test need a *Func field; others return nil / empty slices.
type MockPendingToolCallRepository struct {
	ListPendingByConversationFunc func(ctx context.Context, conversationID string) ([]*domain.PendingToolCallBatch, error)
	GetByBatchIDFunc              func(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error)
}

func (m *MockPendingToolCallRepository) InsertPreparing(_ context.Context, _ *domain.PendingToolCallBatch) error {
	return nil
}
func (m *MockPendingToolCallRepository) PromoteToPending(_ context.Context, _ string) error {
	return nil
}
func (m *MockPendingToolCallRepository) GetByBatchID(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	if m.GetByBatchIDFunc != nil {
		return m.GetByBatchIDFunc(ctx, batchID)
	}
	return nil, domain.ErrBatchNotFound
}
func (m *MockPendingToolCallRepository) ListPendingByConversation(ctx context.Context, conversationID string) ([]*domain.PendingToolCallBatch, error) {
	if m.ListPendingByConversationFunc != nil {
		return m.ListPendingByConversationFunc(ctx, conversationID)
	}
	return nil, nil
}
func (m *MockPendingToolCallRepository) AtomicTransitionToResolving(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return nil, domain.ErrBatchNotFound
}
func (m *MockPendingToolCallRepository) RecordDecisions(_ context.Context, _ string, _ []domain.PendingCall) error {
	return nil
}
func (m *MockPendingToolCallRepository) MarkDispatched(_ context.Context, _, _ string) error {
	return nil
}
func (m *MockPendingToolCallRepository) MarkResolved(_ context.Context, _ string) error { return nil }
func (m *MockPendingToolCallRepository) MarkExpired(_ context.Context, _ string) error  { return nil }
func (m *MockPendingToolCallRepository) ReconcileOrphanPreparing(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// MockMessageRepository is a minimal mock for MessageRepository. Phase 16
// extends the interface with Update + FindByConversationActive; tests that
// don't exercise those paths leave the *Func fields nil and the mock returns
// safe defaults (nil / ErrMessageNotFound).
type MockMessageRepository struct {
	CreateFunc                   func(ctx context.Context, msg *domain.Message) error
	ListByConversationIDFunc     func(ctx context.Context, conversationID string, limit, offset int) ([]domain.Message, error)
	UpdateFunc                   func(ctx context.Context, msg *domain.Message) error
	FindByConversationActiveFunc func(ctx context.Context, conversationID string) (*domain.Message, error)
}

func (m *MockMessageRepository) Create(ctx context.Context, msg *domain.Message) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, msg)
	}
	return nil
}
func (m *MockMessageRepository) ListByConversationID(ctx context.Context, convID string, limit, offset int) ([]domain.Message, error) {
	if m.ListByConversationIDFunc != nil {
		return m.ListByConversationIDFunc(ctx, convID, limit, offset)
	}
	return []domain.Message{}, nil
}
func (m *MockMessageRepository) CountByConversationID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (m *MockMessageRepository) Update(ctx context.Context, msg *domain.Message) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, msg)
	}
	return nil
}
func (m *MockMessageRepository) FindByConversationActive(ctx context.Context, conversationID string) (*domain.Message, error) {
	if m.FindByConversationActiveFunc != nil {
		return m.FindByConversationActiveFunc(ctx, conversationID)
	}
	return nil, domain.ErrMessageNotFound
}

// noopBusinessService returns ErrBusinessNotFound by default. Tests that need
// a populated business override GetByUserIDFunc.
type noopBusinessService struct {
	GetByUserIDFunc func(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
}

func (s *noopBusinessService) Create(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *noopBusinessService) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	if s.GetByUserIDFunc != nil {
		return s.GetByUserIDFunc(ctx, userID)
	}
	return nil, domain.ErrBusinessNotFound
}
func (s *noopBusinessService) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	return nil, domain.ErrBusinessNotFound
}
func (s *noopBusinessService) Update(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *noopBusinessService) GetToolApprovals(_ context.Context, _, _ uuid.UUID) (map[string]domain.ToolFloor, error) {
	return map[string]domain.ToolFloor{}, nil
}
func (s *noopBusinessService) UpdateToolApprovals(_ context.Context, _, _ uuid.UUID, _ map[string]domain.ToolFloor) error {
	return nil
}

// noopProjectService returns ErrProjectNotFound by default. Tests that need
// a populated project override GetByIDFunc.
type noopProjectService struct {
	GetByIDFunc func(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
}

func (s *noopProjectService) Create(_ context.Context, _ uuid.UUID, _ service.CreateProjectInput) (*domain.Project, error) {
	return nil, nil
}
func (s *noopProjectService) GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error) {
	if s.GetByIDFunc != nil {
		return s.GetByIDFunc(ctx, businessID, id)
	}
	return nil, domain.ErrProjectNotFound
}
func (s *noopProjectService) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Project, error) {
	return []domain.Project{}, nil
}
func (s *noopProjectService) Update(_ context.Context, _, _ uuid.UUID, _ service.UpdateProjectInput) (*domain.Project, error) {
	return nil, nil
}
func (s *noopProjectService) DeleteCascade(_ context.Context, _, _ uuid.UUID) (deletedConversations, deletedMessages int, err error) {
	return 0, 0, nil
}
func (s *noopProjectService) CountConversations(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}

// newTestConversationHandler builds a ConversationHandler wired with a stub
// business service that always returns a valid business (so create/move do not
// 404 on the lookup) and a stub project service that returns ErrProjectNotFound
// by default. Tests that need custom behavior call NewConversationHandler
// directly with their own services. Phase 16 injects an empty
// PendingToolCallRepository mock so the HITL-11 pendingApprovals array is
// always serialized as [] for legacy tests.
func newTestConversationHandler(convRepo domain.ConversationRepository, msgRepo domain.MessageRepository) *ConversationHandler {
	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, userID uuid.UUID) (*domain.Business, error) {
			return &domain.Business{
				ID:     uuid.New(),
				UserID: userID,
				Name:   "Test Business",
			}, nil
		},
	}
	h, err := NewConversationHandler(convRepo, msgRepo, biz, &noopProjectService{}, &MockPendingToolCallRepository{})
	if err != nil {
		panic(err)
	}
	return h
}

// TestCreateConversation_Success tests successful conversation creation
func TestCreateConversation_Success(t *testing.T) {
	// Setup
	mockRepo := &MockConversationRepository{
		CreateFunc: func(ctx context.Context, conv *domain.Conversation) error {
			// Verify conversation has required fields
			assert.NotEmpty(t, conv.ID)
			assert.NotEmpty(t, conv.UserID)
			assert.Equal(t, "My New Conversation", conv.Title)
			assert.False(t, conv.CreatedAt.IsZero())
			assert.False(t, conv.UpdatedAt.IsZero())
			return nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	reqBody := CreateConversationRequest{
		Title: "My New Conversation",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

	// Add user ID to context (simulating auth middleware)
	userID := uuid.New()
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusCreated, w.Code)

	var response domain.Conversation
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.NotEmpty(t, response.ID)
	assert.Equal(t, userID.String(), response.UserID)
	assert.Equal(t, "My New Conversation", response.Title)
	assert.False(t, response.CreatedAt.IsZero())
	assert.False(t, response.UpdatedAt.IsZero())
}

// TestCreateConversation_MissingUserID tests creation without user ID in context
func TestCreateConversation_MissingUserID(t *testing.T) {
	// Setup
	mockRepo := &MockConversationRepository{}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request without user ID in context
	reqBody := CreateConversationRequest{
		Title: "My New Conversation",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

	// Execute
	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "unauthorized", response.Error)
}

// TestCreateConversation_ValidationError tests validation errors
func TestCreateConversation_ValidationError(t *testing.T) {
	tests := []struct {
		name          string
		request       CreateConversationRequest
		expectedField string
	}{
		{
			name:          "missing title",
			request:       CreateConversationRequest{Title: ""},
			expectedField: "Title",
		},
		{
			name:          "title too long",
			request:       CreateConversationRequest{Title: strings.Repeat("a", 201)},
			expectedField: "Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := &MockConversationRepository{}
			handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

			// Create request
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

			// Add user ID to context
			userID := uuid.New()
			ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			req = req.WithContext(ctx)

			// Execute
			w := httptest.NewRecorder()
			handler.CreateConversation(w, req)

			// Assert
			assert.Equal(t, http.StatusBadRequest, w.Code)

			var response ValidationErrorResponse
			err := json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)
			assert.Equal(t, "validation failed", response.Error)
			assert.Contains(t, response.Fields, tt.expectedField)
		})
	}
}

// TestCreateConversation_RepositoryError tests repository errors
func TestCreateConversation_RepositoryError(t *testing.T) {
	// Setup
	mockRepo := &MockConversationRepository{
		CreateFunc: func(ctx context.Context, conv *domain.Conversation) error {
			return errors.New("database error")
		},
	}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	reqBody := CreateConversationRequest{
		Title: "My New Conversation",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

	// Add user ID to context
	userID := uuid.New()
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal server error", response.Error)
}

// TestNewConversationHandler_NilRepository tests error on nil repository
func TestNewConversationHandler_NilRepository(t *testing.T) {
	h, err := NewConversationHandler(nil, &MockMessageRepository{}, &noopBusinessService{}, &noopProjectService{}, &MockPendingToolCallRepository{})
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestNewConversationHandler_NilBusinessService ensures the Phase 15 new dep
// is checked.
func TestNewConversationHandler_NilBusinessService(t *testing.T) {
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, nil, &noopProjectService{}, &MockPendingToolCallRepository{})
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestNewConversationHandler_NilProjectService ensures the Phase 15 new dep
// is checked.
func TestNewConversationHandler_NilProjectService(t *testing.T) {
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, &noopBusinessService{}, nil, &MockPendingToolCallRepository{})
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestNewConversationHandler_NilPendingRepo ensures the Phase 16 new dep is
// checked (chat_proxy and GET /messages both rely on it).
func TestNewConversationHandler_NilPendingRepo(t *testing.T) {
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, &noopBusinessService{}, &noopProjectService{}, nil)
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestListConversations_Success tests successful conversation list retrieval
func TestListConversations_Success(t *testing.T) {
	// Setup
	userID := uuid.New()
	conversations := []domain.Conversation{
		{
			ID:        "507f1f77bcf86cd799439011",
			UserID:    userID.String(),
			Title:     "Conversation 1",
			CreatedAt: time.Now().Add(-2 * time.Hour),
			UpdatedAt: time.Now().Add(-2 * time.Hour),
		},
		{
			ID:        "507f1f77bcf86cd799439012",
			UserID:    userID.String(),
			Title:     "Conversation 2",
			CreatedAt: time.Now().Add(-1 * time.Hour),
			UpdatedAt: time.Now().Add(-1 * time.Hour),
		},
	}

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(ctx context.Context, uid string, limit, offset int) ([]domain.Conversation, error) {
			assert.Equal(t, userID.String(), uid)
			assert.Equal(t, 20, limit) // Default limit
			assert.Equal(t, 0, offset) // Default offset
			return conversations, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)

	var response []domain.Conversation
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response, 2)
	assert.Equal(t, "Conversation 1", response[0].Title)
	assert.Equal(t, "Conversation 2", response[1].Title)
}

// TestListConversations_EmptyList tests empty conversation list
func TestListConversations_EmptyList(t *testing.T) {
	// Setup
	userID := uuid.New()

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(ctx context.Context, uid string, limit, offset int) ([]domain.Conversation, error) {
			return []domain.Conversation{}, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)

	var response []domain.Conversation
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response, 0)
	assert.NotNil(t, response) // Should be empty array, not null
}

// TestListConversations_WithQueryParams tests list with limit and offset
func TestListConversations_WithQueryParams(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		expectedLimit  int
		expectedOffset int
	}{
		{
			name:           "custom limit and offset",
			queryParams:    "?limit=10&offset=5",
			expectedLimit:  10,
			expectedOffset: 5,
		},
		{
			name:           "max limit enforced",
			queryParams:    "?limit=200",
			expectedLimit:  100, // Max limit is 100
			expectedOffset: 0,
		},
		{
			name:           "negative values treated as defaults",
			queryParams:    "?limit=-10&offset=-5",
			expectedLimit:  20, // Default limit
			expectedOffset: 0,  // Default offset
		},
		{
			name:           "invalid values treated as defaults",
			queryParams:    "?limit=abc&offset=xyz",
			expectedLimit:  20, // Default limit
			expectedOffset: 0,  // Default offset
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			userID := uuid.New()

			mockRepo := &MockConversationRepository{
				ListByUserIDFunc: func(ctx context.Context, uid string, limit, offset int) ([]domain.Conversation, error) {
					assert.Equal(t, tt.expectedLimit, limit)
					assert.Equal(t, tt.expectedOffset, offset)
					return []domain.Conversation{}, nil
				},
			}

			handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations"+tt.queryParams, http.NoBody)

			// Add user ID to context
			ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			req = req.WithContext(ctx)

			// Execute
			w := httptest.NewRecorder()
			handler.ListConversations(w, req)

			// Assert
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestListConversations_MissingUserID tests list without user ID in context
func TestListConversations_MissingUserID(t *testing.T) {
	// Setup
	mockRepo := &MockConversationRepository{}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request without user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	// Execute
	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "unauthorized", response.Error)
}

// TestListConversations_RepositoryError tests repository errors
func TestListConversations_RepositoryError(t *testing.T) {
	// Setup
	userID := uuid.New()

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(ctx context.Context, uid string, limit, offset int) ([]domain.Conversation, error) {
			return nil, errors.New("database error")
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	// Assert
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal server error", response.Error)
}

// TestGetConversation_Success tests successful conversation retrieval
func TestGetConversation_Success(t *testing.T) {
	// Setup
	userID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	conversation := &domain.Conversation{
		ID:        conversationID,
		UserID:    userID.String(),
		Title:     "Test Conversation",
		CreatedAt: time.Now().Add(-1 * time.Hour),
		UpdatedAt: time.Now().Add(-1 * time.Hour),
	}

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			assert.Equal(t, conversationID, id)
			return conversation, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	// Add chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)

	var response domain.Conversation
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, conversationID, response.ID)
	assert.Equal(t, userID.String(), response.UserID)
	assert.Equal(t, "Test Conversation", response.Title)
}

// TestGetConversation_Unauthorized tests authorization check (different user)
func TestGetConversation_Unauthorized(t *testing.T) {
	// Setup
	userID := uuid.New()
	otherUserID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	conversation := &domain.Conversation{
		ID:        conversationID,
		UserID:    otherUserID.String(), // Different user
		Title:     "Test Conversation",
		CreatedAt: time.Now().Add(-1 * time.Hour),
		UpdatedAt: time.Now().Add(-1 * time.Hour),
	}

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			return conversation, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	// Add chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusForbidden, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "forbidden", response.Error)
}

// TestGetConversation_NotFound tests conversation not found
func TestGetConversation_NotFound(t *testing.T) {
	// Setup
	userID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			return nil, domain.ErrConversationNotFound
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	// Add chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusNotFound, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "conversation not found", response.Error)
}

// TestGetConversation_MissingUserID tests get without user ID in context
func TestGetConversation_MissingUserID(t *testing.T) {
	// Setup
	conversationID := "507f1f77bcf86cd799439011"
	mockRepo := &MockConversationRepository{}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request without user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	// Add chi URL param only
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "unauthorized", response.Error)
}

// TestGetConversation_RepositoryError tests repository errors
func TestGetConversation_RepositoryError(t *testing.T) {
	// Setup
	userID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			return nil, errors.New("database error")
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	// Add user ID to context
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	// Add chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	// Execute
	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	// Assert
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal server error", response.Error)
}

// TestConversation_JSONShape_PopulatedFields asserts that json.Marshal of a
// fully populated domain.Conversation produces the camelCase keys the Phase 15
// frontend (Plan 06 sidebar) relies on for grouping, pinning, and empty-state
// filtering. This is the Plan 15-04 Task 1 JSON-shape contract (five keys
// guaranteed when values are non-zero).
func TestConversation_JSONShape_PopulatedFields(t *testing.T) {
	lastMsg := time.Now().UTC()
	conv := domain.Conversation{
		ID:            "c1",
		UserID:        "u1",
		BusinessID:    "b1",
		ProjectID:     ptr("p1"),
		Title:         "Ошибки после обновления",
		TitleStatus:   domain.TitleStatusAutoPending,
		Pinned:        true,
		LastMessageAt: &lastMsg,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	raw, err := json.Marshal(conv)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	// The five keys Plan 06 sidebar relies on.
	for _, key := range []string{"projectId", "businessId", "pinned", "titleStatus", "lastMessageAt"} {
		_, ok := m[key]
		assert.Truef(t, ok, "expected key %q in JSON shape; got keys: %v", key, keysOf(m))
	}
	assert.Equal(t, "p1", m["projectId"])
	assert.Equal(t, "b1", m["businessId"])
	assert.Equal(t, true, m["pinned"])
	assert.Equal(t, string(domain.TitleStatusAutoPending), m["titleStatus"])
}

// TestConversation_JSONShape_NilProjectIDElided documents that when ProjectID
// is nil, the `json:"projectId,omitempty"` tag elides the key. The frontend
// must treat "missing projectId" as "null / Без проекта" per Plan 15-04.
func TestConversation_JSONShape_NilProjectIDElided(t *testing.T) {
	conv := domain.Conversation{
		ID:          "c2",
		UserID:      "u2",
		BusinessID:  "b2",
		ProjectID:   nil, // virtual "Без проекта" bucket
		Title:       "t",
		TitleStatus: domain.TitleStatusAutoPending,
	}
	raw, err := json.Marshal(conv)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	_, present := m["projectId"]
	assert.False(t, present, "projectId must be elided when ProjectID is nil (omitempty); got: %v", m)
	// businessId/pinned/titleStatus remain present.
	_, ok := m["businessId"]
	assert.True(t, ok)
	_, ok = m["pinned"]
	assert.True(t, ok)
	_, ok = m["titleStatus"]
	assert.True(t, ok)
}

// TestListConversations_JSONShape verifies that GET /api/v1/conversations
// serializes every list item with the five Phase 15 keys the sidebar depends
// on. Nil LastMessageAt is elided (documented as expected).
func TestListConversations_JSONShape(t *testing.T) {
	userID := uuid.New()
	projID := "proj-1"
	lastMsg := time.Now().UTC()

	conversations := []domain.Conversation{
		{
			ID:            "507f1f77bcf86cd799439011",
			UserID:        userID.String(),
			BusinessID:    "biz-1",
			ProjectID:     &projID,
			Title:         "Pinned",
			TitleStatus:   domain.TitleStatusAuto,
			Pinned:        true,
			LastMessageAt: &lastMsg,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	}

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(_ context.Context, _ string, _, _ int) ([]domain.Conversation, error) {
			return conversations, nil
		},
	}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
	require.Len(t, items, 1)

	item := items[0]
	for _, key := range []string{"projectId", "businessId", "pinned", "titleStatus", "lastMessageAt"} {
		_, ok := item[key]
		assert.Truef(t, ok, "GET /api/v1/conversations item must carry key %q; got: %v", key, keysOf(item))
	}
	assert.Equal(t, "biz-1", item["businessId"])
	assert.Equal(t, "proj-1", item["projectId"])
	assert.Equal(t, true, item["pinned"])
	assert.Equal(t, string(domain.TitleStatusAuto), item["titleStatus"])
}

// keysOf returns the keys of m (used only in test failure messages).
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// --- Task 2: CreateConversation with projectId + MoveConversation -----------

// makeAuthedReq builds an *http.Request with userID in context and
// (optionally) a chi URL param {id}. Returns the recorder to write to.
func makeAuthedReq(t *testing.T, method, path string, body []byte, userID uuid.UUID, convID string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, http.NoBody)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	}
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	if convID != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", convID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return r.WithContext(ctx)
}

// TestCreateConversation_WithProjectID covers Behavior 1 from Plan 15-04 Task 2.
func TestCreateConversation_WithProjectID(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	var capturedConv *domain.Conversation
	mockRepo := &MockConversationRepository{
		CreateFunc: func(_ context.Context, conv *domain.Conversation) error {
			capturedConv = conv
			return nil
		},
	}
	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, uid uuid.UUID) (*domain.Business, error) {
			assert.Equal(t, userID, uid)
			return &domain.Business{ID: businessID, UserID: userID, Name: "B"}, nil
		},
	}
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, bizID, id uuid.UUID) (*domain.Project, error) {
			assert.Equal(t, businessID, bizID)
			assert.Equal(t, projectID, id)
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Reviews"}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, biz, proj, &MockPendingToolCallRepository{})
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(CreateConversationRequest{Title: "Chat", ProjectID: &pid})
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations", body, userID, "")
	w := httptest.NewRecorder()
	h.CreateConversation(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, capturedConv)
	require.NotNil(t, capturedConv.ProjectID)
	assert.Equal(t, pid, *capturedConv.ProjectID)
	assert.Equal(t, businessID.String(), capturedConv.BusinessID)
	assert.Equal(t, domain.TitleStatusAutoPending, capturedConv.TitleStatus)
	assert.False(t, capturedConv.Pinned)
}

// TestCreateConversation_NullAndAbsentProjectIDEquivalent covers Behaviors 2 & 3.
// Standard encoding/json semantics: both `"projectId": null` and an absent
// `projectId` key deserialize to *string(nil). Handler must NOT distinguish.
func TestCreateConversation_NullAndAbsentProjectIDEquivalent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"explicit null", `{"title":"x","projectId":null}`},
		{"absent key", `{"title":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			businessID := uuid.New()

			var captured *domain.Conversation
			mockRepo := &MockConversationRepository{
				CreateFunc: func(_ context.Context, conv *domain.Conversation) error {
					captured = conv
					return nil
				},
			}
			biz := &noopBusinessService{
				GetByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
					return &domain.Business{ID: businessID, UserID: userID, Name: "B"}, nil
				},
			}
			h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, biz, &noopProjectService{}, &MockPendingToolCallRepository{})
			require.NoError(t, err)

			req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations", []byte(tc.body), userID, "")
			w := httptest.NewRecorder()
			h.CreateConversation(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
			require.NotNil(t, captured)
			assert.Nil(t, captured.ProjectID, "null and absent projectId must both map to *string(nil)")
			assert.Equal(t, businessID.String(), captured.BusinessID)
		})
	}
}

// TestCreateConversation_ProjectCrossBusiness covers the cross-business guard.
func TestCreateConversation_ProjectCrossBusiness(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()

	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID, UserID: userID}, nil
		},
	}
	// Project belongs to a different business → service returns
	// ErrProjectNotFound (Plan 15-03 anti-enumeration).
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			return nil, domain.ErrProjectNotFound
		},
	}
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, biz, proj, &MockPendingToolCallRepository{})
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(CreateConversationRequest{Title: "x", ProjectID: &pid})
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations", body, userID, "")
	w := httptest.NewRecorder()
	h.CreateConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestMoveConversation_ToProject covers Behavior 4 (move with real destination
// appends the exact Russian system note).
func TestMoveConversation_ToProject(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	convID := "507f1f77bcf86cd799439011"

	convAfterMove := &domain.Conversation{
		ID:         convID,
		UserID:     userID.String(),
		BusinessID: businessID.String(),
		ProjectID:  ptr(projectID.String()),
		Title:      "Moved",
	}

	getByIDCall := 0
	var capturedMsg *domain.Message
	var captureUpdateProjID *string

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			assert.Equal(t, convID, id)
			getByIDCall++
			if getByIDCall == 1 {
				// first call (ownership check) — original state
				return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
			}
			// second call (re-fetch after move)
			return convAfterMove, nil
		},
		UpdateProjectAssignmentFunc: func(_ context.Context, id string, pid *string) error {
			assert.Equal(t, convID, id)
			captureUpdateProjID = pid
			return nil
		},
	}
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			capturedMsg = m
			return nil
		},
	}
	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID, UserID: userID}, nil
		},
	}
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, bid, pid uuid.UUID) (*domain.Project, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, projectID, pid)
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Отзывы"}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, msgRepo, biz, proj, &MockPendingToolCallRepository{})
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(MoveConversationRequest{ProjectID: &pid})
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations/"+convID+"/move", body, userID, convID)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, captureUpdateProjID)
	assert.Equal(t, pid, *captureUpdateProjID)

	require.NotNil(t, capturedMsg, "system note must be appended")
	assert.Equal(t, convID, capturedMsg.ConversationID)
	assert.Equal(t, "system", capturedMsg.Role)
	// Byte-exact Russian copy per 15-UI-SPEC line 194.
	assert.Equal(t, "[Чат перемещён в «Отзывы» — с этого момента применяется новая политика]", capturedMsg.Content)

	// Response body carries the re-fetched conversation.
	var resp domain.Conversation
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.ProjectID)
	assert.Equal(t, projectID.String(), *resp.ProjectID)
}

// TestMoveConversation_ToNullBezProyekta covers Behavior 5 (move to null uses
// "Без проекта" in the system note).
func TestMoveConversation_ToNullBezProyekta(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439012"

	var capturedMsg *domain.Message
	var captureUpdateProjID *string
	getByIDCall := 0

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			getByIDCall++
			if getByIDCall == 1 {
				return &domain.Conversation{ID: convID, UserID: userID.String(), ProjectID: ptr("old-proj")}, nil
			}
			return &domain.Conversation{ID: convID, UserID: userID.String(), ProjectID: nil}, nil
		},
		UpdateProjectAssignmentFunc: func(_ context.Context, _ string, pid *string) error {
			captureUpdateProjID = pid
			return nil
		},
	}
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			capturedMsg = m
			return nil
		},
	}
	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID, UserID: userID}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, msgRepo, biz, &noopProjectService{}, &MockPendingToolCallRepository{})
	require.NoError(t, err)

	// null body
	body := []byte(`{"projectId":null}`)
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations/"+convID+"/move", body, userID, convID)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, captureUpdateProjID, "null projectId must be forwarded as nil to repo")

	require.NotNil(t, capturedMsg)
	assert.Equal(t, "system", capturedMsg.Role)
	assert.Equal(t, "[Чат перемещён в «Без проекта» — с этого момента применяется новая политика]", capturedMsg.Content)
}

// TestMoveConversation_ProjectCrossBusiness covers Behavior 6.
func TestMoveConversation_ProjectCrossBusiness(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	convID := "507f1f77bcf86cd799439013"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String()}, nil
		},
	}
	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: businessID, UserID: userID}, nil
		},
	}
	// Project belongs to a different business → ErrProjectNotFound.
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			return nil, domain.ErrProjectNotFound
		},
	}
	h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, biz, proj, &MockPendingToolCallRepository{})
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(MoveConversationRequest{ProjectID: &pid})
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations/"+convID+"/move", body, userID, convID)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestMoveConversation_MissingConversation covers Behavior 7.
func TestMoveConversation_MissingConversation(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439014"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return nil, domain.ErrConversationNotFound
		},
	}
	h := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body := []byte(`{"projectId":null}`)
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations/"+convID+"/move", body, userID, convID)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestMoveConversation_WrongUser covers Behavior 8.
func TestMoveConversation_WrongUser(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	convID := "507f1f77bcf86cd799439015"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: otherUserID.String()}, nil
		},
	}
	h := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body := []byte(`{"projectId":null}`)
	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations/"+convID+"/move", body, userID, convID)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestMoveConversation_InvalidBody covers malformed-JSON handling.
func TestMoveConversation_InvalidBody(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439016"
	h := newTestConversationHandler(&MockConversationRepository{}, &MockMessageRepository{})

	req := makeAuthedReq(t, http.MethodPost, "/api/v1/conversations/"+convID+"/move", []byte(`not json`), userID, convID)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- Phase 16 Plan 06 Task 2: GET /messages pendingApprovals (HITL-11) ------

// newConversationHandlerWithPending wires a ConversationHandler with a custom
// pending-tool-call repo mock so tests can drive ListPendingByConversation.
func newConversationHandlerWithPending(t *testing.T, convRepo domain.ConversationRepository, msgRepo domain.MessageRepository, pendingRepo domain.PendingToolCallRepository) *ConversationHandler {
	t.Helper()
	biz := &noopBusinessService{
		GetByUserIDFunc: func(_ context.Context, userID uuid.UUID) (*domain.Business, error) {
			return &domain.Business{ID: uuid.New(), UserID: userID, Name: "Biz"}, nil
		},
	}
	h, err := NewConversationHandler(convRepo, msgRepo, biz, &noopProjectService{}, pendingRepo)
	require.NoError(t, err)
	return h
}

// TestGetMessages_NoPendingApprovals_ReturnsEmptyArray covers the default case:
// no active batches → the response serializes `pendingApprovals: []`
// (non-null, empty) so the frontend can iterate unconditionally (HITL-11).
func TestGetMessages_NoPendingApprovals_ReturnsEmptyArray(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439101"

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String()}, nil
		},
	}
	msgRepo := &MockMessageRepository{
		ListByConversationIDFunc: func(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
			return []domain.Message{{ID: "m1", ConversationID: convID, Role: "user", Content: "hi"}}, nil
		},
	}
	pending := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return nil, nil // no active batches
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, msgRepo, pending)

	req := makeAuthedReq(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// Must be an explicit [] not null or missing.
	raw, ok := body["pendingApprovals"]
	require.True(t, ok, "pendingApprovals key must be present; got keys: %v", body)
	assert.Equal(t, "[]", string(raw), "pendingApprovals must serialize as [] when no batches active")
}

// TestGetMessages_WithPendingApprovals_ReturnsPopulatedArray covers the happy
// path: a single pending batch with one manual call surfaces in the response.
func TestGetMessages_WithPendingApprovals_ReturnsPopulatedArray(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439102"
	created := time.Now().UTC().Truncate(time.Second)
	expires := created.Add(24 * time.Hour)

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String()}, nil
		},
	}
	msgRepo := &MockMessageRepository{}
	pending := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, id string) ([]*domain.PendingToolCallBatch, error) {
			assert.Equal(t, convID, id)
			return []*domain.PendingToolCallBatch{
				{
					ID:             "batch-abc",
					ConversationID: convID,
					MessageID:      "msg-42",
					Status:         "pending",
					Calls: []domain.PendingCall{
						{CallID: "toolu_1", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
					},
					CreatedAt: created,
					ExpiresAt: expires,
				},
			}, nil
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, msgRepo, pending)

	req := makeAuthedReq(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Messages         []domain.Message         `json:"messages"`
		PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.PendingApprovals, 1)
	assert.Equal(t, "batch-abc", body.PendingApprovals[0].BatchID)
	assert.Equal(t, "msg-42", body.PendingApprovals[0].MessageID)
	assert.Equal(t, "pending", body.PendingApprovals[0].Status)
	require.Len(t, body.PendingApprovals[0].Calls, 1)
	assert.Equal(t, "toolu_1", body.PendingApprovals[0].Calls[0].CallID)
	assert.Equal(t, "telegram__send_channel_post", body.PendingApprovals[0].Calls[0].ToolName)
	// EditableFields is intentionally empty — Plan 16-06 defers population to
	// the frontend's `['tools']` React Query (Plan 16-08 ships the live map).
	assert.NotNil(t, body.PendingApprovals[0].Calls[0].EditableFields, "EditableFields must be [] not null for stable contract")
}

// TestGetMessages_ExpiredBatch_ReportsExpiredStatus documents the
// lazy-expiration pass: a batch whose expires_at is in the past is reported
// with status="expired" so the UI can render the "Истекло" badge (D-19).
func TestGetMessages_ExpiredBatch_ReportsExpiredStatus(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439103"

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String()}, nil
		},
	}
	pending := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			// Simulate the repo's lazy virtualization: past expires_at with
			// status="pending" is returned as status="expired".
			return []*domain.PendingToolCallBatch{{
				ID:             "batch-old",
				ConversationID: convID,
				Status:         "expired",
				ExpiresAt:      time.Now().Add(-time.Hour),
			}}, nil
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, &MockMessageRepository{}, pending)

	req := makeAuthedReq(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.PendingApprovals, 1)
	assert.Equal(t, "expired", body.PendingApprovals[0].Status)
}

// TestGetMessages_MultiplePendingBatches_AllReturned covers the edge case
// where a resume spawned a second pause (new turn inside a continuation).
func TestGetMessages_MultiplePendingBatches_AllReturned(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439104"

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String()}, nil
		},
	}
	pending := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return []*domain.PendingToolCallBatch{
				{ID: "b1", ConversationID: convID, MessageID: "m1", Status: "pending"},
				{ID: "b2", ConversationID: convID, MessageID: "m2", Status: "resolving"},
			}, nil
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, &MockMessageRepository{}, pending)

	req := makeAuthedReq(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.PendingApprovals, 2)
	assert.Equal(t, "b1", body.PendingApprovals[0].BatchID)
	assert.Equal(t, "pending", body.PendingApprovals[0].Status)
	assert.Equal(t, "b2", body.PendingApprovals[1].BatchID)
	assert.Equal(t, "resolving", body.PendingApprovals[1].Status)
}
