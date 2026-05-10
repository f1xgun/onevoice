package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// --- writeInvitationStateError branch coverage (CONTEXT D-19 refusal matrix) ---

func TestWriteInvitationStateError_Expired(t *testing.T) {
	w := httptest.NewRecorder()
	writeInvitationStateError(w, domain.ErrInvitationExpired)
	require.Equal(t, http.StatusGone, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `"error":"gone"`)
	require.Contains(t, body, `"reason":"expired"`)
}

func TestWriteInvitationStateError_Revoked(t *testing.T) {
	w := httptest.NewRecorder()
	writeInvitationStateError(w, domain.ErrInvitationRevoked)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"revoked"`)
}

func TestWriteInvitationStateError_Accepted(t *testing.T) {
	w := httptest.NewRecorder()
	writeInvitationStateError(w, domain.ErrInvitationAccepted)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"accepted"`)
}

func TestWriteInvitationStateError_NotFound_CollapsesToUnknown(t *testing.T) {
	// CONTEXT D-19: ErrInvitationNotFound → 410 with reason "unknown" to
	// defend against token-existence enumeration.
	w := httptest.NewRecorder()
	writeInvitationStateError(w, domain.ErrInvitationNotFound)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"unknown"`)
}

func TestWriteInvitationStateError_AlreadyMember(t *testing.T) {
	w := httptest.NewRecorder()
	writeInvitationStateError(w, domain.ErrAlreadyMember)
	require.Equal(t, http.StatusConflict, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `"error":"already_member"`)
	require.False(t, strings.Contains(body, `"reason"`), "409 already_member must NOT carry a reason field")
}

func TestWriteInvitationStateError_GenericFallthrough(t *testing.T) {
	w := httptest.NewRecorder()
	writeInvitationStateError(w, errors.New("some unrelated db error"))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), `"error":"internal_server_error"`)
}

// --- writeRevokeError branch coverage (CONTEXT D-11 — 404 vs 410 split) ---

func TestWriteRevokeError_NotFound(t *testing.T) {
	// CONTEXT D-11: revoke handler distinguishes 404 (not exist OR cross-tenant)
	// from 410 (already terminal). NotFound → 404, not 410.
	w := httptest.NewRecorder()
	writeRevokeError(w, domain.ErrInvitationNotFound)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), `"error":"not_found"`)
}

func TestWriteRevokeError_AcceptedDelegates(t *testing.T) {
	// Already-accepted revoke: idempotent 410 (delegates to writeInvitationStateError).
	w := httptest.NewRecorder()
	writeRevokeError(w, domain.ErrInvitationAccepted)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"accepted"`)
}

func TestWriteRevokeError_RevokedDelegates(t *testing.T) {
	w := httptest.NewRecorder()
	writeRevokeError(w, domain.ErrInvitationRevoked)
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"revoked"`)
}
