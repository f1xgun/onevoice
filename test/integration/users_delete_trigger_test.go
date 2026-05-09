package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestUsersDeleteTrigger_RefusesSoleOwner asserts the BEFORE DELETE
// trigger fn_refuse_sole_owner_delete blocks deletion when the user is the
// sole owner of any business, and allows it once a second owner exists.
//
// Both halves of the contract are essential: the refusal proves the trigger
// fires; the post-promotion success proves the trigger doesn't false-positive
// once the sole-owner constraint is no longer violated.
func TestUsersDeleteTrigger_RefusesSoleOwner(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; skipping integration test")
	}
	ctx := context.Background()
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)

	// Setup: user A owns business B (sole owner). user B is registered but
	// not yet a member.
	userA := uuid.New()
	userB := uuid.New()
	bizID := uuid.New()

	// users INSERT column list matches the live schema in
	// migrations/postgres/000001_init.up.sql:
	//   id (PK, defaulted) — provided here
	//   email (NOT NULL, UNIQUE) — provided
	//   password_hash (NOT NULL) — provided as 'x'
	//   role (NOT NULL DEFAULT 'owner') — provided
	//   created_at, updated_at (NOT NULL DEFAULT now()) — omitted; defaults apply
	// If the users schema drifts, re-verify the NOT NULL columns and update.
	_, err := pgPool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'x', 'owner'), ($3, $4, 'x', 'owner')`,
		userA, userA.String()+"@test.local", userB, userB.String()+"@test.local")
	require.NoError(t, err)
	_, err = pgPool.Exec(ctx, `INSERT INTO businesses (id, user_id, name, settings) VALUES ($1, $2, 'TriggerTestBiz', '{}'::jsonb)`, bizID, userA)
	require.NoError(t, err)
	_, err = pgPool.Exec(ctx, `INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3, 'active', now())`, bizID, userA, ownerRoleID)
	require.NoError(t, err)

	t.Cleanup(func() {
		// Use a bounded background context so cleanup does not hang if the
		// test's own ctx was cancelled.
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Defensively re-promote userB so the user-delete cascade does not
		// re-trigger the sole-owner check during cleanup.
		_, _ = pgPool.Exec(cleanCtx, `INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3, 'active', now()) ON CONFLICT DO NOTHING`, bizID, userB, ownerRoleID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM business_members WHERE business_id = $1`, bizID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM businesses WHERE id = $1`, bizID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id IN ($1, $2)`, userA, userB)
	})

	// Step 1: deleting userA must fail because userA is the sole owner of bizID.
	_, err = pgPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userA)
	require.Error(t, err, "expected trigger to refuse delete of sole owner")
	assert.True(t, strings.Contains(err.Error(), "cannot delete user: sole owner of business"),
		"trigger error should mention sole-owner reason; got %v", err)

	// Step 2: promote userB to second owner.
	_, err = pgPool.Exec(ctx, `INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3, 'active', now())`, bizID, userB, ownerRoleID)
	require.NoError(t, err)

	// Step 3: deleting userA now succeeds (userB remains as owner).
	tag, err := pgPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userA)
	require.NoError(t, err, "expected trigger to allow delete after second owner exists")
	assert.Equal(t, int64(1), tag.RowsAffected(), "exactly one user row removed")
}
