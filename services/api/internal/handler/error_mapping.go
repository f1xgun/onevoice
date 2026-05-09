package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// writeAuthzInvariantError centralizes the authz sentinel -> HTTP mapping
// shared by members.go, roles.go, and any future Phase 5 role-mutation
// handlers. Always writes a JSON body of the form {"error":"<code>"}
// matching the contract documented in 02-PATTERNS.md §Error Mapping.
//
// op is a short identifier ("update_member_role", "remove_member", etc.)
// included in the slog.ErrorContext fallback line for triage.
func writeAuthzInvariantError(ctx context.Context, w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, authz.ErrLastOwner):
		writeJSONError(w, http.StatusUnprocessableEntity, "last_owner")
	case errors.Is(err, authz.ErrSelfLockout):
		writeJSONError(w, http.StatusUnprocessableEntity, "self_lockout")
	case errors.Is(err, authz.ErrCannotGrantUnownedPermissions):
		writeJSONError(w, http.StatusForbidden, "cannot_grant_unowned_permissions")
	case errors.Is(err, authz.ErrSystemRoleImmutable):
		writeJSONError(w, http.StatusUnprocessableEntity, "system_role_immutable")
	case errors.Is(err, domain.ErrMembershipNotFound):
		writeJSONError(w, http.StatusNotFound, "member_not_found")
	case errors.Is(err, domain.ErrRoleNotFound):
		writeJSONError(w, http.StatusNotFound, "role_not_found")
	default:
		slog.ErrorContext(ctx, "handler authz error",
			"op", op,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}
