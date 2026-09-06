package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type normalizedResetRepo struct {
	UserRepoForReset
	seen []string
}

func (r *normalizedResetRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.seen = append(r.seen, email)
	return nil, domain.ErrUserNotFound
}

func TestPasswordReset_NormalizesLookupAndRateLimit(t *testing.T) {
	redisClient, server := setupRedis(t)
	repo := &normalizedResetRepo{}
	svc := &PasswordResetService{userRepo: repo, redis: redisClient, auditLog: audit.Nop()}
	for _, input := range []string{"  OWNER@example.COM  ", "owner@example.com"} {
		require.NoError(t, svc.RequestReset(context.Background(), input, "", ""))
	}
	require.Equal(t, []string{"owner@example.com", "owner@example.com"}, repo.seen)
	require.Len(t, server.Keys(), 1)
}

type normalizedVerifyRepo struct {
	VerifyUserRepo
	seen string
}

func (r *normalizedVerifyRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	return &domain.User{ID: id, Email: "old@example.com"}, nil
}

func (r *normalizedVerifyRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.seen = email
	return &domain.User{ID: uuid.New(), Email: email}, nil
}

func TestEmailChange_NormalizesDuplicateLookup(t *testing.T) {
	repo := &normalizedVerifyRepo{}
	svc := &EmailVerificationService{users: repo}
	_, err := svc.ChangeEmailBeforeVerify(context.Background(), uuid.New(), "  OWNER@example.COM  ")
	require.ErrorIs(t, err, domain.ErrEmailTaken)
	require.Equal(t, "owner@example.com", repo.seen)
}
