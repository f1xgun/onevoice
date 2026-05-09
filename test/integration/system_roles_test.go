package integration

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestSystemRoles_DeterministicUUIDs asserts the seed migration installed
// the four system-role rows with the exact UUIDs declared in
// pkg/domain/system_roles.go. Boot-time drift between Go and SQL is the
// failure mode this test catches.
func TestSystemRoles_DeterministicUUIDs(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; skipping integration test")
	}
	ctx := context.Background()

	type row struct {
		id   uuid.UUID
		name string
	}
	rows, err := pgPool.Query(ctx, `SELECT id, name FROM roles WHERE is_system = true ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.id, &r.name))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 4, "expect exactly 4 system roles seeded")

	want := []row{
		{id: uuid.MustParse(domain.SystemRoleAdminID), name: "admin"},
		{id: uuid.MustParse(domain.SystemRoleEditorID), name: "editor"},
		{id: uuid.MustParse(domain.SystemRoleOwnerID), name: "owner"},
		{id: uuid.MustParse(domain.SystemRoleViewerID), name: "viewer"},
	}
	for i := range want {
		assert.Equal(t, want[i].name, got[i].name, "system role at index %d", i)
		assert.Equal(t, want[i].id, got[i].id, "system role %q UUID drifted from pkg/domain/system_roles.go", want[i].name)
	}
}

// TestSystemRoles_SeedDrift computes the expected JSONB permissions array
// per system role from pkg/authz.AllPermissions() minus role-specific
// exclusions, then queries the seeded JSONB and asserts equality
// (order-independent).
//
// Drift between pkg/authz/permissions.go and the migration JSONB is the
// failure this test catches. Adding a new permission to the registry
// without updating the migration seed → this test fails on the next CI
// run.
func TestSystemRoles_SeedDrift(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; skipping integration test")
	}
	ctx := context.Background()

	allPerms := flatPermissions()

	expectedByRole := map[string][]string{
		"owner": allPerms,
		"admin": excludePerms(allPerms, "business.delete", "members.transfer_ownership", "billing.update"),
		"editor": {
			"business.read",
			"members.read",
			"roles.read",
			"integrations.read", "integrations.connect", "integrations.disconnect",
			"content.read", "content.create", "content.update", "content.delete",
		},
		"viewer": {
			"business.read",
			"members.read",
			"roles.read",
			"integrations.read",
			"content.read",
			"billing.read",
		},
	}

	roleIDs := map[string]string{
		"owner":  domain.SystemRoleOwnerID,
		"admin":  domain.SystemRoleAdminID,
		"editor": domain.SystemRoleEditorID,
		"viewer": domain.SystemRoleViewerID,
	}

	for _, name := range []string{"owner", "admin", "editor", "viewer"} {
		t.Run(name, func(t *testing.T) {
			var perms []string
			err := pgPool.QueryRow(ctx,
				`SELECT permissions FROM roles WHERE id = $1::uuid`,
				roleIDs[name],
			).Scan(&perms)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("system role %q (id %s) not seeded — migration did not run", name, roleIDs[name])
				}
				t.Fatalf("query permissions: %v", err)
			}

			expected := append([]string(nil), expectedByRole[name]...)
			sort.Strings(expected)
			sort.Strings(perms)
			assert.Equal(t, expected, perms,
				"system role %q permissions drifted from pkg/authz.AllPermissions() — update either the migration seed or pkg/authz/permissions.go", name)
		})
	}
}

// flatPermissions returns every permission name in pkg/authz.AllPermissions()
// as a string slice (registry order).
func flatPermissions() []string {
	var out []string
	for _, g := range authz.AllPermissions() {
		for _, p := range g.Permissions {
			out = append(out, string(p.Name))
		}
	}
	return out
}

// excludePerms returns a copy of perms minus any names listed in exclude.
func excludePerms(perms []string, exclude ...string) []string {
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excludeSet[e] = struct{}{}
	}
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		if _, drop := excludeSet[p]; drop {
			continue
		}
		out = append(out, p)
	}
	return out
}

// TestTriggerOwnerUUIDMatchesConstant asserts the BEFORE DELETE trigger
// function fn_refuse_sole_owner_delete hardcodes the same owner UUID as
// pkg/domain.SystemRoleOwnerID. Drift between the trigger SQL and the Go
// constant is a silent failure mode — the trigger could refuse the wrong
// row or fail to refuse the right row.
func TestTriggerOwnerUUIDMatchesConstant(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; skipping integration test")
	}
	ctx := context.Background()

	var def string
	err := pgPool.QueryRow(ctx,
		`SELECT pg_get_functiondef('fn_refuse_sole_owner_delete'::regproc)`,
	).Scan(&def)
	require.NoError(t, err, "trigger function fn_refuse_sole_owner_delete must exist")

	assert.Contains(t, def, domain.SystemRoleOwnerID,
		"trigger function hardcodes a different owner UUID than pkg/domain.SystemRoleOwnerID — update either the trigger SQL or the Go constant")
}
