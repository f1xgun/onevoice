package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestRolesHandler_List_HappyPath verifies 200 + JSON array returned with system roles present.
func TestRolesHandler_List_HappyPath(t *testing.T) {
	rr := &MockRoleRepository{}

	bizID := uuid.New()
	viewerID := uuid.New()
	customRoleID := uuid.New()

	systemRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)
	systemBizID := (*uuid.UUID)(nil) // system roles have NULL business_id

	rr.On("ListByBusiness", mock.Anything, bizID).Return([]domain.Role{
		{
			ID:          systemRoleID,
			BusinessID:  systemBizID,
			Name:        "owner",
			Description: "Business owner",
			Permissions: []string{"business.read", "members.read"},
			IsSystem:    true,
		},
		{
			ID:          customRoleID,
			BusinessID:  &bizID,
			Name:        "custom",
			Description: "Custom role",
			Permissions: []string{"content.read"},
			IsSystem:    false,
		},
	}, nil)

	h, err := NewRolesHandler(rr)
	require.NoError(t, err)

	ctx := businessContextWith(context.Background(), bizID, viewerID, authz.PermRolesRead)
	req := httptest.NewRequest(http.MethodGet, "/businesses/"+bizID.String()+"/roles", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body []map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Len(t, body, 2)

	// Verify first item has expected fields
	first := body[0]
	assert.Equal(t, systemRoleID.String(), first["id"])
	assert.Equal(t, "owner", first["name"])
	assert.Equal(t, true, first["is_system"])

	rr.AssertExpectations(t)
}

// TestRolesHandler_List_Forbidden verifies 403 when PermRolesRead is absent.
func TestRolesHandler_List_Forbidden(t *testing.T) {
	rr := &MockRoleRepository{}

	h, err := NewRolesHandler(rr)
	require.NoError(t, err)

	bizID := uuid.New()
	userID := uuid.New()
	// No PermRolesRead
	ctx := businessContextWith(context.Background(), bizID, userID)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
	rr.AssertNotCalled(t, "ListByBusiness")
}

// TestRolesHandler_List_NoBusinessContext verifies 500 when no BusinessContext is in ctx.
func TestRolesHandler_List_NoBusinessContext(t *testing.T) {
	rr := &MockRoleRepository{}

	h, err := NewRolesHandler(rr)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestRolesHandler_List_RepoError verifies 500 on repo error.
func TestRolesHandler_List_RepoError(t *testing.T) {
	rr := &MockRoleRepository{}

	bizID := uuid.New()
	userID := uuid.New()

	rr.On("ListByBusiness", mock.Anything, bizID).Return(nil, errors.New("db connection failed"))

	h, err := NewRolesHandler(rr)
	require.NoError(t, err)

	ctx := businessContextWith(context.Background(), bizID, userID, authz.PermRolesRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assertErrorCode(t, w, "internal_server_error")
	rr.AssertExpectations(t)
}

// TestRolesHandler_NewRolesHandler_NilCheck verifies nil guard.
func TestRolesHandler_NewRolesHandler_NilCheck(t *testing.T) {
	_, err := NewRolesHandler(nil)
	assert.Error(t, err)
}
