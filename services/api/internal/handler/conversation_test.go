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
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ptr is a helper for building *string literals in test tables.
func ptr[T any](v T) *T { return &v }

// MockConversationRepository is a mock implementation of ConversationRepository for testing
type MockConversationRepository struct {
	CreateFunc                  func(ctx context.Context, conv *domain.Conversation) error
	GetByIDFunc                 func(ctx context.Context, id string) (*domain.Conversation, error)
	ListByUserIDFunc            func(ctx context.Context, userID, businessID string, limit, offset int) ([]domain.Conversation, error)
	UpdateFunc                  func(ctx context.Context, conv *domain.Conversation) error
	DeleteFunc                  func(ctx context.Context, id string) error
	UpdateProjectAssignmentFunc func(ctx context.Context, id string, projectID *string) error
	UpdateTitleIfPendingFunc    func(ctx context.Context, id, title string) error
	TransitionToAutoPendingFunc func(ctx context.Context, id string) error
	// atomic Pin/Unpin.
	PinFunc               func(ctx context.Context, id, businessID, userID string) error
	UnpinFunc             func(ctx context.Context, id, businessID, userID string) error
	BumpLastMessageAtFunc func(ctx context.Context, id string, ts time.Time) error
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

func (m *MockConversationRepository) ListByUserID(ctx context.Context, userID, businessID string, limit, offset int) ([]domain.Conversation, error) {
	if m.ListByUserIDFunc != nil {
		return m.ListByUserIDFunc(ctx, userID, businessID, limit, offset)
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

func (m *MockConversationRepository) UpdateTitleIfPending(ctx context.Context, id, title string) error {
	if m.UpdateTitleIfPendingFunc != nil {
		return m.UpdateTitleIfPendingFunc(ctx, id, title)
	}
	return nil
}

func (m *MockConversationRepository) TransitionToAutoPending(ctx context.Context, id string) error {
	if m.TransitionToAutoPendingFunc != nil {
		return m.TransitionToAutoPendingFunc(ctx, id)
	}
	return nil
}

// Pin / Unpin atomic conditional updates.
// Default returns nil so unrelated tests stay green; real Pin/Unpin tests
// install per-call PinFunc / UnpinFunc.
func (m *MockConversationRepository) Pin(ctx context.Context, id, businessID, userID string) error {
	if m.PinFunc != nil {
		return m.PinFunc(ctx, id, businessID, userID)
	}
	return nil
}

func (m *MockConversationRepository) Unpin(ctx context.Context, id, businessID, userID string) error {
	if m.UnpinFunc != nil {
		return m.UnpinFunc(ctx, id, businessID, userID)
	}
	return nil
}

func (m *MockConversationRepository) BumpLastMessageAt(ctx context.Context, id string, ts time.Time) error {
	if m.BumpLastMessageAtFunc != nil {
		return m.BumpLastMessageAtFunc(ctx, id, ts)
	}
	return nil
}

// SearchTitles / ScopedConversationIDs stubs. The conversation handler
// under test never calls these (search is owned by SearchHandler);
// the methods exist solely so MockConversationRepository continues to
// satisfy domain.ConversationRepository after the interface extension.
// Test files exercising the search path use a dedicated fake in
// services/api/internal/service/search_test.go.
func (m *MockConversationRepository) SearchTitles(_ context.Context, _, _, _ string, _ *string, _ int) ([]domain.ConversationTitleHit, []string, error) {
	return nil, nil, nil
}

func (m *MockConversationRepository) ScopedConversationIDs(_ context.Context, _, _ string, _ *string) ([]string, error) {
	return nil, nil
}

// MongoConversationsCleanup stub —. Handler tests don't
// exercise the hard-delete sweeper path so the no-op is sufficient.
func (m *MockConversationRepository) MongoConversationsCleanup(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *MockConversationRepository) MongoBusinessCleanup(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// MockPendingToolCallRepository is a minimal test double for the
// PendingToolCallRepository. Only the methods actually called by the handler
// under test need a *Func field; others return nil / empty slices.
type MockPendingToolCallRepository struct {
	ListPendingByConversationFunc func(ctx context.Context, conversationID string) ([]*domain.PendingToolCallBatch, error)
	GetByBatchIDFunc              func(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error)
}

func (m *MockPendingToolCallRepository) Persist(_ context.Context, _ *domain.PendingToolCallBatch) error {
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
func (m *MockPendingToolCallRepository) AtomicTransitionResolvingToResuming(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	if m.GetByBatchIDFunc != nil {
		b, err := m.GetByBatchIDFunc(ctx, batchID)
		if err != nil {
			return nil, err
		}
		if b == nil || b.Status != "resolving" {
			return nil, domain.ErrBatchNotResolving
		}
		return b, nil
	}
	return nil, domain.ErrBatchNotFound
}
func (m *MockPendingToolCallRepository) ResetResolvingToPending(_ context.Context, _ string) error {
	return nil
}
func (m *MockPendingToolCallRepository) AtomicTransitionResumingToResolving(_ context.Context, _ string) error {
	return nil
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
func (m *MockPendingToolCallRepository) ReconcileOrphanResolving(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// MockMessageRepository is a minimal mock for MessageRepository.
// The interface includes Update + FindByConversationActive; tests that
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
func (m *MockMessageRepository) DeleteByConversationID(_ context.Context, _ string) (int64, error) {
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

// SearchByConversationIDs stub. The conversation handler under test never
// calls this (search is owned by SearchHandler); the method exists solely
// so MockMessageRepository continues to satisfy domain.MessageRepository
// after the interface extension.
func (m *MockMessageRepository) SearchByConversationIDs(_ context.Context, _ string, _ []string, _ int) ([]domain.MessageSearchHit, error) {
	return nil, nil
}

// noopBusinessService returns a live (non-deleted) business by default so the
// CreateConversation soft-delete-aware existence gate passes. Tests that need a
// soft-deleted (pending-erasure) organization override GetByIDFunc to return
// domain.ErrBusinessNotFound.
type noopBusinessService struct {
	GetByIDFunc func(ctx context.Context, id uuid.UUID) (*domain.Business, error)
}

func (s *noopBusinessService) Create(_ context.Context, _ *domain.Business, _ uuid.UUID) (*domain.Business, error) {
	return nil, nil
}
func (s *noopBusinessService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	if s.GetByIDFunc != nil {
		return s.GetByIDFunc(ctx, id)
	}
	return &domain.Business{ID: id}, nil
}
func (s *noopBusinessService) Update(_ context.Context, _ *domain.Business, _ uuid.UUID) (*domain.Business, error) {
	return nil, nil
}
func (s *noopBusinessService) UpdateLogoURL(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (*domain.Business, error) {
	return nil, nil
}
func (s *noopBusinessService) UpdateSettingsKeys(_ context.Context, _ uuid.UUID, _ map[string]interface{}, _ uuid.UUID) (*domain.Business, error) {
	return nil, nil
}
func (s *noopBusinessService) GetToolApprovals(_ context.Context, _ uuid.UUID) (map[string]domain.ToolFloor, error) {
	return map[string]domain.ToolFloor{}, nil
}
func (s *noopBusinessService) UpdateToolApprovals(_ context.Context, _ uuid.UUID, _ map[string]domain.ToolFloor) error {
	return nil
}
func (s *noopBusinessService) ListMembershipsByUser(_ context.Context, _ uuid.UUID) ([]service.MembershipSummary, error) {
	return []service.MembershipSummary{}, nil
}

// noopProjectService returns ErrProjectNotFound by default. Tests that need
// a populated project override GetByIDFunc.
type noopProjectService struct {
	GetByIDFunc func(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
}

func (s *noopProjectService) Create(_ context.Context, _, _ uuid.UUID, _ service.CreateProjectInput) (*domain.Project, error) {
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
func (s *noopProjectService) Update(_ context.Context, _, _, _ uuid.UUID, _ service.UpdateProjectInput) (*domain.Project, error) {
	return nil, nil
}
func (s *noopProjectService) DeleteCascade(_ context.Context, _, _, _ uuid.UUID) (deletedConversations, deletedMessages int, err error) {
	return 0, 0, nil
}
func (s *noopProjectService) CountConversations(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}

// noopConversationService panics on every call. Wired by default into
// handler tests that don't exercise MoveConversation or ListMessages;
// calling it signals the test forgot to inject a real
// *service.ConversationService.
type noopConversationService struct{}

func (noopConversationService) MoveToProject(_ context.Context, _ string, _, _ uuid.UUID, _ *string) (*domain.Conversation, error) {
	panic("noopConversationService.MoveToProject: test must wire a real *service.ConversationService when exercising MoveConversation")
}

func (noopConversationService) OpenChat(_ context.Context, _ string, _, _ uuid.UUID) (*service.ChatView, error) {
	panic("noopConversationService.OpenChat: test must wire a real *service.ConversationService when exercising ListMessages")
}

func (noopConversationService) DeleteWithMessages(_ context.Context, _ string) error {
	panic("noopConversationService.DeleteWithMessages: test must wire a real *service.ConversationService when exercising DeleteConversation")
}

// stubProjectRepoForHandler is a one-method ProjectRepository stub used by
// MoveConversation handler tests to feed the in-line ConversationService.
// Most repo methods are unreachable in this path — they return nil/empty
// values to satisfy the interface.
type stubProjectRepoForHandler struct {
	GetByIDFunc func(ctx context.Context, id uuid.UUID) (*domain.Project, error)
}

func (s *stubProjectRepoForHandler) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	if s.GetByIDFunc != nil {
		return s.GetByIDFunc(ctx, id)
	}
	return nil, domain.ErrProjectNotFound
}
func (s *stubProjectRepoForHandler) Create(_ context.Context, _ *domain.Project) error {
	return nil
}
func (s *stubProjectRepoForHandler) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (s *stubProjectRepoForHandler) Update(_ context.Context, _ *domain.Project) error {
	return nil
}
func (s *stubProjectRepoForHandler) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (s *stubProjectRepoForHandler) CountConversationsByID(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (s *stubProjectRepoForHandler) HardDeleteCascade(_ context.Context, _ uuid.UUID) (deletedConvos, deletedMessages int, err error) {
	return 0, 0, nil
}

// newRealConvSvcForMoveTest constructs a real *service.ConversationService
// over the test's existing conv + message mocks plus a stubProjectRepo that
// echoes a single fixture project. Used by MoveConversation handler tests
// to exercise the full handler→service→repo path while keeping locale and
// system-note assertions intact.
func newRealConvSvcForMoveTest(t *testing.T, convRepo domain.ConversationRepository, msgRepo domain.MessageRepository, projRepo domain.ProjectRepository) *service.ConversationService {
	t.Helper()
	svc, err := service.NewConversationService(convRepo, msgRepo, projRepo, &MockPendingToolCallRepository{})
	if err != nil {
		t.Fatalf("newRealConvSvcForMoveTest: %v", err)
	}
	return svc
}

// newTestConversationHandler builds a ConversationHandler wired with a stub
// business service and a stub project service that returns ErrProjectNotFound
// by default. Tests that need custom behavior call NewConversationHandler
// directly with their own services. Injects an empty
// PendingToolCallRepository mock so the pendingApprovals array is
// always serialized as [] for legacy tests.
func newTestConversationHandler(convRepo domain.ConversationRepository, msgRepo domain.MessageRepository) *ConversationHandler {
	convSvc, err := service.NewConversationService(convRepo, msgRepo, &stubProjectRepoForHandler{}, &MockPendingToolCallRepository{})
	if err != nil {
		panic(err)
	}
	h, err := NewConversationHandler(convRepo, msgRepo, &noopBusinessService{}, &noopProjectService{}, convSvc)
	if err != nil {
		panic(err)
	}
	return h
}

// convBizCtx seeds an authz.BusinessContext into ctx. All conversation handler
// tests use this instead of middleware.UserIDKey so the RBAC gate (BusinessContextFromCtx)
// is satisfied. Permissions default to full content access unless overridden.
func convBizCtx(businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	if len(perms) == 0 {
		perms = []authz.Permission{
			authz.PermContentRead,
			authz.PermContentCreate,
			authz.PermContentUpdate,
			authz.PermContentDelete,
		}
	}
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: perms,
	})
}

// TestCreateConversation_Success tests successful conversation creation
func TestCreateConversation_Success(t *testing.T) {
	mockRepo := &MockConversationRepository{
		CreateFunc: func(ctx context.Context, conv *domain.Conversation) error {
			assert.NotEmpty(t, conv.ID)
			assert.NotEmpty(t, conv.UserID)
			assert.Equal(t, "My New Conversation", conv.Title)
			assert.False(t, conv.CreatedAt.IsZero())
			assert.False(t, conv.UpdatedAt.IsZero())
			return nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body, _ := json.Marshal(map[string]any{"title": "My New Conversation"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

	userID := uuid.New()
	ctx := convBizCtx(uuid.New(), userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

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

// TestCreateConversation_SoftDeletedBusiness pins the 152-ФЗ data-lifecycle
// gate: creating a new PII conversation against a soft-deleted (pending-erasure)
// organization must 404 and persist nothing. authz.RequireBusinessAccess gates
// the route on the surviving membership row alone, so during the deletion grace
// window a member still reaches this handler; the soft-delete-aware
// businessService.GetByID (deleted_at IS NULL) surfaces the org as
// ErrBusinessNotFound, which the handler maps to 404. Reverting the existence
// check lets the conversation be created against an organization awaiting
// erasure — this test then fails on the created flag.
func TestCreateConversation_SoftDeletedBusiness(t *testing.T) {
	var created bool
	mockRepo := &MockConversationRepository{
		CreateFunc: func(_ context.Context, _ *domain.Conversation) error {
			created = true
			return nil
		},
	}

	softDeletedBiz := &noopBusinessService{
		GetByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return nil, domain.ErrBusinessNotFound
		},
	}
	convSvc, err := service.NewConversationService(mockRepo, &MockMessageRepository{}, &stubProjectRepoForHandler{}, &MockPendingToolCallRepository{})
	require.NoError(t, err)
	handler, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, softDeletedBiz, &noopProjectService{}, convSvc)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"title": "My New Conversation"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))
	req = req.WithContext(convBizCtx(uuid.New(), uuid.New()))

	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.False(t, created,
		"no conversation may be created against a soft-deleted organization — reverting the existence gate reproduces the 152-ФЗ ingestion leak")

	var response ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&response))
	assert.Equal(t, "business not found", response.Error)
}

// TestCreateConversation_NoBusinessContext tests creation without BusinessContext in context
func TestCreateConversation_NoBusinessContext(t *testing.T) {
	mockRepo := &MockConversationRepository{}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body, _ := json.Marshal(map[string]any{"title": "My New Conversation"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestCreateConversation_ValidationError tests validation errors
func TestCreateConversation_ValidationError(t *testing.T) {
	tests := []struct {
		name          string
		request       map[string]any
		expectedField string
	}{
		{
			name:          "missing title",
			request:       map[string]any{"title": ""},
			expectedField: "Title",
		},
		{
			name:          "title too long",
			request:       map[string]any{"title": strings.Repeat("a", 201)},
			expectedField: "Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockConversationRepository{}
			handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

			userID := uuid.New()
			ctx := convBizCtx(uuid.New(), userID)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.CreateConversation(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var response ValidationErrorResponse
			err := json.NewDecoder(w.Body).Decode(&response)
			require.NoError(t, err)
			assert.Equal(t, "Проверка не пройдена", response.Error)
			assert.Contains(t, response.Fields, tt.expectedField)
		})
	}
}

// TestCreateConversation_RepositoryError tests repository errors
func TestCreateConversation_RepositoryError(t *testing.T) {
	mockRepo := &MockConversationRepository{
		CreateFunc: func(ctx context.Context, conv *domain.Conversation) error {
			return errors.New("database error")
		},
	}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body, _ := json.Marshal(map[string]any{"title": "My New Conversation"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))

	userID := uuid.New()
	ctx := convBizCtx(uuid.New(), userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.CreateConversation(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal server error", response.Error)
}

// TestNewConversationHandler_NilRepository tests error on nil repository
func TestNewConversationHandler_NilRepository(t *testing.T) {
	h, err := NewConversationHandler(nil, &MockMessageRepository{}, &noopBusinessService{}, &noopProjectService{}, noopConversationService{})
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestNewConversationHandler_NilBusinessService ensures the new dep
// is checked.
func TestNewConversationHandler_NilBusinessService(t *testing.T) {
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, nil, &noopProjectService{}, noopConversationService{})
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestNewConversationHandler_NilProjectService ensures the new dep
// is checked.
func TestNewConversationHandler_NilProjectService(t *testing.T) {
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, &noopBusinessService{}, nil, noopConversationService{})
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestNewConversationHandler_NilConversationService ensures the move-only
// dep is checked. ConversationService owns the MoveToProject transition;
// without it, /move would nil-deref at request time.
func TestNewConversationHandler_NilConversationService(t *testing.T) {
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, &noopBusinessService{}, &noopProjectService{}, nil)
	assert.Error(t, err)
	assert.Nil(t, h)
}

// TestListConversations_Success tests successful conversation list retrieval
func TestListConversations_Success(t *testing.T) {
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

	businessID := uuid.New()
	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(ctx context.Context, uid, bizID string, limit, offset int) ([]domain.Conversation, error) {
			assert.Equal(t, userID.String(), uid)
			assert.Equal(t, businessID.String(), bizID,
				"ListConversations must scope by the active business_id, not just user_id")
			assert.Equal(t, 20, limit)
			assert.Equal(t, 0, offset)
			return conversations, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	ctx := convBizCtx(businessID, userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

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
	userID := uuid.New()

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(ctx context.Context, uid, bizID string, limit, offset int) ([]domain.Conversation, error) {
			return []domain.Conversation{}, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	ctx := convBizCtx(uuid.New(), userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []domain.Conversation
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response, 0)
	assert.NotNil(t, response)
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
			expectedLimit:  100,
			expectedOffset: 0,
		},
		{
			name:           "negative values treated as defaults",
			queryParams:    "?limit=-10&offset=-5",
			expectedLimit:  20,
			expectedOffset: 0,
		},
		{
			name:           "invalid values treated as defaults",
			queryParams:    "?limit=abc&offset=xyz",
			expectedLimit:  20,
			expectedOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()

			mockRepo := &MockConversationRepository{
				ListByUserIDFunc: func(ctx context.Context, uid, bizID string, limit, offset int) ([]domain.Conversation, error) {
					assert.Equal(t, tt.expectedLimit, limit)
					assert.Equal(t, tt.expectedOffset, offset)
					return []domain.Conversation{}, nil
				},
			}

			handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations"+tt.queryParams, http.NoBody)

			ctx := convBizCtx(uuid.New(), userID)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.ListConversations(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestListConversations_NoBusinessContext tests list without BusinessContext in context
func TestListConversations_NoBusinessContext(t *testing.T) {
	mockRepo := &MockConversationRepository{}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestListConversations_RepositoryError tests repository errors
func TestListConversations_RepositoryError(t *testing.T) {
	userID := uuid.New()

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(ctx context.Context, uid, bizID string, limit, offset int) ([]domain.Conversation, error) {
			return nil, errors.New("database error")
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)

	ctx := convBizCtx(uuid.New(), userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal server error", response.Error)
}

// TestGetConversation_Success tests successful conversation retrieval
func TestGetConversation_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	conversation := &domain.Conversation{
		ID:         conversationID,
		UserID:     userID.String(),
		BusinessID: businessID.String(),
		Title:      "Test Conversation",
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		UpdatedAt:  time.Now().Add(-1 * time.Hour),
	}

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			assert.Equal(t, conversationID, id)
			return conversation, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	ctx := convBizCtx(businessID, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

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
	userID := uuid.New()
	otherUserID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	conversation := &domain.Conversation{
		ID:        conversationID,
		UserID:    otherUserID.String(),
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	ctx := convBizCtx(uuid.New(), userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "forbidden", response.Error)
}

// TestGetConversation_CrossBusiness_NotFound is the handler-level
// fail-on-revert guard for the cross-organization read leak: GetByID returns a
// conversation owned by the requester but scoped to a DIFFERENT organization
// than the active one. The handler must respond 404 "conversation not found"
// (uniform with the genuinely-missing case, so the response is not an
// existence-leak oracle) rather than 200 with the foreign conversation's
// title/project/timestamps. Reverting the business_id check returns 200 here
// and fails the test.
func TestGetConversation_CrossBusiness_NotFound(t *testing.T) {
	userID := uuid.New()
	activeBusiness := uuid.New()
	otherBusiness := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	conversation := &domain.Conversation{
		ID:         conversationID,
		UserID:     userID.String(),
		BusinessID: otherBusiness.String(),
		Title:      "Org B Conversation",
	}

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return conversation, nil
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	ctx := convBizCtx(activeBusiness, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code,
		"a conversation owned under another organization must surface as 404, not 200")

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "conversation not found", response.Error,
		"cross-business get must use the uniform not-found message (no existence-leak oracle)")
}

// TestGetConversation_NotFound tests conversation not found
func TestGetConversation_NotFound(t *testing.T) {
	userID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			return nil, domain.ErrConversationNotFound
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	ctx := convBizCtx(uuid.New(), userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "conversation not found", response.Error)
}

// TestGetConversation_NoBusinessContext tests get without BusinessContext in context
func TestGetConversation_NoBusinessContext(t *testing.T) {
	conversationID := "507f1f77bcf86cd799439011"
	mockRepo := &MockConversationRepository{}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestGetConversation_RepositoryError tests repository errors
func TestGetConversation_RepositoryError(t *testing.T) {
	userID := uuid.New()
	conversationID := "507f1f77bcf86cd799439011"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.Conversation, error) {
			return nil, errors.New("database error")
		},
	}

	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, http.NoBody)

	ctx := convBizCtx(uuid.New(), userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetConversation(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal server error", response.Error)
}

// TestConversation_JSONShape_PopulatedFields asserts that json.Marshal of a
// fully populated domain.Conversation produces the camelCase keys the
// frontend (sidebar) relies on for grouping, pinning, and empty-state
// filtering. `pinned` was swapped for `pinnedAt` (single source of truth).
func TestConversation_JSONShape_PopulatedFields(t *testing.T) {
	lastMsg := time.Now().UTC()
	pinnedAt := time.Now().UTC()
	conv := domain.Conversation{
		ID:            "c1",
		UserID:        "u1",
		BusinessID:    "b1",
		ProjectID:     ptr("p1"),
		Title:         "Ошибки после обновления",
		TitleStatus:   domain.TitleStatusAutoPending,
		PinnedAt:      &pinnedAt,
		LastMessageAt: &lastMsg,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	raw, err := json.Marshal(conv)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	for _, key := range []string{"projectId", "businessId", "pinnedAt", "titleStatus", "lastMessageAt"} {
		_, ok := m[key]
		assert.Truef(t, ok, "expected key %q in JSON shape; got keys: %v", key, keysOf(m))
	}
	assert.Equal(t, "p1", m["projectId"])
	assert.Equal(t, "b1", m["businessId"])
	assert.Equal(t, string(domain.TitleStatusAutoPending), m["titleStatus"])
	_, hasLegacy := m["pinned"]
	assert.False(t, hasLegacy, "legacy `pinned` JSON key must be removed")
}

// TestConversation_JSONShape_NilProjectIDElided documents that when ProjectID
// is nil, the `json:"projectId,omitempty"` tag elides the key. The frontend
// must treat "missing projectId" as "null / Без проекта".
// `pinnedAt` is also omitempty so unpinned chats elide that key.
func TestConversation_JSONShape_NilProjectIDElided(t *testing.T) {
	conv := domain.Conversation{
		ID:          "c2",
		UserID:      "u2",
		BusinessID:  "b2",
		ProjectID:   nil,
		Title:       "t",
		TitleStatus: domain.TitleStatusAutoPending,
	}
	raw, err := json.Marshal(conv)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	_, present := m["projectId"]
	assert.False(t, present, "projectId must be elided when ProjectID is nil (omitempty); got: %v", m)
	_, ok := m["businessId"]
	assert.True(t, ok)
	_, ok = m["titleStatus"]
	assert.True(t, ok)
	_, hasPinned := m["pinnedAt"]
	assert.False(t, hasPinned, "pinnedAt must be elided when PinnedAt is nil (omitempty)")
}

// TestListConversations_JSONShape verifies that GET /api/v1/conversations
// serializes every list item with the five keys the sidebar depends
// on. Nil LastMessageAt is elided (documented as expected).
func TestListConversations_JSONShape(t *testing.T) {
	userID := uuid.New()
	projID := "proj-1"
	lastMsg := time.Now().UTC()

	pinnedAt := time.Now().UTC()
	conversations := []domain.Conversation{
		{
			ID:            "507f1f77bcf86cd799439011",
			UserID:        userID.String(),
			BusinessID:    "biz-1",
			ProjectID:     &projID,
			Title:         "Pinned",
			TitleStatus:   domain.TitleStatusAuto,
			PinnedAt:      &pinnedAt,
			LastMessageAt: &lastMsg,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	}

	mockRepo := &MockConversationRepository{
		ListByUserIDFunc: func(_ context.Context, _, _ string, _, _ int) ([]domain.Conversation, error) {
			return conversations, nil
		},
	}
	handler := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", http.NoBody)
	ctx := convBizCtx(uuid.New(), userID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ListConversations(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
	require.Len(t, items, 1)

	item := items[0]
	for _, key := range []string{"projectId", "businessId", "pinnedAt", "titleStatus", "lastMessageAt"} {
		_, ok := item[key]
		assert.Truef(t, ok, "GET /api/v1/conversations item must carry key %q; got: %v", key, keysOf(item))
	}
	assert.Equal(t, "biz-1", item["businessId"])
	assert.Equal(t, "proj-1", item["projectId"])
	pa, ok := item["pinnedAt"].(string)
	require.True(t, ok, "pinnedAt must serialize as a string (ISO timestamp)")
	assert.NotEmpty(t, pa)
	assert.Equal(t, string(domain.TitleStatusAuto), item["titleStatus"])
	_, hasLegacy := item["pinned"]
	assert.False(t, hasLegacy, "legacy `pinned` JSON key must be removed")
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

// makeAuthedReq builds an *http.Request with a BusinessContext seeded into
// context (satisfying the RBAC gate) and (optionally) a chi URL param {id}.
func makeAuthedReq(t *testing.T, method, path string, body []byte, userID uuid.UUID, convID string) *http.Request {
	t.Helper()
	return makeAuthedReqForBiz(t, method, path, body, uuid.New(), userID, convID)
}

// makeAuthedReqForBiz is makeAuthedReq with an explicit active business so
// tests that exercise the business-scoping guard can align the conversation's
// business_id with the request's active organization.
func makeAuthedReqForBiz(t *testing.T, method, path string, body []byte, businessID, userID uuid.UUID, convID string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, http.NoBody)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	}
	ctx := convBizCtx(businessID, userID)
	if convID != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", convID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return r.WithContext(ctx)
}

// TestCreateConversation_WithProjectID covers Behavior 1.
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
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, bizID, id uuid.UUID) (*domain.Project, error) {
			assert.Equal(t, businessID, bizID)
			assert.Equal(t, projectID, id)
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Reviews"}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, &noopBusinessService{}, proj, noopConversationService{})
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(map[string]any{"title": "Chat", "projectId": pid})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))
	req = req.WithContext(convBizCtx(businessID, userID))
	w := httptest.NewRecorder()
	h.CreateConversation(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, capturedConv)
	require.NotNil(t, capturedConv.ProjectID)
	assert.Equal(t, pid, *capturedConv.ProjectID)
	assert.Equal(t, businessID.String(), capturedConv.BusinessID)
	assert.Equal(t, domain.TitleStatusAutoPending, capturedConv.TitleStatus)
	assert.Nil(t, capturedConv.PinnedAt)
}

// TestCreateConversation_NonCanonicalProjectID_PersistsCanonical guards against
// the data-loss defect where a non-canonical client-supplied project_id was
// persisted verbatim on the new conversation. The stored value must be the
// canonical lowercase-hyphenated UUID so project count and cascade-delete
// queries (which use uuid.String()) still match.
func TestCreateConversation_NonCanonicalProjectID_PersistsCanonical(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	canonical := projectID.String()

	cases := []struct {
		name  string
		input string
	}{
		{"uppercase hex", strings.ToUpper(canonical)},
		{"urn:uuid prefix", "urn:uuid:" + canonical},
		{"braced", "{" + canonical + "}"},
		{"no hyphens", strings.ReplaceAll(canonical, "-", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedConv *domain.Conversation
			mockRepo := &MockConversationRepository{
				CreateFunc: func(_ context.Context, conv *domain.Conversation) error {
					capturedConv = conv
					return nil
				},
			}
			proj := &noopProjectService{
				GetByIDFunc: func(_ context.Context, bizID, id uuid.UUID) (*domain.Project, error) {
					assert.Equal(t, businessID, bizID)
					assert.Equal(t, projectID, id, "project lookup must use the parsed UUID")
					return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Reviews"}, nil
				},
			}
			h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, &noopBusinessService{}, proj, noopConversationService{})
			require.NoError(t, err)

			body, _ := json.Marshal(map[string]any{"title": "Chat", "projectId": tc.input})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))
			req = req.WithContext(convBizCtx(businessID, userID))
			w := httptest.NewRecorder()
			h.CreateConversation(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
			require.NotNil(t, capturedConv)
			require.NotNil(t, capturedConv.ProjectID)
			assert.Equal(t, canonical, *capturedConv.ProjectID,
				"persisted project_id must be the canonical lowercase-hyphenated UUID, not the raw %q", tc.input)
		})
	}
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
			h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, &noopBusinessService{}, &noopProjectService{}, noopConversationService{})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader([]byte(tc.body)))
			req = req.WithContext(convBizCtx(businessID, userID))
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

	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			return nil, domain.ErrProjectNotFound
		},
	}
	h, err := NewConversationHandler(&MockConversationRepository{}, &MockMessageRepository{}, &noopBusinessService{}, proj, noopConversationService{})
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(map[string]any{"title": "x", "projectId": pid})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(body))
	req = req.WithContext(convBizCtx(businessID, userID))
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
				return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
			}
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
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, bid, pid uuid.UUID) (*domain.Project, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, projectID, pid)
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Отзывы"}, nil
		},
	}
	projRepo := &stubProjectRepoForHandler{
		GetByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Отзывы"}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, msgRepo, &noopBusinessService{}, proj, newRealConvSvcForMoveTest(t, mockRepo, msgRepo, projRepo))
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(map[string]any{"projectId": pid})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/move", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	req = req.WithContext(context.WithValue(convBizCtx(businessID, userID), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, captureUpdateProjID)
	assert.Equal(t, pid, *captureUpdateProjID)

	require.NotNil(t, capturedMsg, "system note must be appended")
	assert.Equal(t, convID, capturedMsg.ConversationID)
	assert.Equal(t, "system", capturedMsg.Role)
	assert.Equal(t, "[Чат перемещён в «Отзывы» — с этого момента применяется новая политика]", capturedMsg.Content)

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
				return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String(), ProjectID: ptr("old-proj")}, nil
			}
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String(), ProjectID: nil}, nil
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
	h, err := NewConversationHandler(mockRepo, msgRepo, &noopBusinessService{}, &noopProjectService{}, newRealConvSvcForMoveTest(t, mockRepo, msgRepo, &stubProjectRepoForHandler{}))
	require.NoError(t, err)

	body := []byte(`{"projectId":null}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/move", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	req = req.WithContext(context.WithValue(convBizCtx(businessID, userID), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, captureUpdateProjID, "null projectId must be forwarded as nil to repo")

	require.NotNil(t, capturedMsg)
	assert.Equal(t, "system", capturedMsg.Role)
	assert.Equal(t, "[Чат перемещён в «Без проекта» — с этого момента применяется новая политика]", capturedMsg.Content)
}

func TestMoveConversation_EnglishLocale_NullDestination(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd7994390e1"

	var capturedMsg *domain.Message
	getByIDCall := 0

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			getByIDCall++
			if getByIDCall == 1 {
				return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String(), ProjectID: ptr("old-proj")}, nil
			}
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String(), ProjectID: nil}, nil
		},
		UpdateProjectAssignmentFunc: func(_ context.Context, _ string, _ *string) error {
			return nil
		},
	}
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			capturedMsg = m
			return nil
		},
	}
	h, err := NewConversationHandler(mockRepo, msgRepo, &noopBusinessService{}, &noopProjectService{}, newRealConvSvcForMoveTest(t, mockRepo, msgRepo, &stubProjectRepoForHandler{}))
	require.NoError(t, err)

	body := []byte(`{"projectId":null}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/move", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := context.WithValue(convBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	ctx = i18n.WithLocale(ctx, language.English)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedMsg, "system note must be appended")
	assert.Equal(t,
		"[Chat moved to \"No project\" — the new policy applies from this point]",
		capturedMsg.Content)
}

func TestMoveConversation_EnglishLocale_RealProject(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	convID := "507f1f77bcf86cd7994390e2"

	convAfterMove := &domain.Conversation{
		ID:         convID,
		UserID:     userID.String(),
		BusinessID: businessID.String(),
		ProjectID:  ptr(projectID.String()),
	}

	getByIDCall := 0
	var capturedMsg *domain.Message

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			getByIDCall++
			if getByIDCall == 1 {
				return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
			}
			return convAfterMove, nil
		},
		UpdateProjectAssignmentFunc: func(_ context.Context, _ string, _ *string) error {
			return nil
		},
	}
	msgRepo := &MockMessageRepository{
		CreateFunc: func(_ context.Context, m *domain.Message) error {
			capturedMsg = m
			return nil
		},
	}
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Reviews"}, nil
		},
	}
	projRepo := &stubProjectRepoForHandler{
		GetByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
			return &domain.Project{ID: projectID, BusinessID: businessID, Name: "Reviews"}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, msgRepo, &noopBusinessService{}, proj, newRealConvSvcForMoveTest(t, mockRepo, msgRepo, projRepo))
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(map[string]any{"projectId": pid})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/move", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := context.WithValue(convBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	ctx = i18n.WithLocale(ctx, language.English)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.MoveConversation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedMsg)
	assert.Equal(t,
		"[Chat moved to \"Reviews\" — the new policy applies from this point]",
		capturedMsg.Content)
}

// TestMoveConversation_ProjectCrossBusiness covers Behavior 6.
func TestMoveConversation_ProjectCrossBusiness(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	projectID := uuid.New()
	convID := "507f1f77bcf86cd799439013"

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
		},
	}
	proj := &noopProjectService{
		GetByIDFunc: func(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
			return nil, domain.ErrProjectNotFound
		},
	}
	projRepo := &stubProjectRepoForHandler{
		GetByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Project, error) {
			return &domain.Project{ID: id, BusinessID: uuid.New(), Name: "OtherBizProject"}, nil
		},
	}
	h, err := NewConversationHandler(mockRepo, &MockMessageRepository{}, &noopBusinessService{}, proj, newRealConvSvcForMoveTest(t, mockRepo, &MockMessageRepository{}, projRepo))
	require.NoError(t, err)

	pid := projectID.String()
	body, _ := json.Marshal(map[string]any{"projectId": pid})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/move", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	req = req.WithContext(context.WithValue(convBizCtx(businessID, userID), chi.RouteCtxKey, rctx))
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

// --- GET /messages pendingApprovals tests ------

// newConversationHandlerWithPending wires a ConversationHandler with a
// real *service.ConversationService whose pending-tool-call repo is the
// supplied mock — so the existing GET /messages tests still drive the
// projection via ListPendingByConversation, just through the service
// seam instead of the deleted handler-side path.
func newConversationHandlerWithPending(t *testing.T, convRepo domain.ConversationRepository, msgRepo domain.MessageRepository, pendingRepo domain.PendingToolCallRepository) *ConversationHandler {
	t.Helper()
	convSvc, err := service.NewConversationService(convRepo, msgRepo, &stubProjectRepoForHandler{}, pendingRepo)
	require.NoError(t, err)
	h, err := NewConversationHandler(convRepo, msgRepo, &noopBusinessService{}, &noopProjectService{}, convSvc)
	require.NoError(t, err)
	return h
}

// TestGetMessages_NoPendingApprovals_ReturnsEmptyArray covers the default case:
// no active batches → the response serializes `pendingApprovals: []`
// (non-null, empty) so the frontend can iterate unconditionally.
func TestGetMessages_NoPendingApprovals_ReturnsEmptyArray(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439101"

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
		},
	}
	msgRepo := &MockMessageRepository{
		ListByConversationIDFunc: func(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
			return []domain.Message{{ID: "m1", ConversationID: convID, Role: "user", Content: "hi"}}, nil
		},
	}
	pending := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return nil, nil
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, msgRepo, pending)

	req := makeAuthedReqForBiz(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, businessID, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	raw, ok := body["pendingApprovals"]
	require.True(t, ok, "pendingApprovals key must be present; got keys: %v", body)
	assert.Equal(t, "[]", string(raw), "pendingApprovals must serialize as [] when no batches active")
}

// TestGetMessages_WithPendingApprovals_ReturnsPopulatedArray covers the happy
// path: a single pending batch with one manual call surfaces in the response.
func TestGetMessages_WithPendingApprovals_ReturnsPopulatedArray(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439102"
	created := time.Now().UTC().Truncate(time.Second)
	expires := created.Add(24 * time.Hour)

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
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
						{CallID: "toolu_1", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
					},
					CreatedAt: created,
					ExpiresAt: expires,
				},
			}, nil
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, msgRepo, pending)

	req := makeAuthedReqForBiz(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, businessID, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Messages         []domain.Message                 `json:"messages"`
		PendingApprovals []service.PendingApprovalSummary `json:"pendingApprovals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.PendingApprovals, 1)
	assert.Equal(t, "batch-abc", body.PendingApprovals[0].BatchID)
	assert.Equal(t, "msg-42", body.PendingApprovals[0].MessageID)
	assert.Equal(t, "pending", body.PendingApprovals[0].Status)
	require.Len(t, body.PendingApprovals[0].Calls, 1)
	assert.Equal(t, "toolu_1", body.PendingApprovals[0].Calls[0].CallID)
	assert.Equal(t, tools.TelegramSendChannelPost, body.PendingApprovals[0].Calls[0].ToolName)
	assert.NotNil(t, body.PendingApprovals[0].Calls[0].EditableFields, "EditableFields must be [] not null for stable contract")
}

// TestGetMessages_ExpiredBatch_ReportsExpiredStatus documents the
// lazy-expiration pass: a batch whose expires_at is in the past is reported
// with status="expired" so the UI can render the "Истекло" badge.
func TestGetMessages_ExpiredBatch_ReportsExpiredStatus(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439103"

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
		},
	}
	pending := &MockPendingToolCallRepository{
		ListPendingByConversationFunc: func(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
			return []*domain.PendingToolCallBatch{{
				ID:             "batch-old",
				ConversationID: convID,
				Status:         "expired",
				ExpiresAt:      time.Now().Add(-time.Hour),
			}}, nil
		},
	}
	h := newConversationHandlerWithPending(t, convRepo, &MockMessageRepository{}, pending)

	req := makeAuthedReqForBiz(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, businessID, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		PendingApprovals []service.PendingApprovalSummary `json:"pendingApprovals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.PendingApprovals, 1)
	assert.Equal(t, "expired", body.PendingApprovals[0].Status)
}

// TestGetMessages_MultiplePendingBatches_AllReturned covers the edge case
// where a resume spawned a second pause (new turn inside a continuation).
func TestGetMessages_MultiplePendingBatches_AllReturned(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439104"

	convRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			return &domain.Conversation{ID: convID, UserID: userID.String(), BusinessID: businessID.String()}, nil
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

	req := makeAuthedReqForBiz(t, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil, businessID, userID, convID)
	w := httptest.NewRecorder()
	h.ListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		PendingApprovals []service.PendingApprovalSummary `json:"pendingApprovals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.PendingApprovals, 2)
	assert.Equal(t, "b1", body.PendingApprovals[0].BatchID)
	assert.Equal(t, "pending", body.PendingApprovals[0].Status)
	assert.Equal(t, "b2", body.PendingApprovals[1].BatchID)
	assert.Equal(t, "resolving", body.PendingApprovals[1].Status)
}

// TestUpdateConversation_TitleStatusManual is the plumbing
// regression test (Landmine 7): a successful PUT /conversations/{id} with a
// title field MUST flip TitleStatus to "manual" and the repo's Update method
// MUST receive a Conversation whose TitleStatus is "manual" — anything less
// allows the next auto-titler turn to clobber the user's manual rename
// (PITFALLS §12 — the trust-critical contract).
//
// The handler's Update assignment is the load-bearing line; this test
// asserts the assignment survives the request roundtrip and reaches the
// repository layer. The repo Update writes title_status into the $set
// block so the flag persists; this test guards the handler half.
func TestUpdateConversation_TitleStatusManual(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439040"
	original := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  businessID.String(),
		Title:       "Старый",
		TitleStatus: domain.TitleStatusAuto,
	}

	var updated *domain.Conversation
	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			cp := *original
			return &cp, nil
		},
		UpdateFunc: func(_ context.Context, conv *domain.Conversation) error {
			c := *conv
			updated = &c
			return nil
		},
	}
	h := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body, _ := json.Marshal(map[string]any{"title": "Новый ручной заголовок"})
	req := makeAuthedReqForBiz(t, http.MethodPut,
		"/api/v1/conversations/"+convID, body, businessID, userID, convID)
	w := httptest.NewRecorder()
	h.UpdateConversation(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotNil(t, updated, "repo Update must be invoked")

	assert.Equal(t, domain.TitleStatusManual, updated.TitleStatus,
		"PUT /conversations/{id} must unconditionally flip TitleStatus to manual")
	assert.Equal(t, "Новый ручной заголовок", updated.Title,
		"new title must be persisted alongside the manual flag")

	var resp domain.Conversation
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, domain.TitleStatusManual, resp.TitleStatus)
}

// TestUpdateConversation_TitleStatusManual_FromAutoPending is a stricter
// regression: even when the conversation is currently auto_pending (a titler
// goroutine is mid-flight), PUT /conversations/{id} MUST flip to manual.
// The flip is unconditional — there's no "only flip if status was auto" branch.
func TestUpdateConversation_TitleStatusManual_FromAutoPending(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439041"
	original := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  businessID.String(),
		Title:       "",
		TitleStatus: domain.TitleStatusAutoPending,
	}

	var updated *domain.Conversation
	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			cp := *original
			return &cp, nil
		},
		UpdateFunc: func(_ context.Context, conv *domain.Conversation) error {
			c := *conv
			updated = &c
			return nil
		},
	}
	h := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body, _ := json.Marshal(map[string]any{"title": "Победил гонку"})
	req := makeAuthedReqForBiz(t, http.MethodPut,
		"/api/v1/conversations/"+convID, body, businessID, userID, convID)
	w := httptest.NewRecorder()
	h.UpdateConversation(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, updated)
	assert.Equal(t, domain.TitleStatusManual, updated.TitleStatus,
		"PUT must flip auto_pending → manual; the repo's atomic UpdateTitleIfPending will then no-op when the titler returns")
}

// TestUpdateConversation_CrossBusiness_NotFound is the write-path mirror of
// TestGetConversation_CrossBusiness_NotFound: the requester authored the
// conversation (UserID matches, since UserID is identical across every
// organization the user belongs to) but it is scoped to a DIFFERENT
// organization than the active one. PUT /conversations/{id} must respond 404
// "conversation not found" WITHOUT reaching the repo Update — otherwise a user
// with PermContentUpdate in org B can rename a conversation that lives in org A.
// Reverting the business_id guard reaches Update and returns 200, failing this test.
func TestUpdateConversation_CrossBusiness_NotFound(t *testing.T) {
	userID := uuid.New()
	activeBusiness := uuid.New()
	otherBusiness := uuid.New()
	convID := "507f1f77bcf86cd799439050"

	conversation := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  otherBusiness.String(),
		Title:       "Org A Conversation",
		TitleStatus: domain.TitleStatusAuto,
	}

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			cp := *conversation
			return &cp, nil
		},
		UpdateFunc: func(_ context.Context, _ *domain.Conversation) error {
			t.Error("Update must not be called for a cross-organization conversation")
			return nil
		},
	}
	h := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	body, _ := json.Marshal(map[string]any{"title": "Forced rename"})
	req := makeAuthedReqForBiz(t, http.MethodPut,
		"/api/v1/conversations/"+convID, body, activeBusiness, userID, convID)
	w := httptest.NewRecorder()
	h.UpdateConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code,
		"a conversation owned under another organization must surface as 404, not 200")

	var response ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&response))
	assert.Equal(t, "conversation not found", response.Error,
		"cross-business update must use the uniform not-found message (no existence-leak oracle)")
}

// TestDeleteConversation_CrossBusiness_NotFound is the delete-path mirror of
// TestGetConversation_CrossBusiness_NotFound: the requester authored the
// conversation but it is scoped to a DIFFERENT organization than the active
// one. DELETE /conversations/{id} must respond 404 WITHOUT reaching the repo
// Delete — otherwise a user with PermContentDelete in org B can irreversibly
// hard-delete a conversation that lives in org A. Reverting the business_id
// guard reaches Delete and returns 204, failing this test.
func TestDeleteConversation_CrossBusiness_NotFound(t *testing.T) {
	userID := uuid.New()
	activeBusiness := uuid.New()
	otherBusiness := uuid.New()
	convID := "507f1f77bcf86cd799439051"

	conversation := &domain.Conversation{
		ID:         convID,
		UserID:     userID.String(),
		BusinessID: otherBusiness.String(),
		Title:      "Org A Conversation",
	}

	mockRepo := &MockConversationRepository{
		GetByIDFunc: func(_ context.Context, _ string) (*domain.Conversation, error) {
			cp := *conversation
			return &cp, nil
		},
		DeleteFunc: func(_ context.Context, _ string) error {
			t.Error("Delete must not be called for a cross-organization conversation")
			return nil
		},
	}
	h := newTestConversationHandler(mockRepo, &MockMessageRepository{})

	req := makeAuthedReqForBiz(t, http.MethodDelete,
		"/api/v1/conversations/"+convID, nil, activeBusiness, userID, convID)
	w := httptest.NewRecorder()
	h.DeleteConversation(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code,
		"a conversation owned under another organization must surface as 404, not 204")

	var response ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&response))
	assert.Equal(t, "conversation not found", response.Error,
		"cross-business delete must use the uniform not-found message (no existence-leak oracle)")
}

// --- Pin / Unpin handler tests ---------------------

// pinTestHandler builds a ConversationHandler wired with a businessService
// that returns a fixed business ID so Pin/Unpin handler tests can assert
// the (id, business_id, user_id) scope filter without re-stubbing each test.
func pinTestHandler(convRepo domain.ConversationRepository, businessID, userID uuid.UUID) *ConversationHandler {
	h, err := NewConversationHandler(convRepo, &MockMessageRepository{}, &noopBusinessService{}, &noopProjectService{}, noopConversationService{})
	if err != nil {
		panic(err)
	}
	return h
}

// pinBizCtx builds a context with the given businessID and userID seeded as a
// BusinessContext. The Pin/Unpin handlers read bc.BusinessID and bc.UserID to
// scope the atomic update at the repo layer.
func pinBizCtx(businessID, userID uuid.UUID) context.Context {
	return convBizCtx(businessID, userID)
}

func TestConversation_Pin_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439011"

	now := time.Now().UTC()
	pinCalls := 0
	mockRepo := &MockConversationRepository{
		PinFunc: func(_ context.Context, id, biz, uid string) error {
			pinCalls++
			assert.Equal(t, convID, id)
			assert.Equal(t, businessID.String(), biz)
			assert.Equal(t, userID.String(), uid)
			return nil
		},
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			assert.Equal(t, convID, id)
			return &domain.Conversation{
				ID:       convID,
				UserID:   userID.String(),
				PinnedAt: &now,
			}, nil
		},
	}

	h := pinTestHandler(mockRepo, businessID, userID)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/pin", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := context.WithValue(pinBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Pin(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, pinCalls)

	var got domain.Conversation
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.PinnedAt, "Pin response must carry the persisted pinned_at")
}

func TestConversation_Pin_CrossTenant_Returns404(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439011"

	mockRepo := &MockConversationRepository{
		PinFunc: func(_ context.Context, _, _, _ string) error {
			return domain.ErrConversationNotFound
		},
	}

	h := pinTestHandler(mockRepo, businessID, userID)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/pin", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := context.WithValue(pinBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Pin(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code, "cross-tenant pin must return 404, not 403 (no existence leak)")
}

func TestConversation_Pin_NoBusinessContext_Returns500(t *testing.T) {
	mockRepo := &MockConversationRepository{}
	h := pinTestHandler(mockRepo, uuid.New(), uuid.New())

	convID := "507f1f77bcf86cd799439011"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/pin", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.Pin(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestConversation_Pin_BadID_Returns400(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	mockRepo := &MockConversationRepository{}
	h := pinTestHandler(mockRepo, businessID, userID)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/short-id/pin", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "short-id")
	ctx := context.WithValue(pinBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Pin(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestConversation_Unpin_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439011"

	unpinCalls := 0
	mockRepo := &MockConversationRepository{
		UnpinFunc: func(_ context.Context, id, biz, uid string) error {
			unpinCalls++
			assert.Equal(t, convID, id)
			assert.Equal(t, businessID.String(), biz)
			assert.Equal(t, userID.String(), uid)
			return nil
		},
		GetByIDFunc: func(_ context.Context, id string) (*domain.Conversation, error) {
			return &domain.Conversation{
				ID:       convID,
				UserID:   userID.String(),
				PinnedAt: nil,
			}, nil
		},
	}

	h := pinTestHandler(mockRepo, businessID, userID)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/unpin", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := context.WithValue(pinBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Unpin(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, unpinCalls)

	var got domain.Conversation
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Nil(t, got.PinnedAt, "Unpin response must carry pinned_at = nil")
}

func TestConversation_Unpin_CrossTenant_Returns404(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439011"

	mockRepo := &MockConversationRepository{
		UnpinFunc: func(_ context.Context, _, _, _ string) error {
			return domain.ErrConversationNotFound
		},
	}

	h := pinTestHandler(mockRepo, businessID, userID)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/unpin", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := context.WithValue(pinBizCtx(businessID, userID), chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Unpin(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
