package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// --- Mock repositories ---

// MockBusinessMembershipRepository is a testify/mock implementation.
type MockBusinessMembershipRepository struct {
	mock.Mock
}

func (m *MockBusinessMembershipRepository) Insert(ctx context.Context, tx pgx.Tx, member *domain.BusinessMember) error {
	args := m.Called(ctx, tx, member)
	return args.Error(0)
}

func (m *MockBusinessMembershipRepository) GetByBusinessUser(ctx context.Context, businessID, userID uuid.UUID) (*domain.BusinessMember, error) {
	args := m.Called(ctx, businessID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BusinessMember), args.Error(1)
}

func (m *MockBusinessMembershipRepository) ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]domain.BusinessMember, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BusinessMember), args.Error(1)
}

func (m *MockBusinessMembershipRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.BusinessMember, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BusinessMember), args.Error(1)
}

func (m *MockBusinessMembershipRepository) CountOwnersByBusiness(ctx context.Context, businessID uuid.UUID) (int, error) {
	args := m.Called(ctx, businessID)
	return args.Int(0), args.Error(1)
}

func (m *MockBusinessMembershipRepository) UpdateRole(ctx context.Context, businessID, userID, newRoleID, actorUserID uuid.UUID) error {
	args := m.Called(ctx, businessID, userID, newRoleID, actorUserID)
	return args.Error(0)
}

func (m *MockBusinessMembershipRepository) UpdateRoleInTx(ctx context.Context, tx pgx.Tx, businessID, userID, newRoleID, actorUserID uuid.UUID) error {
	args := m.Called(ctx, tx, businessID, userID, newRoleID, actorUserID)
	return args.Error(0)
}

func (m *MockBusinessMembershipRepository) Delete(ctx context.Context, businessID, userID uuid.UUID) error {
	args := m.Called(ctx, businessID, userID)
	return args.Error(0)
}

func (m *MockBusinessMembershipRepository) DeleteInTx(ctx context.Context, tx pgx.Tx, businessID, userID uuid.UUID) error {
	args := m.Called(ctx, tx, businessID, userID)
	return args.Error(0)
}

func (m *MockBusinessMembershipRepository) ListUserIDsByRole(ctx context.Context, businessID, roleID uuid.UUID) ([]uuid.UUID, error) {
	args := m.Called(ctx, businessID, roleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]uuid.UUID), args.Error(1)
}

// MockRoleRepository is a testify/mock implementation for domain.RoleRepository.
type MockRoleRepository struct {
	mock.Mock
}

func (m *MockRoleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Role), args.Error(1)
}

func (m *MockRoleRepository) ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]domain.Role, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Role), args.Error(1)
}

func (m *MockRoleRepository) ListSystem(ctx context.Context) ([]domain.Role, error) {
	return nil, errors.New("not implemented")
}

func (m *MockRoleRepository) ListByBusinessWithCounts(ctx context.Context, businessID uuid.UUID) ([]domain.RoleWithMemberCount, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.RoleWithMemberCount), args.Error(1)
}

func (m *MockRoleRepository) Create(ctx context.Context, role *domain.Role) error {
	args := m.Called(ctx, role)
	return args.Error(0)
}

func (m *MockRoleRepository) CreateInTx(ctx context.Context, tx pgx.Tx, role *domain.Role) error {
	args := m.Called(ctx, tx, role)
	return args.Error(0)
}

func (m *MockRoleRepository) Update(ctx context.Context, role *domain.Role) error {
	args := m.Called(ctx, role)
	return args.Error(0)
}

func (m *MockRoleRepository) UpdateInTx(ctx context.Context, tx pgx.Tx, role *domain.Role) error {
	args := m.Called(ctx, tx, role)
	return args.Error(0)
}

func (m *MockRoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRoleRepository) DeleteInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	args := m.Called(ctx, tx, id)
	return args.Error(0)
}

func (m *MockRoleRepository) DeleteWithReassignInTx(ctx context.Context, tx pgx.Tx, businessID, oldRoleID, reassignToID, actorUserID uuid.UUID) error {
	args := m.Called(ctx, tx, businessID, oldRoleID, reassignToID, actorUserID)
	return args.Error(0)
}

func (m *MockRoleRepository) Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error {
	args := m.Called(ctx, businessID, oldRoleID, newRoleID)
	return args.Error(0)
}

func (m *MockRoleRepository) CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error) {
	args := m.Called(ctx, businessID, roleID)
	return args.Int(0), args.Error(1)
}

func (m *MockRoleRepository) GetByMemberInBusiness(ctx context.Context, businessID, userID uuid.UUID) (*domain.Role, error) {
	args := m.Called(ctx, businessID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Role), args.Error(1)
}

// MockUserRepository is a testify/mock implementation for domain.UserRepository.
type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) Create(ctx context.Context, user *domain.User) error {
	return errors.New("not implemented")
}

func (m *MockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// GetByIDIncludingDeleted stub —. Handler tests for
// members / invitations don't exercise the soft-delete path; route
// through GetByID so existing testify expectations still match.
func (m *MockUserRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return m.GetByID(ctx, id)
}

func (m *MockUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, errors.New("not implemented")
}

// GetByEmailIncludingDeleted stub — members/invitations handler tests don't
// exercise the login soft-delete path; returns "not implemented" like GetByEmail.
func (m *MockUserRepository) GetByEmailIncludingDeleted(ctx context.Context, email string) (*domain.User, error) {
	return nil, errors.New("not implemented")
}

func (m *MockUserRepository) Update(ctx context.Context, user *domain.User) error {
	return errors.New("not implemented")
}

// UpdatePreferredLocale is unused by the members/invitations handlers — the
// stub returns "not implemented" so any future caller that wires it gets a
// loud failure rather than a silent no-op.
func (m *MockUserRepository) UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	return errors.New("not implemented")
}

func (m *MockUserRepository) UpdateName(ctx context.Context, userID uuid.UUID, name string) error {
	return errors.New("not implemented")
}

// MockCacheInvalidator is a testify/mock for memberCacheInvalidator.
type MockCacheInvalidator struct {
	mock.Mock
}

func (m *MockCacheInvalidator) InvalidateMember(businessID, userID uuid.UUID) {
	m.Called(businessID, userID)
}

// --- Test helpers ---

// businessContextWith returns a context with a BusinessContext injected.
func businessContextWith(ctx context.Context, businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(ctx, authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		Permissions: perms,
	})
}

// newMembersHandlerForTest constructs a MembersHandler with the given dependencies
// using the mockable pool interface. : audit defaults to audit.Nop
// so existing tests don't have to thread a logger through every call site.
func newMembersHandlerForTest(
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
	pool poolBeginner,
	inv memberCacheInvalidator,
) *MembersHandler {
	return &MembersHandler{
		membershipRepo: mr,
		roleRepo:       rr,
		userRepo:       ur,
		pool:           pool,
		invalidator:    inv,
		audit:          audit.Nop(),
	}
}

// withChiParams injects chi URL params into a request context.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	chiCtx := chi.NewRouteContext()
	for k, v := range params {
		chiCtx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
}

// assertErrorCode asserts the JSON body contains the given error code.
func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, wantCode, body.Error)
}

// --- ListMembers tests ---

func TestMembersHandler_ListMembers_HappyPath(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}

	bizID := uuid.New()
	userID1 := uuid.New()
	userID2 := uuid.New()
	roleID := uuid.New()
	now := time.Now().UTC()

	mr.On("ListByBusiness", mock.Anything, bizID).Return([]domain.BusinessMember{
		{BusinessID: bizID, UserID: userID1, RoleID: roleID, Status: "active", JoinedAt: now},
		{BusinessID: bizID, UserID: userID2, RoleID: roleID, Status: "active", JoinedAt: now},
	}, nil)
	ur.On("GetByID", mock.Anything, userID1).Return(&domain.User{ID: userID1, Email: "user1@example.com", Name: "Alice Liddell"}, nil)
	ur.On("GetByID", mock.Anything, userID2).Return(&domain.User{ID: userID2, Email: "user2@example.com"}, nil)
	rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{ID: roleID, Name: "viewer", Permissions: []string{"members.read"}}, nil).Times(2)

	h := newMembersHandlerForTest(mr, rr, ur, nil, nil)

	ctx := businessContextWith(context.Background(), bizID, userID1, authz.PermMembersRead)
	req := httptest.NewRequest(http.MethodGet, "/businesses/"+bizID.String()+"/members", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListMembers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Len(t, body, 2)

	byID := map[string]map[string]any{}
	for _, row := range body {
		u := row["user"].(map[string]any)
		byID[u["id"].(string)] = u
	}
	assert.Equal(t, "Alice Liddell", byID[userID1.String()]["name"], "named member carries name")
	_, hasName := byID[userID2.String()]["name"]
	assert.False(t, hasName, "nameless member omits the name field (omitempty)")

	mr.AssertExpectations(t)
	ur.AssertExpectations(t)
	rr.AssertExpectations(t)
}

func TestMembersHandler_ListMembers_Forbidden(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	bizID := uuid.New()
	userID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, userID)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListMembers(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestMembersHandler_ListMembers_NoBusinessContext(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ListMembers(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- UpdateMemberRole tests ---

func TestMembersHandler_UpdateMemberRole_HappyPath(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	newRoleID := uuid.New()
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)
	now := time.Now().UTC()

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery("SELECT user_id, role_id").
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id"}).
			AddRow(actorID, ownerRoleID).
			AddRow(targetID, newRoleID))
	mockPool.ExpectCommit()

	rr.On("GetByID", mock.Anything, newRoleID).Return(&domain.Role{
		ID:         newRoleID,
		BusinessID: nil,
		Name:       "viewer",
	}, nil)
	mr.On("UpdateRoleInTx", mock.Anything, mock.Anything, bizID, targetID, newRoleID, actorID).Return(nil)
	mr.On("GetByBusinessUser", mock.Anything, bizID, targetID).Return(&domain.BusinessMember{
		BusinessID:    bizID,
		UserID:        targetID,
		RoleID:        newRoleID,
		Status:        "active",
		JoinedAt:      now,
		RoleChangedAt: &now,
		RoleChangedBy: &actorID,
	}, nil)
	inv.On("InvalidateMember", bizID, targetID).Return()

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
	body := map[string]interface{}{"role_id": newRoleID.String()}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/businesses/"+bizID.String()+"/members/"+targetID.String(), bytes.NewReader(bodyBytes)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	inv.AssertExpectations(t)
	mr.AssertExpectations(t)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembersHandler_UpdateMemberRole_LastOwnerRefuses(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	newRoleID := uuid.New()
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)

	rr.On("GetByID", mock.Anything, newRoleID).Return(&domain.Role{
		ID:         newRoleID,
		BusinessID: nil,
		Name:       "viewer",
	}, nil)

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery("SELECT user_id, role_id").
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id"}).
			AddRow(targetID, ownerRoleID))
	mockPool.ExpectRollback()

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
	body := map[string]interface{}{"role_id": newRoleID.String()}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assertErrorCode(t, w, "last_owner")
	inv.AssertNotCalled(t, "InvalidateMember")
}

func TestMembersHandler_UpdateMemberRole_Forbidden(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	bizID := uuid.New()
	userID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, userID)
	body := map[string]interface{}{"role_id": uuid.New().String()}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": uuid.New().String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestMembersHandler_UpdateMemberRole_SelfRoleChangeForbidden(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	h := newMembersHandlerForTest(mr, rr, ur, nil, inv)

	bizID := uuid.New()
	actorID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
	body := `{"role_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(body))).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": actorID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "cannot_change_own_role")
	mr.AssertNotCalled(t, "UpdateRoleInTx")
	rr.AssertNotCalled(t, "GetByID")
	inv.AssertNotCalled(t, "InvalidateMember")
}

func TestMembersHandler_UpdateMemberRole_InvalidBody(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	bizID := uuid.New()
	userID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, userID, authz.PermMembersUpdateRole)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(`{}`))).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": uuid.New().String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMembersHandler_UpdateMemberRole_InvalidUserIDParam(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	bizID := uuid.New()
	userID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, userID, authz.PermMembersUpdateRole)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(`{"role_id":"`+uuid.New().String()+`"}`))).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": "not-a-uuid"})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_user_id")
}

// cross-business role rejection. An admin of business A passes a role
// UUID that exists, but whose BusinessID points at business B. Expect 400
// invalid_role_id, no UpdateRoleInTx call, no cache invalidation.
func TestMembersHandler_UpdateMemberRole_RejectsCrossBusinessRole(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}

	bizA := uuid.New()
	bizB := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	bizBRoleID := uuid.New()

	rr.On("GetByID", mock.Anything, bizBRoleID).Return(&domain.Role{
		ID:         bizBRoleID,
		BusinessID: &bizB,
		Name:       "custom_role_in_business_b",
	}, nil)

	h := newMembersHandlerForTest(mr, rr, ur, nil, inv)

	ctx := businessContextWith(context.Background(), bizA, actorID, authz.PermMembersUpdateRole)
	body := map[string]interface{}{"role_id": bizBRoleID.String()}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_role_id")
	mr.AssertNotCalled(t, "UpdateRoleInTx")
	inv.AssertNotCalled(t, "InvalidateMember")
}

// unknown role_id → 400 invalid_role_id, never reaches UpdateRoleInTx.
func TestMembersHandler_UpdateMemberRole_RejectsUnknownRole(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	unknownRoleID := uuid.New()

	rr.On("GetByID", mock.Anything, unknownRoleID).Return(nil, domain.ErrRoleNotFound)

	h := newMembersHandlerForTest(mr, rr, ur, nil, inv)

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
	body := map[string]interface{}{"role_id": unknownRoleID.String()}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_role_id")
	mr.AssertNotCalled(t, "UpdateRoleInTx")
	inv.AssertNotCalled(t, "InvalidateMember")
}

// --- RemoveMember tests ---

func TestMembersHandler_RemoveMember_HappyPath_NonSelf(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)
	nonOwnerRoleID := uuid.New()

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery("SELECT user_id, role_id").
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id"}).
			AddRow(actorID, ownerRoleID).
			AddRow(targetID, nonOwnerRoleID))
	mockPool.ExpectCommit()

	mr.On("DeleteInTx", mock.Anything, mock.Anything, bizID, targetID).Return(nil)
	inv.On("InvalidateMember", bizID, targetID).Return()

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersRemove)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
	inv.AssertExpectations(t)
	mr.AssertExpectations(t)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembersHandler_RemoveMember_LastOwnerRefuses(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := actorID
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery("SELECT user_id, role_id").
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id"}).
			AddRow(targetID, ownerRoleID))
	mockPool.ExpectRollback()

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := businessContextWith(context.Background(), bizID, actorID)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assertErrorCode(t, w, "last_owner")
	inv.AssertNotCalled(t, "InvalidateMember")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembersHandler_RemoveMember_NonSelf_NoPermission(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, actorID)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestMembersHandler_RemoveMember_SelfRemoval_WithoutPermission(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := actorID
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)
	nonOwnerRoleID := uuid.New()

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery("SELECT user_id, role_id").
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id"}).
			AddRow(uuid.New(), ownerRoleID).
			AddRow(targetID, nonOwnerRoleID))
	mockPool.ExpectCommit()

	mr.On("DeleteInTx", mock.Anything, mock.Anything, bizID, targetID).Return(nil)
	inv.On("InvalidateMember", bizID, targetID).Return()

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := businessContextWith(context.Background(), bizID, actorID)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
	inv.AssertExpectations(t)
	mr.AssertExpectations(t)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembersHandler_RemoveMember_InvalidUserIDParam(t *testing.T) {
	h := newMembersHandlerForTest(&MockBusinessMembershipRepository{}, &MockRoleRepository{}, &MockUserRepository{}, nil, nil)

	bizID := uuid.New()
	actorID := uuid.New()
	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersRemove)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": "not-a-uuid"})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_user_id")
}

// TestMembersHandler_NewMembersHandler_NilChecks verifies nil guard behavior.
func TestMembersHandler_NewMembersHandler_NilChecks(t *testing.T) {
	validMR := &MockBusinessMembershipRepository{}
	validRR := &MockRoleRepository{}
	validUR := &MockUserRepository{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	validInv := &MockCacheInvalidator{}
	validAudit := audit.Nop()

	_, err = NewMembersHandler(nil, validRR, validUR, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, nil, validUR, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, nil, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, nil, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, mockPool, nil, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, mockPool, validInv, nil)
	assert.Error(t, err)

	h, err := NewMembersHandler(validMR, validRR, validUR, mockPool, validInv, validAudit)
	require.NoError(t, err)
	assert.NotNil(t, h)
}
