package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// maxMemberBodyBytes caps the JSON request body on the member write endpoint
// (PATCH members/{userId}). The payload is a single role_id UUID; this guards
// the decoder from an unbounded body. Mirrors maxRoleBodyBytes.
const maxMemberBodyBytes = 64 * 1024

// poolBeginner is the minimal interface the handler needs from a pgx pool:
// only BeginTx is required. Both *pgxpool.Pool and pgxmock.PgxPoolIface
// satisfy this interface, so tests can inject a mock pool.
type poolBeginner = service.PgxBeginner

// memberCacheInvalidator is the narrow cache interface the handler needs.
// Using an interface instead of *authz.Cache directly lets tests inject a
// mock invalidator without constructing a real Cache with a real loader.
type memberCacheInvalidator interface {
	InvalidateMember(businessID, userID uuid.UUID)
}

// MembersHandler implements MEMBER-01..06:
//
//	GET    /businesses/{id}/members          → ListMembers   (MEMBER-01)
//	PATCH  /businesses/{id}/members/{userId} → UpdateMemberRole (MEMBER-02 + MEMBER-05 + MEMBER-06)
//	DELETE /businesses/{id}/members/{userId} → RemoveMember  (MEMBER-03 + MEMBER-04 + MEMBER-05)
type MembersHandler struct {
	mutations       *service.MemberMutationService
	businessService businessGetter
}

// NewMembersHandler constructs a MembersHandler. All dependencies are required.
//
// adds `auditLogger` so PATCH/DELETE member endpoints
// can emit rbac.role_granted / rbac.member_removed audit events AFTER the
// underlying transaction commits. businessService lets the write endpoints
// reject mutations against a soft-deleted (erasure-pending) organization that
// RequireBusinessAccess (membership-only) does not gate.
func NewMembersHandler(
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
	ir domain.InvitationRepository,
	bs businessGetter,
	pool poolBeginner,
	inv memberCacheInvalidator,
	auditLogger audit.Logger,
) (*MembersHandler, error) {
	if mr == nil {
		return nil, fmt.Errorf("NewMembersHandler: membershipRepo cannot be nil")
	}
	if rr == nil {
		return nil, fmt.Errorf("NewMembersHandler: roleRepo cannot be nil")
	}
	if ur == nil {
		return nil, fmt.Errorf("NewMembersHandler: userRepo cannot be nil")
	}
	if ir == nil {
		return nil, fmt.Errorf("NewMembersHandler: invitationRepo cannot be nil")
	}
	if bs == nil {
		return nil, fmt.Errorf("NewMembersHandler: businessService cannot be nil")
	}
	if pool == nil {
		return nil, fmt.Errorf("NewMembersHandler: pool cannot be nil")
	}
	if inv == nil {
		return nil, fmt.Errorf("NewMembersHandler: invalidator cannot be nil")
	}
	if auditLogger == nil {
		return nil, fmt.Errorf("NewMembersHandler: auditLogger cannot be nil")
	}
	return &MembersHandler{
		mutations:       service.NewMemberMutationService(mr, rr, ur, ir, pool, inv, auditLogger),
		businessService: bs,
	}, nil
}

// domainMemberToOpenAPI maps the (membership, user, role) domain triple into
// the spec-side Member wire shape. JoinedAt / InvitedAt are normalized to UTC
// so JSON time-zone is stable across hosts (oapi-codegen emits time.Time which
// serializes as RFC3339Nano in UTC).
func domainMemberToOpenAPI(m domain.BusinessMember, user *domain.User, role *domain.Role) openapi.Member {
	out := openapi.Member{
		Status:    m.Status,
		JoinedAt:  m.JoinedAt.UTC(),
		InvitedBy: m.InvitedBy,
	}
	out.User.Id = user.ID
	out.User.Email = openapi_types.Email(user.Email)
	if user.Name != "" {
		name := user.Name
		out.User.Name = &name
	}
	out.Role.Id = role.ID
	out.Role.Name = role.Name
	out.Role.Permissions = role.Permissions
	if m.InvitedAt != nil {
		t := m.InvitedAt.UTC()
		out.InvitedAt = &t
	}
	return out
}

// ListMembers handles GET /api/v1/businesses/{id}/members.
// Includes members in their account deletion grace window.
func (h *MembersHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "", authz.PermMembersRead)
	if !ok {
		return
	}

	members, err := h.mutations.ListMembers(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_members", err)
		return
	}
	out := make([]openapi.Member, 0, len(members))
	for _, m := range members {
		out = append(out, domainMemberToOpenAPI(m.Member, m.User, m.Role))
	}

	writeJSON(w, http.StatusOK, out)
}

// UpdateMemberRole handles PATCH /api/v1/businesses/{id}/members/{userId}.
// The membership service validates the role and commits the mutation with invitation revocation.
func (h *MembersHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "", authz.PermMembersUpdateRole)
	if !ok {
		return
	}
	targetUserID, ok := parseMemberUserIDParam(w, r)
	if !ok {
		return
	}

	if _, err := h.businessService.GetByID(r.Context(), bc.BusinessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "update member role: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if targetUserID == bc.UserID {
		writeJSONError(w, http.StatusForbidden, "cannot_change_own_role")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMemberBodyBytes)
	var req openapi.UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if req.RoleId == uuid.Nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	m, err := h.mutations.UpdateMemberRole(r.Context(), bc, targetUserID, req.RoleId)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
			return
		}
		writeAuthzInvariantError(r.Context(), w, "update_member_role", err)
		return
	}

	writeJSON(w, http.StatusOK, openapi.UpdateMemberRoleResponse{
		BusinessId:    m.BusinessID,
		UserId:        m.UserID,
		RoleId:        m.RoleID,
		Status:        m.Status,
		RoleChangedAt: m.RoleChangedAt,
		RoleChangedBy: m.RoleChangedBy,
	})
}

// RemoveMember handles DELETE /api/v1/businesses/{id}/members/{userId}.
// Self-removal is permission-exempt but remains subject to the service last-owner check.
func (h *MembersHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	bc, ok := businessContext(w, r, "")
	if !ok {
		return
	}
	targetUserID, ok := parseMemberUserIDParam(w, r)
	if !ok {
		return
	}

	if _, err := h.businessService.GetByID(r.Context(), bc.BusinessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "remove member: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if targetUserID != bc.UserID {
		if !authz.Can(r.Context(), authz.PermMembersRemove) {
			writeJSONError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	err := h.mutations.RemoveMember(r.Context(), bc, targetUserID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "RemoveMember", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseMemberUserIDParam extracts and validates the {userId} URL param.
// Writes 400 invalid_user_id and returns false on parse failure.
func parseMemberUserIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return parseUUIDParam(w, r, "userId", "invalid_user_id")
}
