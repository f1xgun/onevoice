package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// roleCacheInvalidator is the narrow cache interface RolesHandler needs.
// InvalidateMember handles the fanout after role-delete-with-reassign: the
// membership cache still holds the OLD role_id until evicted.
type roleCacheInvalidator interface {
	InvalidateRole(businessID, roleID uuid.UUID)
	InvalidateMember(businessID, userID uuid.UUID)
}

// RolesHandler serves the business role CRUD + MyPermissions endpoints.
// See docs/api/handlers/roles.md.
type RolesHandler struct {
	roleRepo       domain.RoleRepository
	membershipRepo domain.BusinessMembershipRepository
	pool           poolBeginner
	invalidator    roleCacheInvalidator
	audit          audit.Logger
}

// NewRolesHandler constructs a RolesHandler; every dep is required.
func NewRolesHandler(
	rr domain.RoleRepository,
	mr domain.BusinessMembershipRepository,
	pool poolBeginner,
	inv roleCacheInvalidator,
	auditLogger audit.Logger,
) (*RolesHandler, error) {
	if rr == nil {
		return nil, fmt.Errorf("NewRolesHandler: roleRepo cannot be nil")
	}
	if mr == nil {
		return nil, fmt.Errorf("NewRolesHandler: membershipRepo cannot be nil")
	}
	if pool == nil {
		return nil, fmt.Errorf("NewRolesHandler: pool cannot be nil")
	}
	if inv == nil {
		return nil, fmt.Errorf("NewRolesHandler: invalidator cannot be nil")
	}
	if auditLogger == nil {
		return nil, fmt.Errorf("NewRolesHandler: auditLogger cannot be nil")
	}
	return &RolesHandler{
		roleRepo:       rr,
		membershipRepo: mr,
		pool:           pool,
		invalidator:    inv,
		audit:          auditLogger,
	}, nil
}

// domainRoleToOpenAPI maps a domain.Role into the spec-side openapi.Role.
// memberCount is a pointer so List can populate it and Create/Update omit it
// (Role.MemberCount has json:"member_count,omitempty").
func domainRoleToOpenAPI(r *domain.Role, memberCount *int) openapi.Role {
	return openapi.Role{
		Id:          r.ID,
		BusinessId:  r.BusinessID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: r.Permissions,
		IsSystem:    r.IsSystem,
		MemberCount: memberCount,
	}
}

// maxPermissionsPerRole caps the permissions[] array on POST/PATCH to bound
// serialization cost (the static registry has <100 perms).
const maxPermissionsPerRole = 100

// List handles GET /api/v1/businesses/{id}/roles (PermRolesRead).
// See docs/api/handlers/roles.md.
func (h *RolesHandler) List(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermRolesRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	rows, err := h.roleRepo.ListByBusinessWithCounts(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_roles", fmt.Errorf("list roles: %w", err))
		return
	}

	out := make([]openapi.Role, 0, len(rows))
	for _, row := range rows {
		count := row.MemberCount
		role := row.Role
		out = append(out, domainRoleToOpenAPI(&role, &count))
	}

	writeJSON(w, http.StatusOK, out)
}

// Create handles POST /api/v1/businesses/{id}/roles (PermRolesCreate).
// See docs/api/handlers/roles.md.
//
// Note: `?clone_from=` is intentionally ignored server-side — the frontend
// pre-fills permissions on the client and POSTs the result here.
func (h *RolesHandler) Create(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermRolesCreate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	if len(req.Permissions) > maxPermissionsPerRole {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	proposed, err := toTypedPerms(req.Permissions)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_permission")
		return
	}
	if err := authz.CheckEscalationSubset(bc.RoleID, bc.Permissions, proposed); err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_role.subset", err)
		return
	}

	// Persist the deduplicated slice — duplicates must not leak into JSONB.
	dedupedPerms := typedPermsToStrings(proposed)

	businessID := bc.BusinessID
	role := &domain.Role{
		BusinessID:  &businessID,
		Name:        name,
		Description: strDeref(req.Description),
		Permissions: dedupedPerms,
		IsSystem:    false,
		CreatedBy:   &bc.UserID,
		UpdatedBy:   &bc.UserID,
	}

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_role.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	if err := h.roleRepo.CreateInTx(r.Context(), tx, role); err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_role.exec", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_role.commit", err)
		return
	}
	committed = true

	// No InvalidateRole on Create — no existing memberships reference this
	// brand-new role, so no cache entry can be stale.

	audit.LogRoleCreated(r.Context(), h.audit, bc.BusinessID, bc.UserID, role.ID, role.Name, role.Permissions)

	slog.InfoContext(r.Context(), "role created",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"role_id", role.ID,
		"role_name", role.Name,
	)

	writeJSON(w, http.StatusCreated, domainRoleToOpenAPI(role, nil))
}

// Update handles PATCH /api/v1/businesses/{id}/roles/{roleId} (PermRolesUpdate).
// See docs/api/handlers/roles.md.
func (h *RolesHandler) Update(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermRolesUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	roleID, ok := parseRoleIDParam(w, r)
	if !ok {
		return
	}

	var req openapi.UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	if len(req.Permissions) > maxPermissionsPerRole {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	proposed, err := toTypedPerms(req.Permissions)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_permission")
		return
	}

	existing, err := h.roleRepo.GetByID(r.Context(), roleID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.lookup", err)
		return
	}
	// Cross-tenant masquerade as 404 — runs BEFORE CheckSystemRoleImmutable so
	// roles owned by another business never reveal themselves as "system" 422.
	if existing.BusinessID != nil && *existing.BusinessID != bc.BusinessID {
		writeJSONError(w, http.StatusNotFound, "role_not_found")
		return
	}
	if err := authz.CheckSystemRoleImmutable(existing); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.immutable", err)
		return
	}
	if err := authz.CheckEscalationSubset(bc.RoleID, bc.Permissions, proposed); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.subset", err)
		return
	}
	if err := authz.CheckSelfLockout(bc.UserID, bc.RoleID, roleID, proposed); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.lockout", err)
		return
	}

	existing.Name = name
	existing.Description = strDeref(req.Description)
	// Persist deduplicated slice — reads must never observe duplicates.
	existing.Permissions = typedPermsToStrings(proposed)
	existing.UpdatedBy = &bc.UserID

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	if err := h.roleRepo.UpdateInTx(r.Context(), tx, existing); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.exec", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "update_role.commit", err)
		return
	}
	committed = true

	// InvalidateRole AFTER commit only — pre-commit eviction would cache
	// stale-then-rolled-back permissions for up to the ~30s TTL.
	h.invalidator.InvalidateRole(bc.BusinessID, roleID)

	audit.LogRoleUpdated(r.Context(), h.audit, bc.BusinessID, bc.UserID, roleID, existing.Name, existing.Permissions)

	slog.InfoContext(r.Context(), "role updated",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"role_id", roleID,
	)

	writeJSON(w, http.StatusOK, domainRoleToOpenAPI(existing, nil))
}

// Delete handles DELETE /api/v1/businesses/{id}/roles/{roleId}?reassign_to=<uuid> (PermRolesDelete).
// See docs/api/handlers/roles.md.
func (h *RolesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermRolesDelete) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	roleID, ok := parseRoleIDParam(w, r)
	if !ok {
		return
	}

	var reassignTo *uuid.UUID
	if s := r.URL.Query().Get("reassign_to"); s != "" {
		parsed, err := uuid.Parse(s)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_reassign_to")
			return
		}
		if parsed == roleID {
			// Self-reassign would orphan members on the doomed role.
			writeJSONError(w, http.StatusBadRequest, "invalid_reassign_to")
			return
		}
		reassignTo = &parsed
	}

	existing, err := h.roleRepo.GetByID(r.Context(), roleID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "delete_role.lookup", err)
		return
	}
	if existing.BusinessID != nil && *existing.BusinessID != bc.BusinessID {
		// Cross-tenant rejection masquerades as 404.
		writeJSONError(w, http.StatusNotFound, "role_not_found")
		return
	}
	if err := authz.CheckSystemRoleImmutable(existing); err != nil {
		writeAuthzInvariantError(r.Context(), w, "delete_role.immutable", err)
		return
	}

	memberCount, err := h.roleRepo.CountMembersByRole(r.Context(), bc.BusinessID, roleID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "delete_role.count", err)
		return
	}
	if memberCount > 0 && reassignTo == nil {
		// Role-in-use: refuse before opening the tx (no `?reassign_to=` supplied).
		writeAuthzInvariantError(r.Context(), w, "delete_role.in_use", domain.ErrRoleInUse)
		return
	}

	// Validate target role: exists, in-tenant, AND grantable by the actor.
	if reassignTo != nil && memberCount > 0 {
		target, err := h.roleRepo.GetByID(r.Context(), *reassignTo)
		if err != nil {
			if errors.Is(err, domain.ErrRoleNotFound) {
				writeJSONError(w, http.StatusBadRequest, "invalid_reassign_to")
				return
			}
			writeAuthzInvariantError(r.Context(), w, "delete_role.target_lookup", err)
			return
		}
		// Target must be system (BusinessID == nil) OR belong to this business.
		if target.BusinessID != nil && *target.BusinessID != bc.BusinessID {
			writeJSONError(w, http.StatusBadRequest, "invalid_reassign_to")
			return
		}
		targetPerms, err := toTypedPerms(target.Permissions)
		if err != nil {
			writeAuthzInvariantError(r.Context(), w, "delete_role.target_perms", err)
			return
		}
		if err := authz.CheckEscalationSubset(bc.RoleID, bc.Permissions, targetPerms); err != nil {
			writeAuthzInvariantError(r.Context(), w, "delete_role.target_subset", err)
			return
		}
	}

	// Capture affected user IDs BEFORE the tx — the DELETE deletes the rows,
	// so reading after commit returns the NEW role_id. Needed for the per-member
	// membership cache fanout after commit.
	var affectedUserIDs []uuid.UUID
	if memberCount > 0 && reassignTo != nil {
		affectedUserIDs, err = h.membershipRepo.ListUserIDsByRole(r.Context(), bc.BusinessID, roleID)
		if err != nil {
			writeAuthzInvariantError(r.Context(), w, "delete_role.list_affected", err)
			return
		}
	}

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "delete_role.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	if memberCount > 0 {
		if err := h.roleRepo.DeleteWithReassignInTx(r.Context(), tx, bc.BusinessID, roleID, *reassignTo, bc.UserID); err != nil {
			writeAuthzInvariantError(r.Context(), w, "delete_role.reassign_exec", err)
			return
		}
	} else {
		if err := h.roleRepo.DeleteInTx(r.Context(), tx, roleID); err != nil {
			writeAuthzInvariantError(r.Context(), w, "delete_role.exec", err)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "delete_role.commit", err)
		return
	}
	committed = true

	// InvalidateRole AFTER commit only — pre-commit eviction would cache stale state.
	h.invalidator.InvalidateRole(bc.BusinessID, roleID)
	// InvalidateRole evicts only the role-perms cache; the membership cache
	// still pins the OLD role_id. Fanout so the next Can pulls fresh membership.
	for _, uid := range affectedUserIDs {
		h.invalidator.InvalidateMember(bc.BusinessID, uid)
	}

	audit.LogRoleDeleted(r.Context(), h.audit, bc.BusinessID, bc.UserID, roleID, existing.Name, reassignTo, memberCount)

	slog.InfoContext(r.Context(), "role deleted",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"role_id", roleID,
		"member_count", memberCount,
		"reassigned_to", reassignTo,
		"affected_users", len(affectedUserIDs),
	)

	w.WriteHeader(http.StatusNoContent)
}

// MyPermissions handles GET /api/v1/businesses/{id}/me/permissions.
// See docs/api/handlers/roles.md.
//
// No additional authz.Can gate — BY DESIGN. RequireBusinessAccess on the parent
// route already rejects non-members (404) and suspended members (403); any
// active member can read their own permissions.
func (h *RolesHandler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	// Defensive copy — bc.Permissions backs the middleware LRU cache; aliasing
	// it into the JSON encoder would race with cache mutations.
	perms := make([]string, len(bc.Permissions))
	for i, p := range bc.Permissions {
		perms[i] = string(p)
	}
	writeJSON(w, http.StatusOK, openapi.MyPermissionsResponse{Permissions: perms})
}

// parseRoleIDParam extracts {roleId}; writes 400 invalid_role_id on failure.
func parseRoleIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "roleId")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
		return uuid.Nil, false
	}
	return id, true
}

// typedPermsToStrings converts []authz.Permission back to []string for persistence.
func typedPermsToStrings(perms []authz.Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}

// toTypedPerms validates entries against the authz registry and deduplicates
// (first-occurrence wins). Returns (nil, err) on unknown entry; caller maps to 400.
func toTypedPerms(strs []string) ([]authz.Permission, error) {
	valid := make(map[string]struct{})
	for _, group := range authz.AllPermissions() {
		for _, meta := range group.Permissions {
			valid[string(meta.Name)] = struct{}{}
		}
	}
	out := make([]authz.Permission, 0, len(strs))
	seen := make(map[string]struct{}, len(strs))
	for _, s := range strs {
		if _, ok := valid[s]; !ok {
			return nil, fmt.Errorf("unknown permission: %q", s)
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, authz.Permission(s))
	}
	return out, nil
}
