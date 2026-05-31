package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPermissionsHandler_List_HappyPath asserts the registry endpoint shape:
// 200 OK, application/json, {"groups": [...]}, seven PermissionGroups in
// declared order (business, members, roles, integrations, content, billing,
// audit), and that the first group's first permission is `business.read`
// with a non-empty Russian description (populated all descriptions
// for tooltip UX; added the `audit` group).
//
// This is the only test the plan specifies — auth/401-path testing belongs to
// authMiddleware (covered in its own tests) and the route wiring is Plan G.
func TestPermissionsHandler_List_HappyPath(t *testing.T) {
	h := NewPermissionsHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", http.NoBody)
	w := httptest.NewRecorder()

	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body struct {
		Groups []struct {
			Resource    string `json:"resource"`
			Permissions []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"permissions"`
		} `json:"groups"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))

	require.Len(t, body.Groups, 7, "expect 7 PermissionGroups (business, members, roles, integrations, content, billing, audit)")
	assert.Equal(t, "business", body.Groups[0].Resource)
	assert.Equal(t, "members", body.Groups[1].Resource)
	assert.Equal(t, "roles", body.Groups[2].Resource)
	assert.Equal(t, "integrations", body.Groups[3].Resource)
	assert.Equal(t, "content", body.Groups[4].Resource)
	assert.Equal(t, "billing", body.Groups[5].Resource)
	assert.Equal(t, "audit", body.Groups[6].Resource)

	// Sample permission name format — first business permission must be
	// `business.read` per pkg/authz.AllPermissions declaration order.
	require.NotEmpty(t, body.Groups[0].Permissions)
	assert.Equal(t, "business.read", body.Groups[0].Permissions[0].Name)
	assert.NotEmpty(t, body.Groups[0].Permissions[0].Description, "descriptions populated for tooltip UX")

	// audit group has exactly one permission, audit.read.
	require.Len(t, body.Groups[6].Permissions, 1, "audit group has exactly one permission")
	assert.Equal(t, "audit.read", body.Groups[6].Permissions[0].Name)
	assert.NotEmpty(t, body.Groups[6].Permissions[0].Description, "audit.read must have a Russian description for tooltip UX")
}
