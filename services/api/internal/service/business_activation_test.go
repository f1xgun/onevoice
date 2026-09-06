package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type firstActionStub struct {
	read func(context.Context, uuid.UUID) (bool, error)
}

func (s firstActionStub) HasFirstSuccessfulAction(ctx context.Context, id uuid.UUID) (bool, error) {
	return s.read(ctx, id)
}

func TestBusinessFirstSuccessfulAction(t *testing.T) {
	failed := errors.New("read failed")
	for _, tt := range []struct {
		name    string
		success bool
		err     error
	}{
		{name: "successful history", success: true},
		{name: "no successful history"},
		{name: "unavailable history", err: failed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := uuid.New()
			svc := &businessService{
				repo: &mockBusinessRepository{getByIDFunc: func(context.Context, uuid.UUID) (*domain.Business, error) { return &domain.Business{ID: id}, nil }},
				firstActions: firstActionStub{read: func(ctx context.Context, got uuid.UUID) (bool, error) {
					require.Equal(t, id, got)
					require.NoError(t, ctx.Err())
					return tt.success, tt.err
				}},
			}
			got, err := svc.GetByID(context.Background(), id)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.success, got.HasFirstSuccessfulAction)
		})
	}
}
