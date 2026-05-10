package handler

import (
	"net/http"

	"github.com/f1xgun/onevoice/pkg/authz"
)

// PermissionsHandler exposes the static permission registry as a JSON
// endpoint. Phase 1 ships GET /api/v1/permissions; the endpoint sits
// behind the existing authMiddleware (any authenticated user) per
// AUTHZ-01 / CONTEXT decision D-15.
//
// The handler holds no state: AllPermissions() returns a fresh slice on
// every call and the body is computed in the request goroutine.
type PermissionsHandler struct{}

// NewPermissionsHandler constructs a zero-dependency handler. It exists
// for symmetry with the project's NewXxxHandler convention; future
// versions (Phase 5 descriptions) may take dependencies and benefit from
// the seam.
func NewPermissionsHandler() *PermissionsHandler {
	return &PermissionsHandler{}
}

// permissionsResponse wraps the registry under "groups" so future fields
// (e.g. version, etag) can be added without breaking the wire shape. A
// bare top-level array is an OWASP anti-pattern; the wrapper also gives
// frontend consumers a stable place to bind `data.groups` against.
type permissionsResponse struct {
	Groups []authz.PermissionGroup `json:"groups"`
}

// List returns the permission registry as JSON. Auth is enforced upstream
// by the existing authMiddleware (router wiring in Plan G).
//
// GET /api/v1/permissions
//
//	200 -> permissionsResponse
//	401 -> handled by authMiddleware before we get here
func (h *PermissionsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, permissionsResponse{Groups: authz.AllPermissions()})
}
