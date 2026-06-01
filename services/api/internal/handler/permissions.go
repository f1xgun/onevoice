package handler

import (
	"net/http"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// PermissionsHandler exposes the static permission registry as a JSON
// endpoint. ships GET /api/v1/permissions; the endpoint sits
// behind the existing authMiddleware (any authenticated user) per
// AUTHZ-01 / CONTEXT decision.
//
// The handler holds no state: AllPermissions returns a fresh slice on
// every call and the body is computed in the request goroutine.
type PermissionsHandler struct{}

// NewPermissionsHandler constructs a zero-dependency handler. It exists
// for symmetry with the project's NewXxxHandler convention; future
// versions (descriptions) may take dependencies and benefit from
// the seam.
func NewPermissionsHandler() *PermissionsHandler {
	return &PermissionsHandler{}
}

// authzGroupsToOpenAPI maps the typed authz registry groups into the
// spec-side PermissionRegistryResponse wire shape. The handler wraps the
// list under "groups" so future fields (e.g. version, etag) can be added
// without breaking the wire shape; a bare top-level array is an OWASP
// anti-pattern.
func authzGroupsToOpenAPI(groups []authz.PermissionGroup) openapi.PermissionRegistryResponse {
	out := openapi.PermissionRegistryResponse{
		Groups: make([]openapi.PermissionGroup, 0, len(groups)),
	}
	for _, g := range groups {
		perms := make([]openapi.Permission, 0, len(g.Permissions))
		for _, p := range g.Permissions {
			perms = append(perms, openapi.Permission{
				Name:        string(p.Name),
				Description: p.Description,
			})
		}
		out.Groups = append(out.Groups, openapi.PermissionGroup{
			Resource:    g.Resource,
			Permissions: perms,
		})
	}
	return out
}

// List returns the permission registry as JSON. Auth is enforced upstream
// by the existing authMiddleware (router wiring in Plan G).
//
// Descriptions are localized per request locale via the i18n catalog
// (permissions.<resource>.<action>.desc). The registry's hardcoded RU
// Description is the fallback when a catalog key is absent.
//
// GET /api/v1/permissions
//
//	200 -> openapi.PermissionRegistryResponse
//	401 -> handled by authMiddleware before we get here
func (h *PermissionsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authzGroupsToOpenAPI(localizedPermissions(r)))
}

// localizedPermissions copies the registry and overrides each Description
// with the catalog value for the request locale. The copy is shallow per
// group but rebuilds the Permissions slices so the shared registry returned
// by AllPermissions is never mutated.
func localizedPermissions(r *http.Request) []authz.PermissionGroup {
	ctx := r.Context()
	groups := authz.AllPermissions()
	out := make([]authz.PermissionGroup, len(groups))
	for i, g := range groups {
		perms := make([]authz.PermissionMeta, len(g.Permissions))
		for j, p := range g.Permissions {
			key := "permissions." + string(p.Name) + ".desc"
			if desc := i18n.Tr(ctx, key); desc != key {
				p.Description = desc
			}
			perms[j] = p
		}
		out[i] = authz.PermissionGroup{Resource: g.Resource, Permissions: perms}
	}
	return out
}
