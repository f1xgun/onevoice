package authz_test

import (
	"errors"
	"testing"

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
