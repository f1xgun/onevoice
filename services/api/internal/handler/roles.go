package handler

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// RolesHandler implements ROLE-03 (read-only role listing).
// Phase 5 will add Create/Update/Delete on this same struct.
type RolesHandler struct {
	roleRepo domain.RoleRepository
}

// NewRolesHandler constructs a RolesHandler. roleRepo is required.
func NewRolesHandler(rr domain.RoleRepository) (*RolesHandler, error) {
	if rr == nil {
		return nil, fmt.Errorf("NewRolesHandler: roleRepo cannot be nil")
	}
	return &RolesHandler{roleRepo: rr}, nil
}

type roleResponseItem struct {
	ID          uuid.UUID  `json:"id"`
	BusinessID  *uuid.UUID `json:"business_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Permissions []string   `json:"permissions"`
	IsSystem    bool       `json:"is_system"`
}

// List handles GET /api/v1/businesses/{id}/roles.
// Permission: PermRolesRead. SPEC ROLE-03.
// Returns system roles (business_id IS NULL) + custom roles for this business,
// ordered by is_system DESC, name ASC as delivered by the repository.
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

	roles, err := h.roleRepo.ListByBusiness(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_roles", fmt.Errorf("list roles: %w", err))
		return
	}

	out := make([]roleResponseItem, 0, len(roles))
	for _, role := range roles {
		out = append(out, roleResponseItem{
			ID:          role.ID,
			BusinessID:  role.BusinessID,
			Name:        role.Name,
			Description: role.Description,
			Permissions: role.Permissions,
			IsSystem:    role.IsSystem,
		})
	}

	writeJSON(w, http.StatusOK, out)
}
