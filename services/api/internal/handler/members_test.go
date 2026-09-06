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

func (m *MockRoleRepository) DeleteWithReassignInTx(ctx context.Context, tx pgx.Tx, businessID, oldRoleID, reassignToID, actorUserID uuid.UUID) ([]uuid.UUID, error) {
	args := m.Called(ctx, tx, businessID, oldRoleID, reassignToID, actorUserID)
	var ids []uuid.UUID
	if v := args.Get(0); v != nil {
		ids = v.([]uuid.UUID)
	}
	return ids, args.Error(1)
}

func (m *MockRoleRepository) Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error {
	args := m.Called(ctx, businessID, oldRoleID, newRoleID)
	return args.Error(0)
}

func (m *MockRoleRepository) CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error) {
	args := m.Called(ctx, businessID, roleID)
	return args.Int(0), args.Error(1)
}

func (m *MockRoleRepository) CountInvitationsByRole(ctx context.Context, roleID uuid.UUID) (int, error) {
	args := m.Called(ctx, roleID)
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
// businessService defaults to a getter that resolves every ID to a live
// business; soft-deleted-org tests override it via the constructor directly.
func newMembersHandlerForTest(
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
	pool poolBeginner,
	inv memberCacheInvalidator,
) *MembersHandler {
	return &MembersHandler{
		membershipRepo:  mr,
		roleRepo:        rr,
		userRepo:        ur,
		invitationRepo:  &recordingInvitationRepo{},
		businessService: &mockBusinessGetter{},
		pool:            pool,
		invalidator:     inv,
		audit:           audit.Nop(),
	}
}

// recordingInvitationRepo records the RevokeByCreatorInTx calls the member
// endpoints make. The embedded nil interface panics loudly if any other
// invitation method is ever reached from these handlers.
type recordingInvitationRepo struct {
	domain.InvitationRepository
	calls   []revokedInvitationsCall
	revoked int
	err     error
}

type revokedInvitationsCall struct {
	businessID uuid.UUID
	creatorID  uuid.UUID
}

func (r *recordingInvitationRepo) RevokeByCreatorInTx(_ context.Context, _ pgx.Tx, businessID, creatorUserID uuid.UUID) (int64, error) {
	r.calls = append(r.calls, revokedInvitationsCall{businessID: businessID, creatorID: creatorUserID})
	if r.err != nil {
		return 0, r.err
	}
	return int64(r.revoked), nil
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

// pendingDeletionUserRepo mimics the repository split for a member inside their
// 30-day account-deletion grace window: the active-only read reports
// ErrUserNotFound while the deletion-aware read still returns the row.
type pendingDeletionUserRepo struct {
	*MockUserRepository
	pending *domain.User
}

func (r *pendingDeletionUserRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.User, error) {
	return nil, domain.ErrUserNotFound
}

func (r *pendingDeletionUserRepo) GetByIDIncludingDeleted(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if r.pending != nil && r.pending.ID == id {
		return r.pending, nil
	}
	return nil, domain.ErrUserNotFound
}

// TestMembersHandler_ListMembers_MemberPendingDeletion is the regression for a
// member whose account deletion is pending: the team list must still render for
// the whole organization instead of collapsing to 500.
func TestMembersHandler_ListMembers_MemberPendingDeletion(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}

	bizID := uuid.New()
	leavingID := uuid.New()
	roleID := uuid.New()
	now := time.Now().UTC()

	ur := &pendingDeletionUserRepo{
		MockUserRepository: &MockUserRepository{},
		pending:            &domain.User{ID: leavingID, Email: "leaving@example.com"},
	}

	mr.On("ListByBusiness", mock.Anything, bizID).Return([]domain.BusinessMember{
		{BusinessID: bizID, UserID: leavingID, RoleID: roleID, Status: "active", JoinedAt: now},
	}, nil)
	rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{ID: roleID, Name: "viewer", Permissions: []string{"members.read"}}, nil).Once()

	h := newMembersHandlerForTest(mr, rr, ur, nil, nil)

	ctx := businessContextWith(context.Background(), bizID, uuid.New(), authz.PermMembersRead)
	req := httptest.NewRequest(http.MethodGet, "/businesses/"+bizID.String()+"/members", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListMembers(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, leavingID.String(), body[0]["user"].(map[string]any)["id"])

	mr.AssertExpectations(t)
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
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(actorID, ownerRoleID, false).
			AddRow(targetID, newRoleID, false))
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

// TestMembersHandler_UpdateMemberRole_RevokesInvitationsOnLostInvitePermission
// pins the invitation-revocation half of the demote path: a member who loses
// members.invite must not keep handing out working invitation links, while a
// member whose new role still carries it keeps their pending invitations.
func TestMembersHandler_UpdateMemberRole_RevokesInvitationsOnLostInvitePermission(t *testing.T) {
	tests := []struct {
		name        string
		newRolePerm []string
		wantRevoke  bool
	}{
		{name: "new role without members.invite revokes", newRolePerm: []string{"members.read"}, wantRevoke: true},
		{name: "new role keeping members.invite does not revoke", newRolePerm: []string{"members.invite"}, wantRevoke: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &MockBusinessMembershipRepository{}
			rr := &MockRoleRepository{}
			inv := &MockCacheInvalidator{}
			invitations := &recordingInvitationRepo{revoked: 2}
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
				ID: newRoleID, Name: "custom", Permissions: tt.newRolePerm,
			}, nil)
			mr.On("UpdateRoleInTx", mock.Anything, mock.Anything, bizID, targetID, newRoleID, actorID).Return(nil)
			mr.On("GetByBusinessUser", mock.Anything, bizID, targetID).Return(&domain.BusinessMember{
				BusinessID: bizID, UserID: targetID, RoleID: newRoleID, Status: "active", JoinedAt: now,
			}, nil)
			inv.On("InvalidateMember", bizID, targetID).Return()

			h := newMembersHandlerForTest(mr, rr, &MockUserRepository{}, mockPool, inv)
			h.invitationRepo = invitations

			ctx := businessContextWith(context.Background(), bizID, actorID,
				authz.PermMembersUpdateRole, authz.PermMembersInvite, authz.PermMembersRead)
			body, _ := json.Marshal(map[string]interface{}{"role_id": newRoleID.String()})
			req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
			req = withChiParams(req, map[string]string{"userId": targetID.String()})
			w := httptest.NewRecorder()
			h.UpdateMemberRole(w, req)

			require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
			if tt.wantRevoke {
				require.Len(t, invitations.calls, 1)
				assert.Equal(t, bizID, invitations.calls[0].businessID)
				assert.Equal(t, targetID, invitations.calls[0].creatorID)
			} else {
				assert.Empty(t, invitations.calls)
			}
			require.NoError(t, mockPool.ExpectationsWereMet())
		})
	}
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
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(targetID, ownerRoleID, false))
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

// TestMembersHandler_UpdateMemberRole_OversizedBody_Rejected asserts the body
// cap on PATCH members/{userId}. The role_id is valid and the bulk lives in an
// unknown padding field, so the decoder must scan past the cap to finish the
// object — no field validator catches it. The role repo is stubbed to succeed,
// so removing the MaxBytesReader line flips the response to 200 and invokes the
// role lookup.
func TestMembersHandler_UpdateMemberRole_OversizedBody_Rejected(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	h := newMembersHandlerForTest(mr, rr, ur, nil, inv)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	newRoleID := uuid.New()

	filler := strings.Repeat("z", maxMemberBodyBytes+1)
	body := `{"role_id":"` + newRoleID.String() + `","_pad":"` + filler + `"}`
	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(body))).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	rr.AssertNotCalled(t, "GetByID")
	mr.AssertNotCalled(t, "UpdateRoleInTx")
	inv.AssertNotCalled(t, "InvalidateMember")
}

// TestMembersHandler_UpdateMemberRole_SmallBodyAccepted asserts a normal
// under-cap body still succeeds through the same path, so the cap does not
// reject legitimate requests.
func TestMembersHandler_UpdateMemberRole_SmallBodyAccepted(t *testing.T) {
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
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(actorID, ownerRoleID, false).
			AddRow(targetID, newRoleID, false))
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
	body := `{"role_id":"` + newRoleID.String() + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(body))).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, mockPool.ExpectationsWereMet())
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

// An actor holding the seeded system admin role has members.update_role but
// lacks owner-only permissions. Assigning a target the system owner role must
// be refused before any tx opens.
func TestMembersHandler_UpdateMemberRole_EscalationSubset_403(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	adminRoleID := uuid.MustParse(domain.SystemRoleAdminID)
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)

	rr.On("GetByID", mock.Anything, ownerRoleID).Return(&domain.Role{
		ID:         ownerRoleID,
		BusinessID: nil,
		Name:       "owner",
		Permissions: []string{
			string(authz.PermMembersUpdateRole),
			string(authz.PermBusinessDelete),
			string(authz.PermBusinessTransferOwnership),
			string(authz.PermBillingUpdate),
		},
	}, nil)

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID: bizID,
		UserID:     actorID,
		RoleID:     adminRoleID,
		Permissions: []authz.Permission{
			authz.PermMembersUpdateRole,
			authz.PermMembersRemove,
			authz.PermMembersInvite,
		},
	})
	body := map[string]interface{}{"role_id": ownerRoleID.String()}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.UpdateMemberRole(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "cannot_grant_unowned_permissions")
	mr.AssertNotCalled(t, "UpdateRoleInTx")
	inv.AssertNotCalled(t, "InvalidateMember")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// A system owner actor is exempt from the escalation subset check and can still
// promote a member to the system owner role.
func TestMembersHandler_UpdateMemberRole_OwnerCanAssignOwner(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	rr := &MockRoleRepository{}
	ur := &MockUserRepository{}
	inv := &MockCacheInvalidator{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	now := time.Now().UTC()

	rr.On("GetByID", mock.Anything, ownerRoleID).Return(&domain.Role{
		ID:         ownerRoleID,
		BusinessID: nil,
		Name:       "owner",
		Permissions: []string{
			string(authz.PermMembersUpdateRole),
			string(authz.PermBusinessDelete),
			string(authz.PermBusinessTransferOwnership),
			string(authz.PermBillingUpdate),
		},
	}, nil)

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(actorID, ownerRoleID, false).
			AddRow(targetID, ownerRoleID, false))
	mockPool.ExpectCommit()

	mr.On("UpdateRoleInTx", mock.Anything, mock.Anything, bizID, targetID, ownerRoleID, actorID).Return(nil)
	mr.On("GetByBusinessUser", mock.Anything, bizID, targetID).Return(&domain.BusinessMember{
		BusinessID:    bizID,
		UserID:        targetID,
		RoleID:        ownerRoleID,
		Status:        "active",
		JoinedAt:      now,
		RoleChangedAt: &now,
		RoleChangedBy: &actorID,
	}, nil)
	inv.On("InvalidateMember", bizID, targetID).Return()

	h := newMembersHandlerForTest(mr, rr, ur, mockPool, inv)

	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  bizID,
		UserID:      actorID,
		RoleID:      ownerRoleID,
		Permissions: []authz.Permission{authz.PermMembersUpdateRole},
	})
	body := map[string]interface{}{"role_id": ownerRoleID.String()}
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
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(actorID, ownerRoleID, false).
			AddRow(targetID, nonOwnerRoleID, false))
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

// TestMembersHandler_RemoveMember_RevokesPendingInvitations pins that removing
// a member kills the invitation links they handed out: otherwise an ex-member
// keeps onboarding people into an organization they no longer belong to, until
// the tokens expire up to 30 days later.
func TestMembersHandler_RemoveMember_RevokesPendingInvitations(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	inv := &MockCacheInvalidator{}
	invitations := &recordingInvitationRepo{revoked: 1}
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

	h := newMembersHandlerForTest(mr, &MockRoleRepository{}, &MockUserRepository{}, mockPool, inv)
	h.invitationRepo = invitations

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersRemove)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Len(t, invitations.calls, 1)
	assert.Equal(t, bizID, invitations.calls[0].businessID)
	assert.Equal(t, targetID, invitations.calls[0].creatorID)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestMembersHandler_RemoveMember_RevokeFailureRollsBack pins that a failed
// revocation aborts the removal instead of committing a half-applied change.
func TestMembersHandler_RemoveMember_RevokeFailureRollsBack(t *testing.T) {
	mr := &MockBusinessMembershipRepository{}
	inv := &MockCacheInvalidator{}
	invitations := &recordingInvitationRepo{err: errors.New("revoke failed")}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	bizID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)

	mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mockPool.ExpectQuery("SELECT user_id, role_id").
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id"}).
			AddRow(actorID, ownerRoleID).
			AddRow(targetID, uuid.New()))
	mockPool.ExpectRollback()

	mr.On("DeleteInTx", mock.Anything, mock.Anything, bizID, targetID).Return(nil)

	h := newMembersHandlerForTest(mr, &MockRoleRepository{}, &MockUserRepository{}, mockPool, inv)
	h.invitationRepo = invitations

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersRemove)
	req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()
	h.RemoveMember(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	inv.AssertNotCalled(t, "InvalidateMember", bizID, targetID)
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
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(targetID, ownerRoleID, false))
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
	mockPool.ExpectQuery(`(?s)SELECT m\.user_id,.*pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"}).
			AddRow(uuid.New(), ownerRoleID, false).
			AddRow(targetID, nonOwnerRoleID, false))
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
	validIR := &MockInvitationRepository{}
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	validInv := &MockCacheInvalidator{}
	validAudit := audit.Nop()
	validBS := &mockBusinessGetter{}

	_, err = NewMembersHandler(nil, validRR, validUR, validIR, validBS, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, nil, validUR, validIR, validBS, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, nil, validIR, validBS, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, nil, validBS, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, validIR, nil, mockPool, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, validIR, validBS, nil, validInv, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, validIR, validBS, mockPool, nil, validAudit)
	assert.Error(t, err)

	_, err = NewMembersHandler(validMR, validRR, validUR, validIR, validBS, mockPool, validInv, nil)
	assert.Error(t, err)

	h, err := NewMembersHandler(validMR, validRR, validUR, validIR, validBS, mockPool, validInv, validAudit)
	require.NoError(t, err)
	assert.NotNil(t, h)
}

// TestMembersHandler_WriteEndpoints_SoftDeletedBusiness_Returns404 asserts the
// write endpoints (UpdateMemberRole, RemoveMember) reject mutations against a
// soft-deleted (erasure-pending) organization with 404 and never reach the
// mutating repo. The businessGetter returns domain.ErrBusinessNotFound; removing
// the existence gate would let the mutation proceed — fail-on-revert.
func TestMembersHandler_WriteEndpoints_SoftDeletedBusiness_Returns404(t *testing.T) {
	deletedBiz := &mockBusinessGetter{
		getByIDFunc: func(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
			return nil, domain.ErrBusinessNotFound
		},
	}
	newHandler := func(t *testing.T, mr *MockBusinessMembershipRepository) *MembersHandler {
		t.Helper()
		h, err := NewMembersHandler(mr, &MockRoleRepository{}, &MockUserRepository{}, &recordingInvitationRepo{}, deletedBiz, mustPool(t), &MockCacheInvalidator{}, audit.Nop())
		require.NoError(t, err)
		return h
	}

	t.Run("UpdateMemberRole", func(t *testing.T) {
		mr := &MockBusinessMembershipRepository{}
		h := newHandler(t, mr)

		bizID := uuid.New()
		actorID := uuid.New()
		targetID := uuid.New()
		ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
		body := map[string]interface{}{"role_id": uuid.New().String()}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
		req = withChiParams(req, map[string]string{"userId": targetID.String()})
		w := httptest.NewRecorder()
		h.UpdateMemberRole(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assertErrorCode(t, w, "business not found")
		mr.AssertNotCalled(t, "UpdateRoleInTx")
	})

	t.Run("RemoveMember", func(t *testing.T) {
		mr := &MockBusinessMembershipRepository{}
		h := newHandler(t, mr)

		bizID := uuid.New()
		actorID := uuid.New()
		targetID := uuid.New()
		ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersRemove)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withChiParams(req, map[string]string{"userId": targetID.String()})
		w := httptest.NewRecorder()
		h.RemoveMember(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assertErrorCode(t, w, "business not found")
		mr.AssertNotCalled(t, "DeleteInTx")
	})
}
