package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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

// --- Test helpers / mocks ---

// recordingInvalidator captures InvalidateRole / InvalidateMember calls for
// assertions. Safe for concurrent use (tests don't currently exercise that).
type recordingInvalidator struct {
	mu               sync.Mutex
	invalidateRole   []roleCall
	invalidateMember []memberCall
}

type roleCall struct {
	BusinessID uuid.UUID
	RoleID     uuid.UUID
}

type memberCall struct {
	BusinessID uuid.UUID
	UserID     uuid.UUID
}

func (r *recordingInvalidator) InvalidateRole(b, role uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidateRole = append(r.invalidateRole, roleCall{BusinessID: b, RoleID: role})
}

func (r *recordingInvalidator) InvalidateMember(b, u uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidateMember = append(r.invalidateMember, memberCall{BusinessID: b, UserID: u})
}

// businessContextFull builds a BusinessContext with explicit RoleID for tests
// that exercise CheckEscalationSubset / CheckSelfLockout.
func businessContextFull(ctx context.Context, businessID, userID, roleID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(ctx, authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      roleID,
		Permissions: perms,
	})
}

// withRoleIDParam injects {roleId} into chi route params.
func withRoleIDParam(r *http.Request, roleID string) *http.Request {
	return withChiParams(r, map[string]string{"roleId": roleID})
}

// newRolesHandlerForTest constructs a RolesHandler from test mocks.
// audit defaults to audit.Nop so existing tests don't have to
// thread a logger through every call site.
func newRolesHandlerForTest(
	rr domain.RoleRepository,
	mr domain.BusinessMembershipRepository,
	pool poolBeginner,
	inv roleCacheInvalidator,
) *RolesHandler {
	return &RolesHandler{
		roleRepo:       rr,
		membershipRepo: mr,
		pool:           pool,
		invalidator:    inv,
		audit:          audit.Nop(),
	}
}

// ownerRoleID parses the system owner role UUID for tests where the actor is
// the system owner (bypasses CheckEscalationSubset).
func ownerRoleUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(domain.SystemRoleOwnerID)
	require.NoError(t, err)
	return id
}

// --- NewRolesHandler nil checks ---

func TestRolesHandler_NewRolesHandler_NilCheck(t *testing.T) {
	t.Parallel()
	_, err := NewRolesHandler(nil, &MockBusinessMembershipRepository{}, mustPool(t), &recordingInvalidator{}, audit.Nop())
	assert.Error(t, err)
	_, err = NewRolesHandler(&MockRoleRepository{}, nil, mustPool(t), &recordingInvalidator{}, audit.Nop())
	assert.Error(t, err)
	_, err = NewRolesHandler(&MockRoleRepository{}, &MockBusinessMembershipRepository{}, nil, &recordingInvalidator{}, audit.Nop())
	assert.Error(t, err)
	_, err = NewRolesHandler(&MockRoleRepository{}, &MockBusinessMembershipRepository{}, mustPool(t), nil, audit.Nop())
	assert.Error(t, err)
	_, err = NewRolesHandler(&MockRoleRepository{}, &MockBusinessMembershipRepository{}, mustPool(t), &recordingInvalidator{}, nil)
	assert.Error(t, err)
}

func mustPool(t *testing.T) poolBeginner {
	t.Helper()
	p, err := pgxmock.NewPool()
	require.NoError(t, err)
	return p
}

// --- List tests ---

func TestRolesHandler_List_WithMemberCount(t *testing.T) {
	rr := &MockRoleRepository{}
	mr := &MockBusinessMembershipRepository{}
	inv := &recordingInvalidator{}

	bizID := uuid.New()
	viewerID := uuid.New()
	customRoleID := uuid.New()
	systemRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)

	rr.On("ListByBusinessWithCounts", mock.Anything, bizID).Return([]domain.RoleWithMemberCount{
		{
			Role: domain.Role{
				ID:          systemRoleID,
				BusinessID:  nil,
				Name:        "owner",
				Description: "Business owner",
				Permissions: []string{"business.read"},
				IsSystem:    true,
			},
			MemberCount: 2,
		},
		{
			Role: domain.Role{
				ID:          customRoleID,
				BusinessID:  &bizID,
				Name:        "custom",
				Description: "Custom role",
				Permissions: []string{"content.read"},
				IsSystem:    false,
			},
			MemberCount: 1,
		},
	}, nil)

	h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

	ctx := businessContextFull(context.Background(), bizID, viewerID, ownerRoleUUID(t), authz.PermRolesRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Len(t, body, 2)
	require.Contains(t, body[0], "member_count")
	require.Contains(t, body[1], "member_count")
	assert.EqualValues(t, 2, body[0]["member_count"])
	assert.EqualValues(t, 1, body[1]["member_count"])
	rr.AssertExpectations(t)
}

func TestRolesHandler_List_Forbidden(t *testing.T) {
	rr := &MockRoleRepository{}
	mr := &MockBusinessMembershipRepository{}
	inv := &recordingInvalidator{}

	h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

	bizID := uuid.New()
	userID := uuid.New()
	ctx := businessContextFull(context.Background(), bizID, userID, ownerRoleUUID(t))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
	rr.AssertNotCalled(t, "ListByBusinessWithCounts")
}

func TestRolesHandler_List_NoBusinessContext(t *testing.T) {
	rr := &MockRoleRepository{}
	mr := &MockBusinessMembershipRepository{}
	inv := &recordingInvalidator{}

	h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRolesHandler_List_RepoError(t *testing.T) {
	rr := &MockRoleRepository{}
	mr := &MockBusinessMembershipRepository{}
	inv := &recordingInvalidator{}

	bizID := uuid.New()
	userID := uuid.New()

	rr.On("ListByBusinessWithCounts", mock.Anything, bizID).Return(nil, errors.New("db connection failed"))

	h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

	ctx := businessContextFull(context.Background(), bizID, userID, ownerRoleUUID(t), authz.PermRolesRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	rr.AssertExpectations(t)
}

// --- Create tests ---

func TestRolesHandler_Create(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("CreateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesCreate, authz.PermContentRead)
		body := map[string]interface{}{
			"name":        "Editor",
			"description": "Can read content",
			"permissions": []string{"content.read"},
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bodyBytes)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Empty(t, inv.invalidateRole)
		rr.AssertExpectations(t)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("missing_permission", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		bizID := uuid.New()
		ctx := businessContextFull(context.Background(), bizID, uuid.New(), uuid.New())
		body, _ := json.Marshal(map[string]interface{}{"name": "x", "permissions": []string{}})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assertErrorCode(t, w, "forbidden")
		rr.AssertNotCalled(t, "CreateInTx")
	})

	t.Run("invalid_body", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesCreate)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("not json"))).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_body")
	})

	t.Run("empty_name", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesCreate)
		body, _ := json.Marshal(map[string]interface{}{"name": "   ", "permissions": []string{}})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "validation_failed")
	})

	t.Run("permissions_array_too_large", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesCreate)
		perms := make([]string, 101)
		for i := range perms {
			perms[i] = "content.read"
		}
		body, _ := json.Marshal(map[string]interface{}{"name": "x", "permissions": perms})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "validation_failed")
	})

	t.Run("unknown_permission", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesCreate)
		body, _ := json.Marshal(map[string]interface{}{"name": "x", "permissions": []string{"made.up"}})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_permission")
	})

	t.Run("escalation_subset", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		actorRoleID := uuid.New()
		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), actorRoleID,
			authz.PermRolesCreate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "Editor",
			"permissions": []string{"members.invite"},
		})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assertErrorCode(t, w, "cannot_grant_unowned_permissions")
		rr.AssertNotCalled(t, "CreateInTx")
	})

	t.Run("name_taken", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectRollback()
		rr.On("CreateInTx", mock.Anything, mock.Anything, mock.Anything).Return(domain.ErrRoleNameTaken)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesCreate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "DuplicateRole",
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assertErrorCode(t, w, "role_name_taken")
		assert.Empty(t, inv.invalidateRole)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("dedup_permissions", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("CreateInTx", mock.Anything, mock.Anything, mock.MatchedBy(func(role *domain.Role) bool {
			return len(role.Permissions) == 1 && role.Permissions[0] == "content.read"
		})).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesCreate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "Dup",
			"permissions": []string{"content.read", "content.read", "content.read"},
		})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		rr.AssertExpectations(t)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("clone_from_noop", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("CreateInTx", mock.Anything, mock.Anything, mock.MatchedBy(func(role *domain.Role) bool {
			return len(role.Permissions) == 1 && role.Permissions[0] == "content.read"
		})).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesCreate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "Editor",
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPost, "/?clone_from="+uuid.New().String(),
			bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		rr.AssertExpectations(t)
	})
}

// TestRolesHandler_Create_OversizedBody_Returns400 asserts both input bounds on
// POST: the body byte cap (MaxBytesReader) and the per-field name length cap.
//
// over-large body: the bulk lives in an unknown field so no per-field check
// catches it — a pass requires the byte cap. over-long name: the bulk is the
// name itself. Revert either the `r.Body = http.MaxBytesReader(...)` line or the
// `len(name) > maxRoleNameLen` check in Create and the respective sub-test flips
// to 201 (CreateInTx runs).
func TestRolesHandler_Create_OversizedBody_Returns400(t *testing.T) {
	t.Run("oversized_body", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("CreateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t),
			authz.PermRolesCreate, authz.PermContentRead)
		filler := strings.Repeat("a", maxRoleBodyBytes+1)
		body := `{"name":"Editor","permissions":["content.read"],"_pad":"` + filler + `"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		rr.AssertNotCalled(t, "CreateInTx")
	})

	t.Run("oversized_name", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("CreateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t),
			authz.PermRolesCreate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        strings.Repeat("a", 300),
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "validation_failed")
		rr.AssertNotCalled(t, "CreateInTx")
	})
}

// TestRolesHandler_Update_OversizedBody_Returns400 mirrors the Create case for
// PATCH. Revert the MaxBytesReader line or the name length cap in Update and the
// respective sub-test flips to 200 (UpdateInTx runs).
func TestRolesHandler_Update_OversizedBody_Returns400(t *testing.T) {
	t.Run("oversized_body", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			Name:       "Old",
			IsSystem:   false,
		}, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("UpdateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		filler := strings.Repeat("a", maxRoleBodyBytes+1)
		body := `{"name":"New","permissions":["content.read"],"_pad":"` + filler + `"}`
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		rr.AssertNotCalled(t, "UpdateInTx")
	})

	t.Run("oversized_name", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			Name:       "Old",
			IsSystem:   false,
		}, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("UpdateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        strings.Repeat("a", 300),
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "validation_failed")
		rr.AssertNotCalled(t, "UpdateInTx")
	})
}

// --- Update tests ---

func TestRolesHandler_Update(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			Name:       "Old",
			IsSystem:   false,
		}, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("UpdateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "New",
			"description": "Updated",
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		require.Len(t, inv.invalidateRole, 1, "InvalidateRole must fire exactly once after commit")
		assert.Equal(t, bizID, inv.invalidateRole[0].BusinessID)
		assert.Equal(t, roleID, inv.invalidateRole[0].RoleID)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("missing_permission", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), uuid.New())
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(`{}`))).WithContext(ctx)
		req = withRoleIDParam(req, uuid.New().String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid_role_id", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesUpdate)
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader([]byte(`{}`))).WithContext(ctx)
		req = withRoleIDParam(req, "not-a-uuid")
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_role_id")
	})

	t.Run("cross_tenant", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizA := uuid.New()
		bizB := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizB,
			Name:       "leaked",
			IsSystem:   false,
		}, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizA, uuid.New(), ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{"name": "x", "permissions": []string{"content.read"}})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assertErrorCode(t, w, "role_not_found")
		rr.AssertNotCalled(t, "UpdateInTx")
	})

	t.Run("system_role", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: nil,
			Name:       "viewer",
			IsSystem:   true,
		}, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{"name": "x", "permissions": []string{"content.read"}})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assertErrorCode(t, w, "system_role_immutable")
		rr.AssertNotCalled(t, "UpdateInTx")
	})

	t.Run("escalation_subset", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		actorRoleID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			Name:       "Custom",
			IsSystem:   false,
		}, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), actorRoleID, authz.PermRolesUpdate)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "x",
			"permissions": []string{"members.invite"},
		})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assertErrorCode(t, w, "cannot_grant_unowned_permissions")
	})

	t.Run("self_lockout", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		actorID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			Name:       "AdminLike",
			IsSystem:   false,
		}, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, roleID,
			authz.PermRolesUpdate, authz.PermMembersUpdateRole, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "x",
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assertErrorCode(t, w, "self_lockout")
		rr.AssertNotCalled(t, "UpdateInTx")
		assert.Empty(t, inv.invalidateRole)
	})

	t.Run("name_taken", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectRollback()
		rr.On("UpdateInTx", mock.Anything, mock.Anything, mock.Anything).Return(domain.ErrRoleNameTaken)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "Dup",
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assertErrorCode(t, w, "role_name_taken")
		assert.Empty(t, inv.invalidateRole)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("invalidate_after_commit", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit().WillReturnError(errors.New("commit fail"))
		mockPool.ExpectRollback()
		rr.On("UpdateInTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t),
			authz.PermRolesUpdate, authz.PermContentRead)
		body, _ := json.Marshal(map[string]interface{}{
			"name":        "x",
			"permissions": []string{"content.read"},
		})
		req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body)).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Update(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Empty(t, inv.invalidateRole, "InvalidateRole must NOT fire when commit fails")
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// --- Delete tests ---

func TestRolesHandler_Delete(t *testing.T) {
	t.Run("happy_path_no_members", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil)
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(0, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(0, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		rr.On("DeleteInTx", mock.Anything, mock.Anything, roleID).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		require.Len(t, inv.invalidateRole, 1)
		assert.Empty(t, inv.invalidateMember)
		rr.AssertNotCalled(t, "DeleteWithReassignInTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("happy_path_with_reassign", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()
		roleID := uuid.New()
		reassignToID := uuid.New()
		user1, user2, user3 := uuid.New(), uuid.New(), uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil).Once()
		rr.On("GetByID", mock.Anything, reassignToID).Return(&domain.Role{
			ID:          reassignToID,
			BusinessID:  &bizID,
			Permissions: []string{"content.read"},
			IsSystem:    false,
		}, nil).Once()
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(3, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(0, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		reassigned := []uuid.UUID{user1, user2, user3}
		rr.On("DeleteWithReassignInTx", mock.Anything, mock.Anything, bizID, roleID, reassignToID, actorID).
			Return(reassigned, nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesDelete, authz.PermContentRead)
		req := httptest.NewRequest(http.MethodDelete, "/?reassign_to="+reassignToID.String(), http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		require.Len(t, inv.invalidateRole, 1)
		require.Len(t, inv.invalidateMember, 3, "InvalidateMember must fanout per affected user")
		require.NoError(t, mockPool.ExpectationsWereMet())
		mr.AssertExpectations(t)
	})

	t.Run("invalidates_member_reassigned_inside_tx_not_pretx_snapshot", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		actorID := uuid.New()
		roleID := uuid.New()
		reassignToID := uuid.New()
		snapshotUser := uuid.New()
		racingUser := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil).Once()
		rr.On("GetByID", mock.Anything, reassignToID).Return(&domain.Role{
			ID:          reassignToID,
			BusinessID:  &bizID,
			Permissions: []string{"content.read"},
			IsSystem:    false,
		}, nil).Once()
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(1, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(0, nil)

		mr.On("ListUserIDsByRole", mock.Anything, bizID, roleID).
			Return([]uuid.UUID{snapshotUser}, nil).Maybe()

		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit()
		txReassigned := []uuid.UUID{snapshotUser, racingUser}
		rr.On("DeleteWithReassignInTx", mock.Anything, mock.Anything, bizID, roleID, reassignToID, actorID).
			Return(txReassigned, nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, actorID, ownerRoleUUID(t),
			authz.PermRolesDelete, authz.PermContentRead)
		req := httptest.NewRequest(http.MethodDelete, "/?reassign_to="+reassignToID.String(), http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		got := make(map[uuid.UUID]bool, len(inv.invalidateMember))
		for _, c := range inv.invalidateMember {
			got[c.UserID] = true
		}
		assert.True(t, got[snapshotUser], "snapshot member must be invalidated")
		assert.True(t, got[racingUser],
			"member reassigned-into-the-role inside the tx must be invalidated; "+
				"sourcing the fan-out from the pre-tx ListUserIDsByRole snapshot drops it and leaks a stale authz cache entry")
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("missing_permission", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), uuid.New())
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, uuid.New().String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid_role_id", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, "not-a-uuid")
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_role_id")
	})

	t.Run("invalid_reassign_to_param", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/?reassign_to=not-a-uuid", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, uuid.New().String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_reassign_to")
	})

	t.Run("self_reassign", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		roleID := uuid.New()
		ctx := businessContextFull(context.Background(), uuid.New(), uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/?reassign_to="+roleID.String(), http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_reassign_to")
	})

	t.Run("cross_tenant", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizA := uuid.New()
		bizB := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizB,
			IsSystem:   false,
		}, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizA, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assertErrorCode(t, w, "role_not_found")
	})

	t.Run("system_role", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: nil,
			IsSystem:   true,
		}, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assertErrorCode(t, w, "system_role_immutable")
	})

	t.Run("role_in_use_no_reassign", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil)
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(2, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assertErrorCode(t, w, "role_in_use")
		rr.AssertNotCalled(t, "DeleteInTx")
		rr.AssertNotCalled(t, "DeleteWithReassignInTx")
	})

	t.Run("invitation_referencing_role_returns_422_not_500", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil)
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(0, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(1, nil)

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assertErrorCode(t, w, "role_in_use")
		rr.AssertNotCalled(t, "DeleteInTx")
		rr.AssertNotCalled(t, "DeleteWithReassignInTx")
	})

	t.Run("reassign_target_cross_tenant", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizA := uuid.New()
		bizB := uuid.New()
		roleID := uuid.New()
		reassignToID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizA,
			IsSystem:   false,
		}, nil).Once()
		rr.On("CountMembersByRole", mock.Anything, bizA, roleID).Return(2, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(0, nil)
		rr.On("GetByID", mock.Anything, reassignToID).Return(&domain.Role{
			ID:         reassignToID,
			BusinessID: &bizB,
			IsSystem:   false,
		}, nil).Once()

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizA, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/?reassign_to="+reassignToID.String(), http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assertErrorCode(t, w, "invalid_reassign_to")
	})

	t.Run("reassign_target_escalation", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}

		bizID := uuid.New()
		actorRoleID := uuid.New()
		roleID := uuid.New()
		reassignToID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil).Once()
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(2, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(0, nil)
		rr.On("GetByID", mock.Anything, reassignToID).Return(&domain.Role{
			ID:          reassignToID,
			BusinessID:  &bizID,
			Permissions: []string{"members.invite"},
			IsSystem:    false,
		}, nil).Once()

		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), actorRoleID,
			authz.PermRolesDelete, authz.PermContentRead)
		req := httptest.NewRequest(http.MethodDelete, "/?reassign_to="+reassignToID.String(), http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assertErrorCode(t, w, "cannot_grant_unowned_permissions")
	})

	t.Run("invalidate_after_commit", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		mockPool, err := pgxmock.NewPool()
		require.NoError(t, err)

		bizID := uuid.New()
		roleID := uuid.New()

		rr.On("GetByID", mock.Anything, roleID).Return(&domain.Role{
			ID:         roleID,
			BusinessID: &bizID,
			IsSystem:   false,
		}, nil)
		rr.On("CountMembersByRole", mock.Anything, bizID, roleID).Return(0, nil)
		rr.On("CountInvitationsByRole", mock.Anything, roleID).Return(0, nil)
		mockPool.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		mockPool.ExpectCommit().WillReturnError(errors.New("commit fail"))
		mockPool.ExpectRollback()
		rr.On("DeleteInTx", mock.Anything, mock.Anything, roleID).Return(nil)

		h := newRolesHandlerForTest(rr, mr, mockPool, inv)

		ctx := businessContextFull(context.Background(), bizID, uuid.New(), ownerRoleUUID(t), authz.PermRolesDelete)
		req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody).WithContext(ctx)
		req = withRoleIDParam(req, roleID.String())
		w := httptest.NewRecorder()
		h.Delete(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Empty(t, inv.invalidateRole, "InvalidateRole must NOT fire when commit fails")
		assert.Empty(t, inv.invalidateMember)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// --- MyPermissions tests ---

func TestRolesHandler_MyPermissions(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		bizID := uuid.New()
		userID := uuid.New()
		ctx := businessContextFull(context.Background(), bizID, userID, ownerRoleUUID(t),
			authz.PermRolesRead, authz.PermContentRead)
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
		w := httptest.NewRecorder()
		h.MyPermissions(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Permissions []string `json:"permissions"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.ElementsMatch(t, []string{"roles.read", "content.read"}, body.Permissions)
	})

	t.Run("empty_permissions", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		bizID := uuid.New()
		userID := uuid.New()
		ctx := businessContextFull(context.Background(), bizID, userID, ownerRoleUUID(t))
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
		w := httptest.NewRecorder()
		h.MyPermissions(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Permissions []string `json:"permissions"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Empty(t, body.Permissions)
	})

	t.Run("no_business_context", func(t *testing.T) {
		rr := &MockRoleRepository{}
		mr := &MockBusinessMembershipRepository{}
		inv := &recordingInvalidator{}
		h := newRolesHandlerForTest(rr, mr, mustPool(t), inv)

		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		w := httptest.NewRecorder()
		h.MyPermissions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
