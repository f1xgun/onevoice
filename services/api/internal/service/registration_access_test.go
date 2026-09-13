package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type registrationAccessRepoStub struct {
	allowed bool
	err     error
	email   string
	ctx     context.Context
}

func (r *registrationAccessRepoStub) HasRegistrationAccess(ctx context.Context, email string) (bool, error) {
	r.email, r.ctx = email, ctx
	return r.allowed, r.err
}

func TestRegistrationAccessService(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
		err     error
	}{
		{name: "approved", allowed: true},
		{name: "not approved"},
		{name: "store error overrides any result", allowed: true, err: assert.AnError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &registrationAccessRepoStub{allowed: tc.allowed, err: tc.err}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			allowed, err := NewRegistrationAccessService(repo).HasRegistrationAccess(ctx, " Owner@Example.org ")
			require.Equal(t, tc.allowed && tc.err == nil, allowed)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, "owner@example.org", repo.email)
			require.Same(t, ctx, repo.ctx)
		})
	}
	t.Run("missing service or store cannot grant access", func(t *testing.T) {
		for _, svc := range []*RegistrationAccessService{nil, NewRegistrationAccessService(nil)} {
			allowed, err := svc.HasRegistrationAccess(context.Background(), "owner@example.org")
			require.Error(t, err)
			require.False(t, allowed)
		}
	})
	t.Run("blank email does not query or grant", func(t *testing.T) {
		repo := &registrationAccessRepoStub{allowed: true}
		allowed, err := NewRegistrationAccessService(repo).HasRegistrationAccess(context.Background(), " \n\t")
		require.NoError(t, err)
		require.False(t, allowed)
		require.Nil(t, repo.ctx)
	})
}
