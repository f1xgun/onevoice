package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestPostOperationClaimScopesAndSerializes(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewPostRepository(db)
	ctx := context.Background()
	post := &domain.Post{BusinessID: uuid.NewString(), Content: "old", Status: "published", MediaURLs: []string{"https://example.org/photo"}, PlatformResults: map[string]domain.PlatformResult{"telegram": {ExternalID: "@own", PostID: "42", Status: "published"}}}
	require.NoError(t, repo.Create(ctx, post))
	claimed, err := repo.ClaimOperation(ctx, uuid.NewString(), post.ID, "published", "foreign")
	require.NoError(t, err)
	require.False(t, claimed)
	var winners atomic.Int32
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ok, err := repo.ClaimOperation(ctx, post.BusinessID, post.ID, "published", "operation")
			if err != nil {
				t.Error(err)
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	workers.Wait()
	require.Equal(t, int32(1), winners.Load())
	require.ErrorIs(t, repo.CompleteOperation(ctx, post, "wrong-operation"), domain.ErrPostConflict)
	post.Content = "edited"
	require.NoError(t, repo.CompleteOperation(ctx, post, "operation"))
	require.NoError(t, repo.ReconcileMutation(ctx, post.BusinessID, "telegram", "@foreign", "42", "deleted", nil))
	got, err := repo.GetByID(ctx, post.ID)
	require.NoError(t, err)
	require.Equal(t, "published", got.Status)
	require.NoError(t, repo.ReconcileMutation(ctx, post.BusinessID, "telegram", "@own", "42", "deleted", nil))
	got, err = repo.GetByID(ctx, post.ID)
	require.NoError(t, err)
	require.Equal(t, "deleted", got.Status)
	require.Equal(t, post.MediaURLs, got.MediaURLs)
	require.Equal(t, "edited", got.Content)
}
