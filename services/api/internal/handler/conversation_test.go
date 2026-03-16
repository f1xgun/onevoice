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
)

// MockConversationRepository is a mock implementation of ConversationRepository for testing
type MockConversationRepository struct {
	CreateFunc       func(ctx context.Context, conv *domain.Conversation) error
	GetByIDFunc      func(ctx context.Context, id string) (*domain.Conversation, error)
	ListByUserIDFunc func(ctx context.Context, userID string, limit, offset int) ([]domain.Conversation, error)
	UpdateFunc       func(ctx context.Context, conv *domain.Conversation) error
	DeleteFunc       func(ctx context.Context, id string) error
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

// MockMessageRepository is a minimal mock for MessageRepository
type MockMessageRepository struct{}

func (m *MockMessageRepository) Create(_ context.Context, _ *domain.Message) error { return nil }
func (m *MockMessageRepository) ListByConversationID(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
	return []domain.Message{}, nil
}
func (m *MockMessageRepository) CountByConversationID(_ context.Context, _ string) (int64, error) {
	return 0, nil
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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
			handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
	h, err := NewConversationHandler(nil, &MockMessageRepository{})
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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

			handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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

	handler, _ := NewConversationHandler(mockRepo, &MockMessageRepository{})

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
