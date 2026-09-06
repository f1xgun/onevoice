package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// TestMembersHandler_AuditRoleGranted_OnHappyPathCommit verifies that PATCH
// /businesses/{id}/members/{userId} fires audit.LogRoleGranted AFTER tx.Commit
// with business_id + actor user_id + target_user_id + new role_id populated.
func TestMembersHandler_AuditRoleGranted_OnHappyPathCommit(t *testing.T) {
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
		ID: newRoleID, BusinessID: nil, Name: "viewer",
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

	auditLog := &capturingAuditLogger{}
	auditLog.expect(1)

	h := &MembersHandler{
		membershipRepo:  mr,
		roleRepo:        rr,
		userRepo:        ur,
		invitationRepo:  &recordingInvitationRepo{},
		businessService: &mockBusinessGetter{},
		pool:            mockPool,
		invalidator:     inv,
		audit:           auditLog,
	}

	ctx := businessContextWith(context.Background(), bizID, actorID, authz.PermMembersUpdateRole)
	body, _ := json.Marshal(map[string]interface{}{"role_id": newRoleID.String()})
	req := httptest.NewRequest(http.MethodPatch, "/businesses/"+bizID.String()+"/members/"+targetID.String(), bytes.NewReader(body)).WithContext(ctx)
	req = withChiParams(req, map[string]string{"userId": targetID.String()})
	w := httptest.NewRecorder()

	h.UpdateMemberRole(w, req)
	auditLog.wait()

	require.Equal(t, http.StatusOK, w.Code)
	entries := auditLog.snapshot()
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, audit.ActionRoleGranted, e.Action)
	assert.Equal(t, "role", e.Resource)
	require.NotNil(t, e.BusinessID)
	assert.Equal(t, bizID, *e.BusinessID)
	require.NotNil(t, e.UserID, "actor user_id (bc.UserID) must populate UserID")
	assert.Equal(t, actorID, *e.UserID)
	details := string(e.Details)
	assert.Contains(t, details, targetID.String(), "Details must capture target_user_id")
	assert.Contains(t, details, newRoleID.String(), "Details must capture new_role_id")
	inv.AssertExpectations(t)
	require.NoError(t, mockPool.ExpectationsWereMet())
}
