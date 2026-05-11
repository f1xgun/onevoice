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
	case errors.Is(err, domain.ErrRoleNameTaken):
		writeJSONError(w, http.StatusConflict, "role_name_taken")
	case errors.Is(err, domain.ErrRoleInUse):
		writeJSONError(w, http.StatusUnprocessableEntity, "role_in_use")
	default:
		slog.ErrorContext(ctx, "handler authz error",
			"op", op,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}

// invitationStateBody is the JSON shape returned by writeInvitationStateError
// for the 410-gone family. Reason discriminates the cause for UI mapping
// (CONTEXT D-19 refusal matrix). Token-existence enumeration is defended via
// uniform 410 across miss/accepted/revoked/expired — only the reason field
// differs, and "unknown" is the safe default for ErrInvitationNotFound.
type invitationStateBody struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
}

// writeInvitationStateError maps invitation-state sentinel errors to the
// CONTEXT D-19 refusal matrix. INVITE-09 (already_member → 409) and
// INVITE-10 (expired/revoked → 410). Body shape:
//
//	410 → {"error":"gone","reason":"expired|revoked|accepted|unknown"}
//	409 → {"error":"already_member"}
//	500 → {"error":"internal_server_error"} (fall-through)
//
// CONTEXT D-19: ErrInvitationNotFound collapses to 410 with reason="unknown"
// to defend against token-existence enumeration. The handler caller for
// REVOKE (which scopes by inviteId not token) MUST use writeRevokeError
// instead — the not-found case there means a cross-tenant attempt and
// returns 404 (D-11).
func writeInvitationStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvitationExpired):
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "expired"})
	case errors.Is(err, domain.ErrInvitationRevoked):
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "revoked"})
	case errors.Is(err, domain.ErrInvitationAccepted):
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "accepted"})
	case errors.Is(err, domain.ErrInvitationNotFound):
		// Token-existence enumeration defense: uniform 410 with reason "unknown".
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "unknown"})
	case errors.Is(err, domain.ErrAlreadyMember):
		writeJSONError(w, http.StatusConflict, "already_member")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}

// writeRevokeError is the special-case mapper for the DELETE revoke endpoint
// where ErrInvitationNotFound means "row doesn't exist OR is cross-tenant"
// and MUST return 404 (CONTEXT D-11), not 410 — the handler scopes by
// {inviteId} (a UUID under owner control) not by an opaque token, so
// information leakage via 404-vs-410 discrimination is not a concern.
//
// Other terminal states (accepted/revoked/expired) idempotently return
// 410 by delegating to writeInvitationStateError.
func writeRevokeError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrInvitationNotFound) {
		writeJSONError(w, http.StatusNotFound, "not_found")
		return
	}
	writeInvitationStateError(w, err)
}
