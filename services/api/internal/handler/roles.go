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
)

// roleCacheInvalidator is the narrow cache interface RolesHandler needs.
// InvalidateRole evicts the role-perms LRU entry; InvalidateMember is used by
// Delete after reassigning members to a new role (the membership cache still
// holds the OLD role_id until evicted — Open Question A2 fanout).
//
// *authz.Cache satisfies both methods, so wire/handlers.go can pass the same
// *authz.Cache value into both NewMembersHandler and NewRolesHandler.
type roleCacheInvalidator interface {
	InvalidateRole(businessID, roleID uuid.UUID)
	InvalidateMember(businessID, userID uuid.UUID)
}

// RolesHandler implements ROLE-03..07 + UI-RBAC-08:
//
//	GET    /businesses/{id}/roles                → List           (ROLE-03 + member_count)
//	POST   /businesses/{id}/roles                → Create         (ROLE-04)
//	PATCH  /businesses/{id}/roles/{roleId}       → Update         (ROLE-05)
//	DELETE /businesses/{id}/roles/{roleId}       → Delete         (ROLE-06)
//	GET    /businesses/{id}/me/permissions       → MyPermissions  (UI-RBAC-08)
//
// Response wire-shape discriminator (MED-05 review):
// - List returns roleResponseItem rows with member_count populated
// (including 0 for unused roles).
// - Create / Update return roleResponseItem WITHOUT member_count — a
// fresh role has 0 members and an updated role's count is unchanged,
// so the field is omitted entirely via `omitempty` on the *int pointer.
// - Description is plain `string` (no `omitempty`) — always present in
// every response, including the empty string. The frontend zod schema
// (`description: z.string.optional.default(”)`) accepts both
// "missing" and "" — backend always sends "" for consistency.
type RolesHandler struct {
	roleRepo       domain.RoleRepository
	membershipRepo domain.BusinessMembershipRepository
	pool           poolBeginner
	invalidator    roleCacheInvalidator
	audit          audit.Logger
}

// NewRolesHandler constructs a RolesHandler. All dependencies are required.
//
// adds `auditLogger` so role CRUD endpoints emit
// rbac.role_created / role_updated / role_deleted audit events AFTER
// tx.Commit succeeds.
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

// roleResponseItem is the wire shape for a single role. MemberCount is a
// pointer so List (which has counts) can populate it while Create/Update
// (which don't) can omit it via the `omitempty` tag.
type roleResponseItem struct {
	ID          uuid.UUID  `json:"id"`
	BusinessID  *uuid.UUID `json:"business_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Permissions []string   `json:"permissions"`
	IsSystem    bool       `json:"is_system"`
	MemberCount *int       `json:"member_count,omitempty"`
}

// maxPermissionsPerRole caps the permissions[] array on POST/PATCH at 100 to
// bound serialization cost and protect against accidental/malicious bloat.
// The full registry today has well under 100 permissions (09).
const maxPermissionsPerRole = 100

// List handles GET /api/v1/businesses/{id}/roles.
// Permission: PermRolesRead. SPEC ROLE-03 + UI-RBAC-08 (member_count column).
// Returns system roles (business_id IS NULL) + custom roles for this business,
// ordered by is_system DESC, name ASC. Each row carries member_count via the
// ListByBusinessWithCounts LEFT JOIN.
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

	out := make([]roleResponseItem, 0, len(rows))
	for _, row := range rows {
		count := row.MemberCount
		out = append(out, roleResponseItem{
			ID:          row.ID,
			BusinessID:  row.BusinessID,
			Name:        row.Name,
			Description: row.Description,
			Permissions: row.Permissions,
			IsSystem:    row.IsSystem,
			MemberCount: &count,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// createRoleRequest is the body shape for POST /roles.
type createRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// Create handles POST /api/v1/businesses/{id}/roles. Permission: PermRolesCreate.
//
// Order: authz.Can → decode → validation → CheckEscalationSubset (no self-lockout
// since actor doesn't hold the new role) → RepeatableRead tx → CreateInTx → commit.
// No InvalidateRole on create — no existing memberships reference this role.
//
// The query param `?clone_from=` is IGNORED
// server-side. The frontend handles all clone semantics:
// when the user picks a source role to clone, the editor pre-fills the
// permissions array on the client and then POSTs the result here exactly
// as if the user had built it from scratch. body.permissions is the
// authoritative source; CheckEscalationSubset already gates the security
// envelope. The backend does NOT parse `?clone_from=` — a malformed value
// is silently accepted. Documented for frontend symmetry only; remove if
// a future contract change splits cloning into a dedicated POST route.
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

	var req createRoleRequest
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

	// MED-04 (review): persist the deduplicated slice (mirrors
	// `proposed`), not the raw request — duplicate keys must not leak into
	// the JSONB column. typedPermsToStrings preserves toTypedPerms' order.
	dedupedPerms := typedPermsToStrings(proposed)

	businessID := bc.BusinessID
	role := &domain.Role{
		BusinessID:  &businessID,
		Name:        name,
		Description: req.Description,
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

	// emit rbac.role_created AFTER tx.Commit.
	// role.Permissions is the deduplicated slice persisted to JSONB.
	audit.LogRoleCreated(r.Context(), h.audit, bc.BusinessID, bc.UserID, role.ID, role.Name, role.Permissions)

	slog.InfoContext(r.Context(), "role created",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"role_id", role.ID,
		"role_name", role.Name,
	)

	writeJSON(w, http.StatusCreated, roleResponseItem{
		ID:          role.ID,
		BusinessID:  role.BusinessID,
		Name:        role.Name,
		Description: role.Description,
		Permissions: role.Permissions,
		IsSystem:    role.IsSystem,
	})
}

// updateRoleRequest is the body shape for PATCH /roles/{roleId}.
type updateRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// Update handles PATCH /api/v1/businesses/{id}/roles/{roleId}. Permission: PermRolesUpdate.
//
// Critical ordering: InvalidateRole runs AFTER tx.Commit only — invalidating before
// commit would expose stale-then-rolled-back permissions on cache miss.
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

	var req updateRoleRequest
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
	// Tenant isolation (pattern from members.go:200-203). System roles
	// (BusinessID == nil) fall through to CheckSystemRoleImmutable below which
	// returns 422 — but only after the cross-tenant check rules out custom
	// roles owned by a different business.
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
	existing.Description = req.Description
	// Persist the deduplicated slice so subsequent reads can't observe duplicates.
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

	// InvalidateRole MUST run after commit; otherwise the role-perms cache could
	// serve stale permissions for up to the TTL (~30s) if commit rolls back.
	h.invalidator.InvalidateRole(bc.BusinessID, roleID)

	audit.LogRoleUpdated(r.Context(), h.audit, bc.BusinessID, bc.UserID, roleID, existing.Name, existing.Permissions)

	slog.InfoContext(r.Context(), "role updated",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"role_id", roleID,
	)

	writeJSON(w, http.StatusOK, roleResponseItem{
		ID:          existing.ID,
		BusinessID:  existing.BusinessID,
		Name:        existing.Name,
		Description: existing.Description,
		Permissions: existing.Permissions,
		IsSystem:    existing.IsSystem,
	})
}

// Delete handles DELETE /api/v1/businesses/{id}/roles/{roleId}?reassign_to=<uuid>.
// Permission: PermRolesDelete. SPEC ROLE-06.
//
// affectedUserIDs MUST be captured BEFORE the tx (the DELETE deletes the rows);
// InvalidateRole + per-member InvalidateMember MUST run AFTER commit only.
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
			// -10 — self-reassign would orphan members.
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
		// -02 — cross-tenant rejection masquerades as 404.
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
		// -05 — refuse before opening tx.
		writeAuthzInvariantError(r.Context(), w, "delete_role.in_use", domain.ErrRoleInUse)
		return
	}

	// When reassignment is requested and there are members to reassign,
	// validate the target role exists, is in-tenant, and is grantable by the
	// actor (11).
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

	// Capture the user IDs that WILL be reassigned. Open Question A2:
	// InvalidateRole evicts only the role-perms entry; the per-member
	// membership cache still holds the OLD role_id. We must fanout
	// InvalidateMember per affected user AFTER commit succeeds.
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

	// RESEARCH Pitfall 2 — invalidate AFTER commit only.
	h.invalidator.InvalidateRole(bc.BusinessID, roleID)
	// Open Question A2 — InvalidateRole evicts only role-perms entries; the
	// membership cache still holds the OLD role_id. Fanout per-user
	// InvalidateMember so the next Can pulls fresh membership.
	for _, uid := range affectedUserIDs {
		h.invalidator.InvalidateMember(bc.BusinessID, uid)
	}

	// emit rbac.role_deleted AFTER tx.Commit +
	// cache invalidation. Captures blast-radius (memberCount) and where
	// members were reassigned (nil if no members held the role).
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

// myPermissionsResponse is the wire shape for GET /me/permissions.
type myPermissionsResponse struct {
	Permissions []string `json:"permissions"`
}

// MyPermissions handles GET /api/v1/businesses/{id}/me/permissions. No
// additional permission gate beyond RequireBusinessAccess — any member can
// read their own permissions. SPEC UI-RBAC-08.
//
// Open Question A6 resolution (RESEARCH Option 1): read from bc.Permissions
// directly. The middleware-loaded permission slice is already scoped to the
// actor + active business. The 60s frontend refetchInterval combined with the
// 30s server cache TTL provides the freshness signal without a fresh DB hit.
// -08 — no path to query another user.
//
// (review): the absence of an authz.Can gate here is BY
// DESIGN — any active member can read their own effective permissions. The
// RequireBusinessAccess middleware on the parent /businesses/{id} route still
// rejects non-members (404) and suspended members (403 forbidden_suspended)
// before the handler runs. See TestRBACCoverage_SuspendedMember_MyPermissions
// for the regression covering the suspended path.
func (h *RolesHandler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	// MED-03 (review): defensive copy. bc.Permissions backs the
	// middleware LRU cache slice. A future refactor that switches this loop
	// to `perms := bc.Permissions` (or otherwise aliases the slice) would
	// share the cached pointer with the JSON encoder — if the middleware
	// mutates the cached slice concurrently the encoder would race. Keep the
	// per-element copy explicit; cost is O(N) for N≤registry-size (<100).
	perms := make([]string, len(bc.Permissions))
	for i, p := range bc.Permissions {
		perms[i] = string(p)
	}
	writeJSON(w, http.StatusOK, myPermissionsResponse{Permissions: perms})
}

// parseRoleIDParam extracts and validates the {roleId} URL param. Returns
// (uuid.Nil, false) when unparseable, having already written 400.
// Mirror of parseMemberUserIDParam in members.go:358.
//
// MED-06 (review): chi.URLParam never returns an empty string for a
// param that the route pattern declares (router.go registers both PATCH and
// DELETE with `{roleId}`). The previous empty-string branch was unreachable
// and has been removed — uuid.Parse handles the empty-string case below by
// returning a parse error, which still maps to 400 invalid_role_id.
func parseRoleIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "roleId")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
		return uuid.Nil, false
	}
	return id, true
}

// typedPermsToStrings converts []authz.Permission back to []string for
// persistence. Used by Create/Update to store the deduplicated permission
// slice (MED-04 review) — toTypedPerms guarantees order-preserved
// uniqueness, so the resulting JSONB column never contains duplicates.
func typedPermsToStrings(perms []authz.Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}

// toTypedPerms validates that every entry is a known permission from the
// authz registry and returns the typed []authz.Permission slice. Returns
// (nil, error) when any entry is unknown — the caller maps this to 400
// invalid_permission.
//
// MED-04 (review): deduplicates entries before returning. A naive
// client posting permissions: ["business.read", "business.read"] would
// otherwise persist a JSONB row with duplicates — CheckEscalationSubset
// still passes (subset holds) but downstream jsonb_array_elements queries
// would double-count. Dedup is order-preserving (first occurrence wins) so
// the resulting array is stable across re-saves.
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
