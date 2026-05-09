package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestEnsureOwnerExistsAfter_LastOwnerMatrix exercises the four mutation
// paths described in CP-3 / AUTHZ-06. For each, the sole-owner case must
// return ErrLastOwner; the multi-owner case must succeed.
//
// Fixture inserts use raw SQL with per-subtest t.Cleanup (CONTEXT D-02 —
// no factory helpers in Phase 1). Every subtest opens its own pgx.Tx and
// rolls back, so the FOR UPDATE locks taken by EnsureOwnerExistsAfter do
// not leak across cases.
func TestEnsureOwnerExistsAfter_LastOwnerMatrix(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; skipping integration test")
	}
	ctx := context.Background()

	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)

	type fixture struct {
		businessID uuid.UUID
		ownerA     uuid.UUID
		ownerB     uuid.UUID // optional second owner
		editorC    uuid.UUID // optional editor for "demote" cases
	}

	// Helper: insert a user, a business, and a sole-owner membership.
	//
	// users INSERT column list `(id, email, password_hash, role)` matches the
	// live schema in migrations/postgres/000001_init.up.sql:
	//   id (PK, defaulted) — provided
	//   email (NOT NULL, UNIQUE) — provided
	//   password_hash (NOT NULL) — provided as 'x'
	//   role (NOT NULL DEFAULT 'owner') — provided
	//   created_at, updated_at (NOT NULL DEFAULT now()) — omitted; defaults apply
	// If the users schema drifts, re-verify the NOT NULL columns and update.
	setupSoleOwner := func(t *testing.T) fixture {
		t.Helper()
		bizID := uuid.New()
		userA := uuid.New()
		_, err := pgPool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'x', 'owner')`, userA, userA.String()+"@test.local")
		require.NoError(t, err)
		_, err = pgPool.Exec(ctx, `INSERT INTO businesses (id, user_id, name, settings) VALUES ($1, $2, 'TestBiz', '{}'::jsonb)`, bizID, userA)
		require.NoError(t, err)
		_, err = pgPool.Exec(ctx, `INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3, 'active', now())`, bizID, userA, ownerRoleID)
		require.NoError(t, err)

		t.Cleanup(func() {
			cleanCtx := context.Background()
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM business_members WHERE business_id = $1`, bizID)
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM businesses WHERE id = $1`, bizID)
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, userA)
		})

		return fixture{businessID: bizID, ownerA: userA}
	}

	addSecondOwner := func(t *testing.T, f *fixture) {
		t.Helper()
		userB := uuid.New()
		_, err := pgPool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'x', 'owner')`, userB, userB.String()+"@test.local")
		require.NoError(t, err)
		_, err = pgPool.Exec(ctx, `INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3, 'active', now())`, f.businessID, userB, ownerRoleID)
		require.NoError(t, err)
		f.ownerB = userB
		t.Cleanup(func() {
			cleanCtx := context.Background()
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM business_members WHERE business_id = $1 AND user_id = $2`, f.businessID, userB)
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, userB)
		})
	}

	addEditor := func(t *testing.T, f *fixture) {
		t.Helper()
		userC := uuid.New()
		_, err := pgPool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'x', 'owner')`, userC, userC.String()+"@test.local")
		require.NoError(t, err)
		_, err = pgPool.Exec(ctx, `INSERT INTO business_members (business_id, user_id, role_id, status, joined_at) VALUES ($1, $2, $3, 'active', now())`, f.businessID, userC, editorRoleID)
		require.NoError(t, err)
		f.editorC = userC
		t.Cleanup(func() {
			cleanCtx := context.Background()
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM business_members WHERE business_id = $1 AND user_id = $2`, f.businessID, userC)
			_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, userC)
		})
	}

	withTx := func(t *testing.T, fn func(tx pgx.Tx)) {
		t.Helper()
		tx, err := pgPool.BeginTx(ctx, pgx.TxOptions{})
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		fn(tx)
	}

	t.Run("Demote_sole_owner_returns_ErrLastOwner", func(t *testing.T) {
		f := setupSoleOwner(t)
		withTx(t, func(tx pgx.Tx) {
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:         authz.OwnerChangeDemote,
				MemberUserID: &f.ownerA,
			})
			assert.ErrorIs(t, err, authz.ErrLastOwner)
		})
	})

	t.Run("Demote_one_of_two_owners_succeeds", func(t *testing.T) {
		f := setupSoleOwner(t)
		addSecondOwner(t, &f)
		withTx(t, func(tx pgx.Tx) {
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:         authz.OwnerChangeDemote,
				MemberUserID: &f.ownerA,
			})
			assert.NoError(t, err)
		})
	})

	t.Run("Remove_sole_owner_returns_ErrLastOwner", func(t *testing.T) {
		f := setupSoleOwner(t)
		withTx(t, func(tx pgx.Tx) {
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:         authz.OwnerChangeRemove,
				MemberUserID: &f.ownerA,
			})
			assert.ErrorIs(t, err, authz.ErrLastOwner)
		})
	})

	t.Run("Remove_one_of_two_owners_succeeds", func(t *testing.T) {
		f := setupSoleOwner(t)
		addSecondOwner(t, &f)
		withTx(t, func(tx pgx.Tx) {
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:         authz.OwnerChangeRemove,
				MemberUserID: &f.ownerA,
			})
			assert.NoError(t, err)
		})
	})

	t.Run("RoleEditRemovesOwnerPerm_on_sole_owner_role_returns_ErrLastOwner", func(t *testing.T) {
		f := setupSoleOwner(t)
		withTx(t, func(tx pgx.Tx) {
			roleID := ownerRoleID
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:   authz.OwnerChangeRoleEditRemovesOwnerPerm,
				RoleID: &roleID,
			})
			assert.ErrorIs(t, err, authz.ErrLastOwner)
		})
	})

	t.Run("RoleEditRemovesOwnerPerm_on_editor_role_succeeds", func(t *testing.T) {
		f := setupSoleOwner(t)
		addEditor(t, &f)
		withTx(t, func(tx pgx.Tx) {
			roleID := editorRoleID
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:   authz.OwnerChangeRoleEditRemovesOwnerPerm,
				RoleID: &roleID,
			})
			assert.NoError(t, err)
		})
	})

	t.Run("RoleDelete_owner_role_with_sole_holder_returns_ErrLastOwner", func(t *testing.T) {
		f := setupSoleOwner(t)
		withTx(t, func(tx pgx.Tx) {
			roleID := ownerRoleID
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:   authz.OwnerChangeRoleDelete,
				RoleID: &roleID,
			})
			assert.ErrorIs(t, err, authz.ErrLastOwner)
		})
	})

	t.Run("RoleDelete_editor_role_succeeds", func(t *testing.T) {
		f := setupSoleOwner(t)
		addEditor(t, &f)
		withTx(t, func(tx pgx.Tx) {
			roleID := editorRoleID
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{
				Kind:   authz.OwnerChangeRoleDelete,
				RoleID: &roleID,
			})
			assert.NoError(t, err)
		})
	})

	t.Run("Unspecified_kind_returns_error", func(t *testing.T) {
		f := setupSoleOwner(t)
		withTx(t, func(tx pgx.Tx) {
			err := authz.EnsureOwnerExistsAfter(ctx, tx, f.businessID, authz.OwnerChange{Kind: authz.OwnerChangeUnspecified})
			require.Error(t, err)
			assert.False(t, errors.Is(err, authz.ErrLastOwner))
		})
	})
}

// TestCheckEscalationSubset covers the 3 escalation scenarios from
// AUTHZ-07 + PITFALLS MP-3(c). No DB needed but lives here per CONTEXT D-01
// for symmetry with the rest of the invariant matrix.
func TestCheckEscalationSubset(t *testing.T) {
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)

	t.Run("editor_proposing_unowned_perm_refused", func(t *testing.T) {
		actorPerms := []authz.Permission{authz.PermContentRead, authz.PermContentCreate}
		proposed := []authz.Permission{authz.PermContentRead, authz.PermMembersInvite}
		err := authz.CheckEscalationSubset(editorRoleID, actorPerms, proposed)
		assert.ErrorIs(t, err, authz.ErrCannotGrantUnownedPermissions)
	})

	t.Run("editor_proposing_only_owned_perms_succeeds", func(t *testing.T) {
		actorPerms := []authz.Permission{authz.PermContentRead, authz.PermContentCreate}
		proposed := []authz.Permission{authz.PermContentRead}
		err := authz.CheckEscalationSubset(editorRoleID, actorPerms, proposed)
		assert.NoError(t, err)
	})

	t.Run("system_owner_exempt_can_grant_unowned_perm", func(t *testing.T) {
		// Owner's actorPerms slice is intentionally empty — the exemption is
		// based on actorRoleID, not on the perms list.
		err := authz.CheckEscalationSubset(ownerRoleID, []authz.Permission{}, []authz.Permission{authz.PermBillingUpdate})
		assert.NoError(t, err)
	})
}

// TestCheckSelfLockout covers the 3 self-lockout scenarios from AUTHZ-08
// plus an additional safe-edit case (own role, both critical perms kept).
func TestCheckSelfLockout(t *testing.T) {
	actorUserID := uuid.New()
	actorRoleID := uuid.New()
	otherRoleID := uuid.New()

	t.Run("actor_edits_own_role_removing_roles_update_refused", func(t *testing.T) {
		// New perms keep members.update_role but drop roles.update.
		newPerms := []authz.Permission{authz.PermMembersUpdateRole, authz.PermContentRead}
		err := authz.CheckSelfLockout(actorUserID, actorRoleID, actorRoleID, newPerms)
		assert.ErrorIs(t, err, authz.ErrSelfLockout)
	})

	t.Run("actor_edits_own_role_removing_members_update_role_refused", func(t *testing.T) {
		// New perms keep roles.update but drop members.update_role.
		newPerms := []authz.Permission{authz.PermRolesUpdate, authz.PermContentRead}
		err := authz.CheckSelfLockout(actorUserID, actorRoleID, actorRoleID, newPerms)
		assert.ErrorIs(t, err, authz.ErrSelfLockout)
	})

	t.Run("actor_edits_other_role_succeeds", func(t *testing.T) {
		newPerms := []authz.Permission{authz.PermContentRead}
		err := authz.CheckSelfLockout(actorUserID, actorRoleID, otherRoleID, newPerms)
		assert.NoError(t, err)
	})

	t.Run("actor_edits_own_role_keeping_both_critical_perms_succeeds", func(t *testing.T) {
		newPerms := []authz.Permission{authz.PermRolesUpdate, authz.PermMembersUpdateRole, authz.PermContentRead}
		err := authz.CheckSelfLockout(actorUserID, actorRoleID, actorRoleID, newPerms)
		assert.NoError(t, err)
	})
}
