package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// --- New mocks (those not already declared in members_test.go) ---

// MockInvitationRepository is a testify/mock implementation of
// domain.InvitationRepository. All 9 methods are stubbed via mock.Mock.
type MockInvitationRepository struct{ mock.Mock }

func (m *MockInvitationRepository) Create(ctx context.Context, inv *domain.Invitation) error {
	return m.Called(ctx, inv).Error(0)
}

func (m *MockInvitationRepository) CreateInTx(ctx context.Context, tx pgx.Tx, inv *domain.Invitation) error {
	return m.Called(ctx, tx, inv).Error(0)
}

func (m *MockInvitationRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.Invitation, error) {
	args := m.Called(ctx, tokenHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Invitation), args.Error(1)
}

func (m *MockInvitationRepository) ListPendingByBusiness(ctx context.Context, businessID uuid.UUID) ([]domain.Invitation, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Invitation), args.Error(1)
}

func (m *MockInvitationRepository) CountPendingByBusiness(ctx context.Context, businessID uuid.UUID) (int, error) {
	args := m.Called(ctx, businessID)
	return args.Int(0), args.Error(1)
}

func (m *MockInvitationRepository) CountPendingByBusinessInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) (int, error) {
	args := m.Called(ctx, tx, businessID)
	return args.Int(0), args.Error(1)
}

func (m *MockInvitationRepository) Revoke(ctx context.Context, id, businessID uuid.UUID) error {
	return m.Called(ctx, id, businessID).Error(0)
}

func (m *MockInvitationRepository) MarkAccepted(ctx context.Context, id, accepterUserID uuid.UUID) error {
	return m.Called(ctx, id, accepterUserID).Error(0)
}

func (m *MockInvitationRepository) MarkAcceptedInTx(ctx context.Context, tx pgx.Tx, id, accepterUserID uuid.UUID) error {
	return m.Called(ctx, tx, id, accepterUserID).Error(0)
}

// MockBusinessRepository is a testify/mock implementation of
// domain.BusinessRepository. Only GetByID is exercised by the
// invitations_test suite; the rest return "not implemented".
type MockBusinessRepository struct{ mock.Mock }

func (m *MockBusinessRepository) Create(ctx context.Context, b *domain.Business) error {
	return errors.New("not implemented")
}

func (m *MockBusinessRepository) CreateInTx(ctx context.Context, tx pgx.Tx, b *domain.Business) error {
	return errors.New("not implemented")
}

func (m *MockBusinessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessRepository) Update(ctx context.Context, b *domain.Business) error {
	return errors.New("not implemented")
}

func (m *MockBusinessRepository) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	return errors.New("not implemented")
}

// --- Test scaffolding ---

type invitationTestFixture struct {
	mockPool pgxmock.PgxPoolIface
	invRepo  *MockInvitationRepository
	memRepo  *MockBusinessMembershipRepository
	roleRepo *MockRoleRepository
	userRepo *MockUserRepository
	bizRepo  *MockBusinessRepository
	inv      *MockCacheInvalidator
	now      time.Time
	handler  *InvitationsHandler
}

func newInvitationFixture(t *testing.T) *invitationTestFixture {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mockPool.Close() })

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	f := &invitationTestFixture{
		mockPool: mockPool,
		invRepo:  &MockInvitationRepository{},
		memRepo:  &MockBusinessMembershipRepository{},
		roleRepo: &MockRoleRepository{},
		userRepo: &MockUserRepository{},
		bizRepo:  &MockBusinessRepository{},
		inv:      &MockCacheInvalidator{},
		now:      now,
	}
	f.handler = &InvitationsHandler{
		invitationRepo: f.invRepo,
		membershipRepo: f.memRepo,
		roleRepo:       f.roleRepo,
		userRepo:       f.userRepo,
		businessRepo:   f.bizRepo,
		pool:           mockPool,
		invalidator:    f.inv,
		now:            func() time.Time { return f.now },
	}
	return f
}

// ownerBC produces a BusinessContext with system Owner role and the
// PermMembersInvite permission. Used as the default actor for Create-path tests.
func ownerBC(bizID, userID uuid.UUID) authz.BusinessContext {
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	return authz.BusinessContext{
		BusinessID:  bizID,
		UserID:      userID,
		RoleID:      ownerRoleID,
		Permissions: []authz.Permission{authz.PermMembersInvite},
	}
}

func requestWithBC(method, path, body string, bc authz.BusinessContext) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(authz.WithBusinessContext(r.Context(), bc))
	return r
}

// --- Tests: Create ---

func TestInvitationsHandler_Create_HappyPath(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	// System role (BusinessID nil) — passes CR-01.
	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
		ID:          roleID,
		BusinessID:  nil,
		Name:        "editor",
		Permissions: []string{string(authz.PermMembersInvite)},
	}, nil)

	f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	f.invRepo.On("CountPendingByBusinessInTx", mock.Anything, mock.Anything, bizID).Return(0, nil)
	f.invRepo.On("CreateInTx", mock.Anything, mock.Anything, mock.AnythingOfType("*domain.Invitation")).Return(nil)
	f.mockPool.ExpectCommit()

	body := fmt.Sprintf(`{"role_id":"%s","expires_in":3600}`, roleID)
	req := requestWithBC(http.MethodPost, "/api/v1/businesses/"+bizID.String()+"/invitations", body, ownerBC(bizID, userID))
	w := httptest.NewRecorder()

	f.handler.Create(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
	var resp createInvitationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token, "INVITE-01: token must be present in create response")
	require.Equal(t, roleID, resp.RoleID)
	require.NoError(t, f.mockPool.ExpectationsWereMet())
}

func TestInvitationsHandler_Create_NoPermission(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	bc := authz.BusinessContext{BusinessID: bizID, UserID: userID, Permissions: []authz.Permission{}}

	body := `{"role_id":"` + uuid.New().String() + `"}`
	req := requestWithBC(http.MethodPost, "/x", body, bc)
	w := httptest.NewRecorder()
	f.handler.Create(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"forbidden"`)
}

func TestInvitationsHandler_Create_CrossTenantRoleID_400(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	otherBizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	// Role belongs to a DIFFERENT business — CR-01 / D-12a / T-03-04.
	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
		ID: roleID, BusinessID: &otherBizID, Name: "custom", Permissions: []string{},
	}, nil)

	body := fmt.Sprintf(`{"role_id":"%s"}`, roleID)
	req := requestWithBC(http.MethodPost, "/x", body, ownerBC(bizID, userID))
	w := httptest.NewRecorder()
	f.handler.Create(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_role_id")
}

func TestInvitationsHandler_Create_EscalationSubset_403(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)
	bc := authz.BusinessContext{
		BusinessID:  bizID,
		UserID:      userID,
		RoleID:      editorRoleID, // NOT system Owner — CheckEscalationSubset enforces
		Permissions: []authz.Permission{authz.PermMembersInvite, authz.PermContentRead},
	}

	// Target role demands a permission the actor does not hold.
	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
		ID: roleID, BusinessID: nil, Name: "admin",
		Permissions: []string{string(authz.PermMembersInvite), "members.update_role"},
	}, nil)

	body := fmt.Sprintf(`{"role_id":"%s"}`, roleID)
	req := requestWithBC(http.MethodPost, "/x", body, bc)
	w := httptest.NewRecorder()
	f.handler.Create(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "cannot_grant_unowned_permissions")
}

func TestInvitationsHandler_Create_PendingCap_429(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
		ID: roleID, BusinessID: nil, Name: "editor",
		Permissions: []string{string(authz.PermMembersInvite)},
	}, nil)

	f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	f.invRepo.On("CountPendingByBusinessInTx", mock.Anything, mock.Anything, bizID).Return(20, nil)
	f.mockPool.ExpectRollback()

	body := fmt.Sprintf(`{"role_id":"%s"}`, roleID)
	req := requestWithBC(http.MethodPost, "/x", body, ownerBC(bizID, userID))
	w := httptest.NewRecorder()
	f.handler.Create(w, req)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "too_many_pending")
	f.invRepo.AssertNotCalled(t, "CreateInTx", mock.Anything, mock.Anything, mock.Anything)
}

func TestInvitationsHandler_Create_ExpiresInTooSmall_400(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	body := fmt.Sprintf(`{"role_id":"%s","expires_in":3599}`, uuid.New())
	req := requestWithBC(http.MethodPost, "/x", body, ownerBC(bizID, uuid.New()))
	w := httptest.NewRecorder()
	f.handler.Create(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "validation_failed")
}

func TestInvitationsHandler_Create_ExpiresInTooLarge_400(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	body := fmt.Sprintf(`{"role_id":"%s","expires_in":2592001}`, uuid.New())
	req := requestWithBC(http.MethodPost, "/x", body, ownerBC(bizID, uuid.New()))
	w := httptest.NewRecorder()
	f.handler.Create(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "validation_failed")
}

func TestInvitationsHandler_Create_ExpiresInDefault_7Days(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
		ID: roleID, BusinessID: nil, Name: "editor",
		Permissions: []string{string(authz.PermMembersInvite)},
	}, nil)
	f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	f.invRepo.On("CountPendingByBusinessInTx", mock.Anything, mock.Anything, bizID).Return(0, nil)
	var captured *domain.Invitation
	f.invRepo.On("CreateInTx", mock.Anything, mock.Anything, mock.MatchedBy(func(inv *domain.Invitation) bool {
		captured = inv
		return true
	})).Return(nil)
	f.mockPool.ExpectCommit()

	body := fmt.Sprintf(`{"role_id":"%s"}`, roleID) // no expires_in → default 7d
	req := requestWithBC(http.MethodPost, "/x", body, ownerBC(bizID, userID))
	w := httptest.NewRecorder()
	f.handler.Create(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
	require.NotNil(t, captured)
	expected := f.now.Add(7 * 24 * time.Hour)
	require.WithinDuration(t, expected, captured.ExpiresAt, time.Second)
}

// --- Tests: ListPending ---

func TestInvitationsHandler_ListPending_HappyPath_NoRawTokens(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	inviterID := uuid.New()

	invs := []domain.Invitation{
		{ID: uuid.New(), BusinessID: bizID, RoleID: roleID, TokenHash: "secret-hash", ExpiresAt: f.now.Add(time.Hour), CreatedBy: inviterID, CreatedAt: f.now},
	}
	f.invRepo.On("ListPendingByBusiness", mock.Anything, bizID).Return(invs, nil)
	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{ID: roleID, Name: "editor"}, nil)
	f.userRepo.On("GetByID", mock.Anything, inviterID).Return(&domain.User{ID: inviterID, Email: "alice@example.com"}, nil)

	req := requestWithBC(http.MethodGet, "/x", "", ownerBC(bizID, userID))
	w := httptest.NewRecorder()
	f.handler.ListPending(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.NotContains(t, body, "secret-hash", "INVITE-05: response must NOT include token_hash")
	require.NotContains(t, body, "\"token\"", "INVITE-05: response must NOT include raw token")
	require.Contains(t, body, "alice@example.com")
}

// --- Tests: Revoke ---

func TestInvitationsHandler_Revoke_204(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	invID := uuid.New()

	f.invRepo.On("Revoke", mock.Anything, invID, bizID).Return(nil)

	req := requestWithBC(http.MethodDelete, "/x", "", ownerBC(bizID, userID))
	req = withChiParams(req, map[string]string{"inviteId": invID.String()})
	w := httptest.NewRecorder()
	f.handler.Revoke(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())
}

func TestInvitationsHandler_Revoke_NotFound_404(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	invID := uuid.New()

	f.invRepo.On("Revoke", mock.Anything, invID, bizID).Return(domain.ErrInvitationNotFound)

	req := requestWithBC(http.MethodDelete, "/x", "", ownerBC(bizID, userID))
	req = withChiParams(req, map[string]string{"inviteId": invID.String()})
	w := httptest.NewRecorder()
	f.handler.Revoke(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not_found")
}

func TestInvitationsHandler_Revoke_AlreadyAccepted_410(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	userID := uuid.New()
	invID := uuid.New()

	f.invRepo.On("Revoke", mock.Anything, invID, bizID).Return(domain.ErrInvitationAccepted)

	req := requestWithBC(http.MethodDelete, "/x", "", ownerBC(bizID, userID))
	req = withChiParams(req, map[string]string{"inviteId": invID.String()})
	w := httptest.NewRecorder()
	f.handler.Revoke(w, req)

	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"accepted"`)
}

// --- Tests: Accept refusal matrix (parametrized) ---

func TestInvitationsHandler_Accept_RefusalMatrix(t *testing.T) {
	type tc struct {
		name string
		// seed configures all mocks; returns the userID and rawToken for the request
		seed        func(t *testing.T, f *invitationTestFixture) (userID uuid.UUID, rawToken string)
		wantStatus  int
		wantSubstr  []string
		mustNotCall []string // method names that MUST NOT have been called
	}
	cases := []tc{
		{
			name: "already_accepted_replay",
			seed: func(t *testing.T, f *invitationTestFixture) (uuid.UUID, string) {
				userID := uuid.New()
				bizID := uuid.New()
				roleID := uuid.New()
				invID := uuid.New()
				rawToken := "raw-token-1"
				accepted := f.now.Add(-time.Minute)
				f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				f.mockPool.ExpectRollback()
				f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
					ID: invID, BusinessID: bizID, RoleID: roleID, AcceptedAt: &accepted, ExpiresAt: f.now.Add(time.Hour),
				}, nil)
				return userID, rawToken
			},
			wantStatus:  http.StatusGone,
			wantSubstr:  []string{`"reason":"accepted"`},
			mustNotCall: []string{"MarkAcceptedInTx", "Insert", "InvalidateMember"},
		},
		{
			name: "revoked",
			seed: func(t *testing.T, f *invitationTestFixture) (uuid.UUID, string) {
				userID := uuid.New()
				revoked := f.now.Add(-time.Minute)
				f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				f.mockPool.ExpectRollback()
				f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
					ID: uuid.New(), BusinessID: uuid.New(), RoleID: uuid.New(), RevokedAt: &revoked, ExpiresAt: f.now.Add(time.Hour),
				}, nil)
				return userID, "raw-token-2"
			},
			wantStatus:  http.StatusGone,
			wantSubstr:  []string{`"reason":"revoked"`},
			mustNotCall: []string{"MarkAcceptedInTx", "Insert", "InvalidateMember"},
		},
		{
			name: "expired",
			seed: func(t *testing.T, f *invitationTestFixture) (uuid.UUID, string) {
				userID := uuid.New()
				f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				f.mockPool.ExpectRollback()
				f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
					ID: uuid.New(), BusinessID: uuid.New(), RoleID: uuid.New(), ExpiresAt: f.now.Add(-time.Hour),
				}, nil)
				return userID, "raw-token-3"
			},
			wantStatus:  http.StatusGone,
			wantSubstr:  []string{`"reason":"expired"`},
			mustNotCall: []string{"MarkAcceptedInTx", "Insert", "InvalidateMember"},
		},
		{
			name: "unknown_token",
			seed: func(t *testing.T, f *invitationTestFixture) (uuid.UUID, string) {
				userID := uuid.New()
				f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				f.mockPool.ExpectRollback()
				f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(nil, domain.ErrInvitationNotFound)
				return userID, "unknown-raw"
			},
			wantStatus:  http.StatusGone,
			wantSubstr:  []string{`"reason":"unknown"`},
			mustNotCall: []string{"MarkAcceptedInTx", "Insert", "InvalidateMember"},
		},
		{
			name: "already_member",
			seed: func(t *testing.T, f *invitationTestFixture) (uuid.UUID, string) {
				userID := uuid.New()
				bizID := uuid.New()
				roleID := uuid.New()
				f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				f.mockPool.ExpectRollback()
				f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
					ID: uuid.New(), BusinessID: bizID, RoleID: roleID, ExpiresAt: f.now.Add(time.Hour),
				}, nil)
				f.memRepo.On("GetByBusinessUser", mock.Anything, bizID, userID).Return(&domain.BusinessMember{
					BusinessID: bizID, UserID: userID, RoleID: roleID, Status: "active",
				}, nil)
				return userID, "already-member-raw"
			},
			wantStatus:  http.StatusConflict,
			wantSubstr:  []string{`"already_member"`},
			mustNotCall: []string{"MarkAcceptedInTx", "Insert", "InvalidateMember"}, // INVITE-09: token NOT consumed
		},
		{
			name: "pending_valid_happy",
			seed: func(t *testing.T, f *invitationTestFixture) (uuid.UUID, string) {
				userID := uuid.New()
				bizID := uuid.New()
				roleID := uuid.New()
				invID := uuid.New()
				inviterID := uuid.New()
				f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
					ID: invID, BusinessID: bizID, RoleID: roleID, ExpiresAt: f.now.Add(time.Hour), CreatedBy: inviterID, CreatedAt: f.now.Add(-time.Hour),
				}, nil)
				f.memRepo.On("GetByBusinessUser", mock.Anything, bizID, userID).Return(nil, domain.ErrMembershipNotFound)
				f.memRepo.On("Insert", mock.Anything, mock.Anything, mock.AnythingOfType("*domain.BusinessMember")).Return(nil)
				f.invRepo.On("MarkAcceptedInTx", mock.Anything, mock.Anything, invID, userID).Return(nil)
				f.mockPool.ExpectCommit()
				f.inv.On("InvalidateMember", bizID, userID).Return()
				return userID, "happy-raw"
			},
			wantStatus: http.StatusOK,
			wantSubstr: []string{`"business_id"`, `"role_id"`},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newInvitationFixture(t)
			userID, rawToken := c.seed(t, f)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/"+rawToken+"/accept", nil)
			ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			req = req.WithContext(ctx)
			req = withChiParams(req, map[string]string{"token": rawToken})
			w := httptest.NewRecorder()

			f.handler.Accept(w, req)

			require.Equal(t, c.wantStatus, w.Code, "body=%s", w.Body.String())
			for _, sub := range c.wantSubstr {
				require.Contains(t, w.Body.String(), sub)
			}
			for _, m := range c.mustNotCall {
				switch m {
				case "MarkAcceptedInTx":
					f.invRepo.AssertNotCalled(t, m)
				case "Insert":
					f.memRepo.AssertNotCalled(t, m)
				case "InvalidateMember":
					f.inv.AssertNotCalled(t, m)
				}
			}
			require.NoError(t, f.mockPool.ExpectationsWereMet())
		})
	}
}

// --- Tests: Accept ordering + auth ---

func TestInvitationsHandler_Accept_NoJWT_401(t *testing.T) {
	f := newInvitationFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req = withChiParams(req, map[string]string{"token": "raw"})
	w := httptest.NewRecorder()
	f.handler.Accept(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInvitationsHandler_Accept_HappyPath_InvalidateAfterCommit(t *testing.T) {
	// Critical INVITE-11 assertion: pgxmock.ExpectationsWereMet enforces
	// strict ORDER (BeginTx → ... → Commit). MockCacheInvalidator.AssertCalled
	// confirms invalidator was invoked. Together they prove
	// "Invalidate AFTER Commit" — not strictly literal-ordering but
	// structurally enforced because Invalidate is only reachable after
	// committed=true is set, which only happens after Commit returns nil.
	f := newInvitationFixture(t)
	userID := uuid.New()
	bizID := uuid.New()
	roleID := uuid.New()
	invID := uuid.New()
	inviterID := uuid.New()

	f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
		ID: invID, BusinessID: bizID, RoleID: roleID, ExpiresAt: f.now.Add(time.Hour), CreatedBy: inviterID, CreatedAt: f.now.Add(-time.Hour),
	}, nil)
	f.memRepo.On("GetByBusinessUser", mock.Anything, bizID, userID).Return(nil, domain.ErrMembershipNotFound)
	f.memRepo.On("Insert", mock.Anything, mock.Anything, mock.AnythingOfType("*domain.BusinessMember")).Return(nil)
	f.invRepo.On("MarkAcceptedInTx", mock.Anything, mock.Anything, invID, userID).Return(nil)
	f.mockPool.ExpectCommit()
	f.inv.On("InvalidateMember", bizID, userID).Return()

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)
	req = withChiParams(req, map[string]string{"token": "happy"})
	w := httptest.NewRecorder()

	f.handler.Accept(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NoError(t, f.mockPool.ExpectationsWereMet())
	f.inv.AssertCalled(t, "InvalidateMember", bizID, userID)
}

func TestInvitationsHandler_Accept_CommitFails_NoInvalidate(t *testing.T) {
	// If Commit fails, Invalidate MUST NOT fire (T-03-10 mitigation).
	f := newInvitationFixture(t)
	userID := uuid.New()
	bizID := uuid.New()
	roleID := uuid.New()
	invID := uuid.New()

	f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
		ID: invID, BusinessID: bizID, RoleID: roleID, ExpiresAt: f.now.Add(time.Hour), CreatedBy: uuid.New(), CreatedAt: f.now.Add(-time.Hour),
	}, nil)
	f.memRepo.On("GetByBusinessUser", mock.Anything, bizID, userID).Return(nil, domain.ErrMembershipNotFound)
	f.memRepo.On("Insert", mock.Anything, mock.Anything, mock.AnythingOfType("*domain.BusinessMember")).Return(nil)
	f.invRepo.On("MarkAcceptedInTx", mock.Anything, mock.Anything, invID, userID).Return(nil)
	f.mockPool.ExpectCommit().WillReturnError(errors.New("commit failed"))
	f.mockPool.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)
	req = withChiParams(req, map[string]string{"token": "raw"})
	w := httptest.NewRecorder()

	f.handler.Accept(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	f.inv.AssertNotCalled(t, "InvalidateMember", mock.Anything, mock.Anything)
}

// --- Tests: Token never logged ---

func TestInvitationsHandler_Accept_TokenNotLogged(t *testing.T) {
	// T-03-02 — capture slog default output and assert raw token + hash absent.
	var buf bytes.Buffer
	prevLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLog) })

	f := newInvitationFixture(t)
	userID := uuid.New()
	bizID := uuid.New()
	roleID := uuid.New()
	invID := uuid.New()

	f.mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
		ID: invID, BusinessID: bizID, RoleID: roleID, ExpiresAt: f.now.Add(time.Hour), CreatedBy: uuid.New(), CreatedAt: f.now.Add(-time.Hour),
	}, nil)
	f.memRepo.On("GetByBusinessUser", mock.Anything, bizID, userID).Return(nil, domain.ErrMembershipNotFound)
	f.memRepo.On("Insert", mock.Anything, mock.Anything, mock.AnythingOfType("*domain.BusinessMember")).Return(nil)
	f.invRepo.On("MarkAcceptedInTx", mock.Anything, mock.Anything, invID, userID).Return(nil)
	f.mockPool.ExpectCommit()
	f.inv.On("InvalidateMember", bizID, userID).Return()

	rawToken := "very-secret-token-x9z"
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)
	req = withChiParams(req, map[string]string{"token": rawToken})
	w := httptest.NewRecorder()

	f.handler.Accept(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	output := buf.String()
	require.NotContains(t, output, rawToken, "T-03-02: raw token must NEVER appear in slog output")
	hash := computeTokenHash(rawToken)
	require.NotContains(t, output, hash, "T-03-02 paranoia: hash must NEVER appear in slog output")
	require.Contains(t, output, invID.String(), "invitation_id IS safe to log")
}

// --- Tests: Preview ---

func TestInvitationsHandler_Preview_PublicNoAuth(t *testing.T) {
	f := newInvitationFixture(t)
	bizID := uuid.New()
	roleID := uuid.New()
	invID := uuid.New()
	inviterID := uuid.New()

	f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
		ID: invID, BusinessID: bizID, RoleID: roleID, ExpiresAt: f.now.Add(time.Hour), CreatedBy: inviterID, CreatedAt: f.now,
	}, nil)
	f.roleRepo.On("GetByID", mock.Anything, roleID).Return(&domain.Role{ID: roleID, Name: "editor"}, nil)
	f.bizRepo.On("GetByID", mock.Anything, bizID).Return(&domain.Business{ID: bizID, Name: "Acme"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/x", nil) // NO BC, NO JWT
	req = withChiParams(req, map[string]string{"token": "any-raw"})
	w := httptest.NewRecorder()

	f.handler.Preview(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	body := w.Body.String()
	require.Contains(t, body, "Acme")
	require.Contains(t, body, "editor")
	require.NotContains(t, body, "created_by", "D-06: information minimization — no inviter identity")
	require.NotContains(t, body, "\"token\"", "D-09: never expose raw token")
	require.NotContains(t, body, "token_hash", "never expose token_hash")
}

func TestInvitationsHandler_Preview_UnknownToken_410(t *testing.T) {
	f := newInvitationFixture(t)
	f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(nil, domain.ErrInvitationNotFound)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = withChiParams(req, map[string]string{"token": "nope"})
	w := httptest.NewRecorder()
	f.handler.Preview(w, req)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"unknown"`)
}

func TestInvitationsHandler_Preview_Expired_410(t *testing.T) {
	f := newInvitationFixture(t)
	f.invRepo.On("GetByTokenHash", mock.Anything, mock.Anything).Return(&domain.Invitation{
		ID: uuid.New(), BusinessID: uuid.New(), RoleID: uuid.New(), ExpiresAt: f.now.Add(-time.Hour),
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = withChiParams(req, map[string]string{"token": "raw"})
	w := httptest.NewRecorder()
	f.handler.Preview(w, req)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"expired"`)
}
