package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	membershipRepo domain.BusinessMembershipRepository
	roleRepo       domain.RoleRepository
	userRepo       domain.UserRepository
	pool           poolBeginner
	invalidator    memberCacheInvalidator
	audit          audit.Logger
}

// NewMembersHandler constructs a MembersHandler. All dependencies are required.
//
// adds `auditLogger` so PATCH/DELETE member endpoints
// can emit rbac.role_granted / rbac.member_removed audit events AFTER the
// underlying transaction commits.
func NewMembersHandler(
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
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
		membershipRepo: mr,
		roleRepo:       rr,
		userRepo:       ur,
		pool:           pool,
		invalidator:    inv,
		audit:          auditLogger,
	}, nil
}

type memberResponseItem struct {
	User struct {
		ID    uuid.UUID `json:"id"`
		Email string    `json:"email"`
	} `json:"user"`
	Role struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		Permissions []string  `json:"permissions"`
	} `json:"role"`
	Status    string     `json:"status"`
	JoinedAt  string     `json:"joined_at"`
	InvitedBy *uuid.UUID `json:"invited_by"`
	InvitedAt *string    `json:"invited_at"`
}

// ListMembers handles GET /api/v1/businesses/{id}/members.
// Permission: PermMembersRead. SPEC MEMBER-01.
func (h *MembersHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermMembersRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	members, err := h.membershipRepo.ListByBusiness(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_members", fmt.Errorf("list members: %w", err))
		return
	}

	out := make([]memberResponseItem, 0, len(members))
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
		item := memberResponseItem{
			Status:    m.Status,
			JoinedAt:  m.JoinedAt.UTC().Format(time.RFC3339),
			InvitedBy: m.InvitedBy,
		}
		item.User.ID = user.ID
		item.User.Email = user.Email
		item.Role.ID = role.ID
		item.Role.Name = role.Name
		item.Role.Permissions = role.Permissions
		if m.InvitedAt != nil {
			s := m.InvitedAt.UTC().Format(time.RFC3339)
			item.InvitedAt = &s
		}
		out = append(out, item)
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermMembersUpdateRole) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	targetUserID, ok := parseMemberUserIDParam(w, r)
	if !ok {
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

	// validate role exists AND belongs to either:
	// (a) the system role set (BusinessID == nil), OR
	// (b) this business (BusinessID == bc.BusinessID).
	// Without this check, an admin of business A who knows a role UUID from
	// business B could assign that B-scoped role to a member of A — a
	// cross-tenant privilege escalation. custom roles make this
	// surface live; pre-emptively closing it here.
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

	// EnsureOwnerExistsAfter serializes concurrent demotes via SELECT FOR UPDATE.
	// OwnerChangeDemote: the target member is being assigned a (possibly non-owner) role.
	// RoleID is intentionally omitted: EnsureOwnerExistsAfter for OwnerChangeDemote
	// unconditionally removes the member from the owner count regardless of which
	// role they are being assigned to. This is safe because the system Owner role
	// is immutable (custom roles will need to revisit this if a custom
	// role can carry owner-level permissions).
	change := authz.OwnerChange{
		Kind:         authz.OwnerChangeDemote,
		MemberUserID: &targetUserID,
	}
	if err := authz.EnsureOwnerExistsAfter(r.Context(), tx, bc.BusinessID, change); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.invariant", err)
		return
	}

	// UpdateRoleInTx writes role_changed_at + role_changed_by inside the same
	// RepeatableRead transaction as EnsureOwnerExistsAfter's SELECT FOR UPDATE,
	// so the mutation is serialized by the row-level lock (fix).
	if err := h.membershipRepo.UpdateRoleInTx(r.Context(), tx, bc.BusinessID, targetUserID, req.RoleId, bc.UserID); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.update", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.commit", err)
		return
	}
	committed = true

	// Invalidate AFTER commit, never before.
	h.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	// oldRoleID is nil: capturing it would require either a pre-commit SELECT
	// (race window) or an UpdateRoleInTx returning the previous role_id —
	// both deferred. Actor + target + new role is the load-bearing forensic data.
	audit.LogRoleGranted(r.Context(), h.audit, bc.BusinessID, bc.UserID, targetUserID, req.RoleId, nil)

	slog.InfoContext(r.Context(), "member role updated",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"new_role_id", req.RoleId,
	)

	// Hydrate the response with the updated row.
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	targetUserID, ok := parseMemberUserIDParam(w, r)
	if !ok {
		return
	}

	// Self-removal exemption: members can always remove themselves regardless
	// of PermMembersRemove. bc.UserID is JWT-validated; targetUserID is URL.
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

	// DeleteInTx runs inside the same RepeatableRead tx as EnsureOwnerExistsAfter's
	// SELECT FOR UPDATE — prevents the pool/tx deadlock.
	if err := h.membershipRepo.DeleteInTx(r.Context(), tx, bc.BusinessID, targetUserID); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.delete", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.commit", err)
		return
	}
	committed = true

	// Invalidate AFTER commit, never before.
	h.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	// selfRemoval=true distinguishes "left the org" from "kicked".
	audit.LogMemberRemoved(r.Context(), h.audit, bc.BusinessID, bc.UserID, targetUserID, targetUserID == bc.UserID)

	slog.InfoContext(r.Context(), "member removed",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"self_removal", targetUserID == bc.UserID,
	)

	// MEDIUM #8: EXACTLY 204 No Content — no body, no writeJSON.
	w.WriteHeader(http.StatusNoContent)
}

// parseMemberUserIDParam extracts and validates the {userId} URL param.
// Writes 400 invalid_user_id and returns false on parse failure.
func parseMemberUserIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "userId")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_user_id")
		return uuid.Nil, false
	}
	return id, true
}
