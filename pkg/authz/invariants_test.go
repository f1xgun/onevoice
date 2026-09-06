package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestCheckSystemRoleImmutable(t *testing.T) {
	t.Run("system role -> ErrSystemRoleImmutable", func(t *testing.T) {
		err := authz.CheckSystemRoleImmutable(&domain.Role{IsSystem: true})
		require.ErrorIs(t, err, authz.ErrSystemRoleImmutable)
	})
	t.Run("custom role -> nil", func(t *testing.T) {
		require.NoError(t, authz.CheckSystemRoleImmutable(&domain.Role{IsSystem: false}))
	})
	t.Run("nil role -> non-nil non-sentinel error", func(t *testing.T) {
		err := authz.CheckSystemRoleImmutable(nil)
		require.Error(t, err)
		require.False(t, errors.Is(err, authz.ErrSystemRoleImmutable))
	})
}

// memberRow is one locked business_members snapshot row as the invariant's
// lock query returns it: membership identity plus the effective-owner flag.
type memberRow struct {
	userID          uuid.UUID
	roleID          uuid.UUID
	pendingDeletion bool
}

// TestEnsureOwnerExistsAfter_PendingDeletionOwnersDoNotCount — an owner whose
// user account is pending deletion is a tombstone, not a manageable owner, so
// it must not satisfy the last-owner quorum. Before the fix the lock query read
// business_members alone, a soft-deleted co-owner counted, and the last real
// owner could remove or demote himself and strand the organization.
//
// The lock query regex is part of the contract under test: reverting to the
// users-less `SELECT user_id, role_id FROM business_members` makes the
// expectation unmatched and every subtest fails.
func TestEnsureOwnerExistsAfter_PendingDeletionOwnersDoNotCount(t *testing.T) {
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)

	actor := uuid.New()
	coOwner := uuid.New()
	viewer := uuid.New()

	tests := []struct {
		name    string
		rows    []memberRow
		change  authz.OwnerChange
		wantErr error
	}{
		{
			name: "remove self while the only co-owner is pending deletion",
			rows: []memberRow{
				{userID: actor, roleID: ownerRoleID},
				{userID: coOwner, roleID: ownerRoleID, pendingDeletion: true},
				{userID: viewer, roleID: viewerRoleID},
			},
			change:  authz.OwnerChange{Kind: authz.OwnerChangeRemove, MemberUserID: &actor},
			wantErr: authz.ErrLastOwner,
		},
		{
			name: "demote self while the only co-owner is pending deletion",
			rows: []memberRow{
				{userID: actor, roleID: ownerRoleID},
				{userID: coOwner, roleID: ownerRoleID, pendingDeletion: true},
			},
			change:  authz.OwnerChange{Kind: authz.OwnerChangeDemote, MemberUserID: &actor},
			wantErr: authz.ErrLastOwner,
		},
		{
			name: "remove self while a live co-owner remains",
			rows: []memberRow{
				{userID: actor, roleID: ownerRoleID},
				{userID: coOwner, roleID: ownerRoleID},
			},
			change:  authz.OwnerChange{Kind: authz.OwnerChangeRemove, MemberUserID: &actor},
			wantErr: nil,
		},
		{
			name: "co-owner canceled his deletion and counts again",
			rows: []memberRow{
				{userID: actor, roleID: ownerRoleID},
				{userID: coOwner, roleID: ownerRoleID, pendingDeletion: false},
			},
			change:  authz.OwnerChange{Kind: authz.OwnerChangeRemove, MemberUserID: &actor},
			wantErr: nil,
		},
		{
			name: "removing the pending-deletion owner leaves a live owner",
			rows: []memberRow{
				{userID: actor, roleID: ownerRoleID},
				{userID: coOwner, roleID: ownerRoleID, pendingDeletion: true},
			},
			change:  authz.OwnerChange{Kind: authz.OwnerChangeRemove, MemberUserID: &coOwner},
			wantErr: nil,
		},
		{
			name: "role delete stranding the last live owner",
			rows: []memberRow{
				{userID: actor, roleID: ownerRoleID},
				{userID: coOwner, roleID: ownerRoleID, pendingDeletion: true},
			},
			change:  authz.OwnerChange{Kind: authz.OwnerChangeRoleDelete, RoleID: &ownerRoleID},
			wantErr: authz.ErrLastOwner,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			t.Cleanup(mock.Close)

			businessID := uuid.New()
			rows := pgxmock.NewRows([]string{"user_id", "role_id", "pending_deletion"})
			for _, r := range tc.rows {
				rows = rows.AddRow(r.userID, r.roleID, r.pendingDeletion)
			}
			mock.ExpectQuery(`(?s)\(u\.deletion_requested_at IS NOT NULL AND u\.deletion_canceled_at IS NULL\) AS pending_deletion.*JOIN users u ON u\.id = m\.user_id`).
				WithArgs(businessID).
				WillReturnRows(rows)

			gotErr := authz.EnsureOwnerExistsAfter(context.Background(), mock, businessID, tc.change)

			if tc.wantErr != nil {
				require.ErrorIs(t, gotErr, tc.wantErr)
			} else {
				require.NoError(t, gotErr)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
