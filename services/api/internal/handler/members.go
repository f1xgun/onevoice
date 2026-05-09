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

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
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
}

// NewMembersHandler constructs a MembersHandler. All dependencies are required.
func NewMembersHandler(
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
	pool poolBeginner,
	inv memberCacheInvalidator,
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
	return &MembersHandler{
		membershipRepo: mr,
		roleRepo:       rr,
		userRepo:       ur,
		pool:           pool,
		invalidator:    inv,
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

type updateMemberRoleRequest struct {
	RoleID uuid.UUID `json:"role_id"`
}

// UpdateMemberRole handles PATCH /api/v1/businesses/{id}/members/{userId}.
// Permission: PermMembersUpdateRole. SPEC MEMBER-02 + MEMBER-05 + MEMBER-06.
//
// Order of operations (SPEC AUTHZ-04 / MEMBER-05):
//  1. Open RepeatableRead tx
//  2. EnsureOwnerExistsAfter (SELECT FOR UPDATE — serializes concurrent demotes)
//  3. repo.UpdateRole (sets role_changed_at + role_changed_by — MEMBER-06)
//  4. tx.Commit()
//  5. cache.InvalidateMember (AFTER commit, never before)
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

	var req updateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if req.RoleID == uuid.Nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	// CR-01: validate role exists AND belongs to either:
	//   (a) the system role set (BusinessID == nil), OR
	//   (b) this business (BusinessID == bc.BusinessID).
	// Without this check, an admin of business A who knows a role UUID from
	// business B could assign that B-scoped role to a member of A — a
	// cross-tenant privilege escalation. Phase 5 custom roles make this
	// surface live; pre-emptively closing it here.
	role, err := h.roleRepo.GetByID(r.Context(), req.RoleID)
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
	// is immutable (Phase 5 custom roles will need to revisit this if a custom
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
	// so the mutation is serialized by the row-level lock (CR-01 fix).
	if err := h.membershipRepo.UpdateRoleInTx(r.Context(), tx, bc.BusinessID, targetUserID, req.RoleID, bc.UserID); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.update", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.commit", err)
		return
	}
	committed = true

	// SPEC AUTHZ-04 + MEMBER-05: invalidate AFTER commit, never before.
	h.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	slog.InfoContext(r.Context(), "member role updated",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"new_role_id", req.RoleID,
	)

	// Hydrate the response with the updated row.
	m, err := h.membershipRepo.GetByBusinessUser(r.Context(), bc.BusinessID, targetUserID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_member_role.read_back", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"business_id":     m.BusinessID,
		"user_id":         m.UserID,
		"role_id":         m.RoleID,
		"status":          m.Status,
		"role_changed_at": m.RoleChangedAt,
		"role_changed_by": m.RoleChangedBy,
	})
}

// RemoveMember handles DELETE /api/v1/businesses/{id}/members/{userId}.
// Permission: PermMembersRemove EXCEPT when targetUserID == bc.UserID (self-removal exempt — CONTEXT D-04 / MEMBER-04).
// Still subject to last-owner check even for self-removal (MEMBER-04 acceptance).
// SPEC MEMBER-03 + MEMBER-04 + MEMBER-05.
//
// Order of operations (SPEC AUTHZ-04 / MEMBER-05):
//  1. Open RepeatableRead tx
//  2. EnsureOwnerExistsAfter (SELECT FOR UPDATE — serializes concurrent removes)
//  3. repo.DeleteInTx (DELETE inside the same tx — G-07 fix)
//  4. tx.Commit()
//  5. cache.InvalidateMember (AFTER commit, never before — SPEC AUTHZ-04)
//
// Returns exactly 204 No Content on success (MEDIUM #8 committed contract).
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

	// Self-removal exemption: a member can always remove themselves regardless
	// of PermMembersRemove (CONTEXT D-04 / MEMBER-04). The comparison is
	// bc.UserID (JWT-validated) vs targetUserID (URL param) — T-02-17 mitigation.
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

	// DeleteInTx writes the DELETE inside the same RepeatableRead transaction as
	// EnsureOwnerExistsAfter's SELECT FOR UPDATE, preventing the pool/tx deadlock
	// (G-07 fix — mirrors CR-01 / UpdateRoleInTx in UpdateMemberRole).
	if err := h.membershipRepo.DeleteInTx(r.Context(), tx, bc.BusinessID, targetUserID); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.delete", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "remove_member.commit", err)
		return
	}
	committed = true

	// SPEC AUTHZ-04 + MEMBER-05: invalidate AFTER commit, never before.
	h.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

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
