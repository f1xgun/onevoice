package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// poolBeginner is the minimal interface the handler needs from a pgx pool:
// only BeginTx is required. Both *pgxpool.Pool and pgxmock.PgxPoolIface
// satisfy this interface, so tests can inject a mock pool.
type poolBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

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
	membershipRepo  domain.BusinessMembershipRepository
	roleRepo        domain.RoleRepository
	userRepo        domain.UserRepository
	businessService businessGetter
	pool            poolBeginner
	invalidator     memberCacheInvalidator
	audit           audit.Logger
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
		membershipRepo:  mr,
		roleRepo:        rr,
		userRepo:        ur,
		businessService: bs,
		pool:            pool,
		invalidator:     inv,
		audit:           auditLogger,
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
// Permission: PermMembersRead. SPEC MEMBER-01.
func (h *MembersHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "", authz.PermMembersRead)
	if !ok {
		return
	}

	members, err := h.membershipRepo.ListByBusiness(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_members", fmt.Errorf("list members: %w", err))
		return
	}

	out := make([]openapi.Member, 0, len(members))
	for _, m := range members {
		user, err := h.userRepo.GetByID(r.Context(), m.UserID)
		if err != nil {
			writeAuthzInvariantError(r.Context(), w, "list_members.user_lookup", err)
			return
		}
		role, err := h.roleRepo.GetByID(r.Context(), m.RoleID)
		if err != nil {
			writeAuthzInvariantError(r.Context(), w, "list_members.role_lookup", err)
			return
		}
		out = append(out, domainMemberToOpenAPI(m, user, role))
	}

	writeJSON(w, http.StatusOK, out)
}

// UpdateMemberRole handles PATCH /api/v1/businesses/{id}/members/{userId}
// (PermMembersUpdateRole).
//
// Order:
//  1. Open RepeatableRead tx
//  2. EnsureOwnerExistsAfter (SELECT FOR UPDATE serializes concurrent demotes)
//  3. repo.UpdateRole (sets role_changed_at + role_changed_by)
//  4. tx.Commit
//  5. cache.InvalidateMember — AFTER commit, never before
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

	var req openapi.UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if req.RoleId == uuid.Nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	role, err := h.roleRepo.GetByID(r.Context(), req.RoleId)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
			return
		}
		writeAuthzInvariantError(r.Context(), w, "update_member_role.role_lookup", err)
		return
	}
	if role.BusinessID != nil && *role.BusinessID != bc.BusinessID {
		writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
		return
	}

	rolePerms := make([]authz.Permission, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		rolePerms = append(rolePerms, authz.Permission(p))
	}
	if err := authz.CheckEscalationSubset(bc.RoleID, bc.Permissions, rolePerms); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.escalation", err)
		return
	}

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	change := authz.OwnerChange{
		Kind:         authz.OwnerChangeDemote,
		MemberUserID: &targetUserID,
	}
	if err := authz.EnsureOwnerExistsAfter(r.Context(), tx, bc.BusinessID, change); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.invariant", err)
		return
	}

	if err := h.membershipRepo.UpdateRoleInTx(r.Context(), tx, bc.BusinessID, targetUserID, req.RoleId, bc.UserID); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.update", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.commit", err)
		return
	}
	committed = true

	h.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	audit.LogRoleGranted(r.Context(), h.audit, bc.BusinessID, bc.UserID, targetUserID, req.RoleId, nil)

	slog.InfoContext(r.Context(), "member role updated",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"new_role_id", req.RoleId,
	)

	m, err := h.membershipRepo.GetByBusinessUser(r.Context(), bc.BusinessID, targetUserID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.read_back", err)
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

// RemoveMember handles DELETE /api/v1/businesses/{id}/members/{userId}
// (PermMembersRemove, self-removal exempt but still subject to last-owner check).
//
// Order:
//  1. Open RepeatableRead tx
//  2. EnsureOwnerExistsAfter (SELECT FOR UPDATE serializes concurrent removes)
//  3. repo.DeleteInTx (DELETE inside the same tx)
//  4. tx.Commit
//  5. cache.InvalidateMember — AFTER commit, never before
//
// Returns 204 No Content on success.
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

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	change := authz.OwnerChange{
		Kind:         authz.OwnerChangeRemove,
		MemberUserID: &targetUserID,
	}
	if err := authz.EnsureOwnerExistsAfter(r.Context(), tx, bc.BusinessID, change); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.invariant", err)
		return
	}

	if err := h.membershipRepo.DeleteInTx(r.Context(), tx, bc.BusinessID, targetUserID); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.delete", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.commit", err)
		return
	}
	committed = true

	h.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	audit.LogMemberRemoved(r.Context(), h.audit, bc.BusinessID, bc.UserID, targetUserID, targetUserID == bc.UserID)

	slog.InfoContext(r.Context(), "member removed",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"self_removal", targetUserID == bc.UserID,
	)

	w.WriteHeader(http.StatusNoContent)
}

// parseMemberUserIDParam extracts and validates the {userId} URL param.
// Writes 400 invalid_user_id and returns false on parse failure.
func parseMemberUserIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return parseUUIDParam(w, r, "userId", "invalid_user_id")
}
