package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestWriteAuthzInvariantError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "ErrLastOwner maps to 422 last_owner",
			err:        authz.ErrLastOwner,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "last_owner",
		},
		{
			name:       "ErrSelfLockout maps to 422 self_lockout",
			err:        authz.ErrSelfLockout,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "self_lockout",
		},
		{
			name:       "ErrCannotGrantUnownedPermissions maps to 403",
			err:        authz.ErrCannotGrantUnownedPermissions,
			wantStatus: http.StatusForbidden,
			wantCode:   "cannot_grant_unowned_permissions",
		},
		{
			name:       "ErrSystemRoleImmutable maps to 422 system_role_immutable",
			err:        authz.ErrSystemRoleImmutable,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "system_role_immutable",
		},
		{
			name:       "ErrMembershipNotFound maps to 404 member_not_found",
			err:        domain.ErrMembershipNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "member_not_found",
		},
		{
			name:       "unknown error maps to 500 internal_server_error",
			err:        errors.New("something unexpected"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_server_error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeAuthzInvariantError(t.Context(), w, "test_op", tc.err)

			assert.Equal(t, tc.wantStatus, w.Code)

			var body struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.wantCode, body.Error)
		})
	}
}

func TestWriteAuthzInvariantError_WrappedErrors(t *testing.T) {
	// Verify errors.Is works via wrapping.
	wrappedLastOwner := errors.Join(errors.New("outer"), authz.ErrLastOwner)
	w := httptest.NewRecorder()
	writeAuthzInvariantError(t.Context(), w, "test_op", wrappedLastOwner)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "last_owner", body.Error)
}
