package authz_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
)

// fakeLoader is a handcoded test double that implements authz.MembershipLoader.
type fakeLoader struct {
	members map[string]*authz.CachedMember // key: businessID+":"+userID
	roles   map[string]*authz.CachedRole   // key: roleID
}

func (f *fakeLoader) LoadMembership(ctx context.Context, businessID, userID uuid.UUID) (*authz.CachedMember, error) {
	key := businessID.String() + ":" + userID.String()
	m, ok := f.members[key]
	if !ok {
		return nil, nil
	}
	return m, nil
}

func (f *fakeLoader) LoadRole(ctx context.Context, roleID uuid.UUID) (*authz.CachedRole, error) {
	r, ok := f.roles[roleID.String()]
	if !ok {
		return nil, nil
	}
	return r, nil
}

// compile-time assertion: *fakeLoader satisfies authz.MembershipLoader.
var _ authz.MembershipLoader = (*fakeLoader)(nil)

func TestMembershipLoader_Interface(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	loader := &fakeLoader{
		members: map[string]*authz.CachedMember{
			bizID.String() + ":" + userID.String(): {
				RoleID:   roleID,
				Status:   "active",
				JoinedAt: time.Now(),
			},
		},
		roles: map[string]*authz.CachedRole{
			roleID.String(): {
				Permissions: []authz.Permission{authz.PermContentRead},
			},
		},
	}

	t.Run("LoadMembership returns CachedMember for known pair", func(t *testing.T) {
		m, err := loader.LoadMembership(context.Background(), bizID, userID)
		require.NoError(t, err)
		require.NotNil(t, m)
		require.Equal(t, roleID, m.RoleID)
		require.Equal(t, "active", m.Status)
	})

	t.Run("LoadRole returns CachedRole for known roleID", func(t *testing.T) {
		r, err := loader.LoadRole(context.Background(), roleID)
		require.NoError(t, err)
		require.NotNil(t, r)
		require.Equal(t, []authz.Permission{authz.PermContentRead}, r.Permissions)
	})

	t.Run("compile-time interface assertion passes", func(t *testing.T) {
		var loader authz.MembershipLoader = &fakeLoader{}
		require.NotNil(t, loader)
	})
}
