package authz

import (
	"regexp"
	"testing"
)

var permNameRegex = regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)

func TestAllPermissions_GroupsAndCounts(t *testing.T) {
	groups := AllPermissions()

	wantOrder := []string{"business", "members", "roles", "integrations", "content", "billing", "audit"}
	if len(groups) != len(wantOrder) {
		t.Fatalf("AllPermissions(): got %d groups, want %d", len(groups), len(wantOrder))
	}
	for i, want := range wantOrder {
		if groups[i].Resource != want {
			t.Errorf("groups[%d].Resource = %q, want %q", i, groups[i].Resource, want)
		}
	}

	wantCounts := map[string]int{
		"business": 4, "members": 4, "roles": 4,
		"integrations": 3, "content": 4, "billing": 2,
		"audit": 1,
	}
	totalCount := 0
	for _, g := range groups {
		if got, want := len(g.Permissions), wantCounts[g.Resource]; got != want {
			t.Errorf("group %q: got %d permissions, want %d", g.Resource, got, want)
		}
		totalCount += len(g.Permissions)
	}
	if totalCount != 22 {
		t.Errorf("total permissions = %d, want 22", totalCount)
	}
}

func TestAllPermissions_NameFormat(t *testing.T) {
	for _, g := range AllPermissions() {
		for _, p := range g.Permissions {
			if !permNameRegex.MatchString(string(p.Name)) {
				t.Errorf("permission %q does not match ^[a-z_]+\\.[a-z_]+$", p.Name)
			}
			// Resource segment of name MUST match the group's Resource.
			expectedPrefix := g.Resource + "."
			if len(string(p.Name)) <= len(expectedPrefix) || string(p.Name)[:len(expectedPrefix)] != expectedPrefix {
				t.Errorf("permission %q is in group %q but resource segment differs", p.Name, g.Resource)
			}
		}
	}
}

func TestPermissionConstantsExist(t *testing.T) {
	// Compile-time: every constant referenced below must exist as an exported identifier.
	// If any constant is renamed, this test stops compiling — that's the point.
	_ = []Permission{
		PermBusinessRead, PermBusinessUpdate, PermBusinessDelete, PermBusinessTransferOwnership,
		PermMembersRead, PermMembersInvite, PermMembersRemove, PermMembersUpdateRole,
		PermRolesRead, PermRolesCreate, PermRolesUpdate, PermRolesDelete,
		PermIntegrationsRead, PermIntegrationsConnect, PermIntegrationsDisconnect,
		PermContentRead, PermContentCreate, PermContentUpdate, PermContentDelete,
		PermBillingRead, PermBillingUpdate,
		PermAuditRead,
	}
}

// TestAllPermissions_DescriptionsNotEmpty asserts every permission has a
// non-empty Description after Phase 5 fill (05-CONTEXT D-13). This is the
// CI guard against regressing UI-RBAC-09 (the PermissionTree Info tooltip
// depends on description text). Adding a new permission requires filling
// Description in permissions.go AllPermissions().
func TestAllPermissions_DescriptionsNotEmpty(t *testing.T) {
	t.Parallel()
	groups := AllPermissions()
	var empty []string
	for _, g := range groups {
		for _, p := range g.Permissions {
			if p.Description == "" {
				empty = append(empty, string(p.Name))
			}
		}
	}
	if len(empty) > 0 {
		t.Fatalf("permissions missing Description (fill in pkg/authz/permissions.go AllPermissions()): %v", empty)
	}
}
