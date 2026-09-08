package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type conversationListRepo struct {
	domain.ConversationRepository
	list func(context.Context, string, string, int, int) ([]domain.Conversation, error)
}

func (r conversationListRepo) ListByUserID(ctx context.Context, userID, businessID string, limit, offset int) ([]domain.Conversation, error) {
	return r.list(ctx, userID, businessID, limit, offset)
}

func TestConversationService_List(t *testing.T) {
	businessID, userID := uuid.New(), uuid.New()
	repoErr := errors.New("list unavailable")
	for _, tc := range []struct {
		name               string
		businessID, userID uuid.UUID
		err                error
	}{
		{"scoped previews", businessID, userID, nil},
		{"missing organization", uuid.Nil, userID, domain.ErrInvalidScope},
		{"missing user", businessID, uuid.Nil, domain.ErrInvalidScope},
		{"repository failure", businessID, userID, repoErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			repo := conversationListRepo{list: func(_ context.Context, gotUserID, gotBusinessID string, limit, offset int) ([]domain.Conversation, error) {
				calls++
				require.Equal(t, userID.String(), gotUserID)
				require.Equal(t, businessID.String(), gotBusinessID)
				require.Equal(t, 20, limit)
				require.Equal(t, 3, offset)
				if tc.err != nil {
					return nil, tc.err
				}
				return []domain.Conversation{{ID: "chat", Preview: "Новый ответ 🦊"}, {ID: "empty"}}, nil
			}}
			svc, err := service.NewConversationService(repo, &stubMessageRepo{}, &stubProjectRepoForConv{}, &stubPendingToolCallRepo{})
			require.NoError(t, err)
			got, err := svc.List(context.Background(), tc.businessID, tc.userID, 20, 3)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Len(t, got, 2)
				require.Equal(t, "Новый ответ 🦊", got[0].Preview)
				require.Empty(t, got[1].Preview)
			}
			if errors.Is(tc.err, domain.ErrInvalidScope) {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}
