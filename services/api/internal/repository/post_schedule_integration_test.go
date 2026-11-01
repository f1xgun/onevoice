package repository

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func scheduleTestRepository(t *testing.T) domain.PostRepository {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI must name an isolated test MongoDB")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("test_post_schedule_" + uuid.NewString()[:8])
	t.Cleanup(func() {
		require.NoError(t, db.Drop(context.Background()))
		require.NoError(t, client.Disconnect(context.Background()))
	})
	return NewPostRepository(db)
}
func TestPostScheduleClaimIsAtomicAndNeverReclaims(t *testing.T) {
	repo := scheduleTestRepository(t)
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	post := &domain.Post{BusinessID: uuid.NewString(), Content: "text", Status: "scheduled", ScheduledAt: &past, PlatformResults: map[string]domain.PlatformResult{"telegram": {ExternalID: "@own", Status: "scheduled"}}}
	require.NoError(t, repo.Create(ctx, post))
	future := now.Add(time.Hour)
	changed, err := repo.UpdateScheduled(ctx, uuid.NewString(), post.ID, &future, nil, "scheduled")
	require.NoError(t, err)
	require.False(t, changed)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := repo.ClaimDue(ctx, now)
			if err != nil {
				t.Error(err)
			}
			if claimed != nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), winners.Load())
	changed, err = repo.UpdateScheduled(ctx, post.BusinessID, post.ID, &future, nil, "canceled")
	require.NoError(t, err)
	require.False(t, changed)
	claimed, err := repo.ClaimDue(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, claimed, "a crashed processing send must not be reclaimed")
}
func TestPostScheduleRescheduleAndCancelBeforeClaim(t *testing.T) {
	repo := scheduleTestRepository(t)
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	post := &domain.Post{BusinessID: uuid.NewString(), Content: "old", Status: "scheduled", ScheduledAt: &past, PlatformResults: map[string]domain.PlatformResult{"telegram": {ExternalID: "@own", Status: "scheduled"}}}
	require.NoError(t, repo.Create(ctx, post))
	content := "new"
	changed, err := repo.UpdateScheduled(ctx, post.BusinessID, post.ID, &future, &content, "scheduled")
	require.NoError(t, err)
	require.True(t, changed)
	claimed, err := repo.ClaimDue(ctx, now)
	require.NoError(t, err)
	require.Nil(t, claimed)
	changed, err = repo.UpdateScheduled(ctx, post.BusinessID, post.ID, nil, nil, "canceled")
	require.NoError(t, err)
	require.True(t, changed)
	claimed, err = repo.ClaimDue(ctx, future.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, claimed)
	saved, err := repo.GetByID(ctx, post.ID)
	require.NoError(t, err)
	require.Equal(t, "new", saved.Content)
	require.Equal(t, "canceled", saved.Status)
	vk := &domain.Post{BusinessID: post.BusinessID, Status: "scheduled", ScheduledAt: &past, PlatformResults: map[string]domain.PlatformResult{"vk": {PostID: "42", Status: "scheduled"}}}
	require.NoError(t, repo.Create(ctx, vk))
	claimed, err = repo.ClaimDue(ctx, now)
	require.NoError(t, err)
	require.Nil(t, claimed, "VK scheduling stays platform-held")
}
