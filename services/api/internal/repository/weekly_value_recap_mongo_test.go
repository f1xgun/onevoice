package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func weeklyRecapMongoDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping weekly recap Mongo test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(2 * time.Second))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))
	db := client.Database("weekly_recap_" + uuid.NewString())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		require.NoError(t, db.Drop(cleanupCtx))
		require.NoError(t, client.Disconnect(cleanupCtx))
	})
	return db
}

func TestWeeklyValueRecapMongoAuthoritativeBoundariesAndClosedTaxonomy(t *testing.T) {
	db := weeklyRecapMongoDB(t)
	ctx := context.Background()
	businessID := uuid.New()
	otherID := uuid.New()
	from := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	to := from.Add(168 * time.Hour)

	_, err := db.Collection("posts").InsertMany(ctx, []any{
		bson.M{"business_id": businessID.String(), "status": "published", "published_at": from, "created_at": to},
		bson.M{"business_id": businessID.String(), "status": "published", "published_at": to.Add(-time.Nanosecond)},
		bson.M{"business_id": businessID.String(), "status": "published", "published_at": to},
		bson.M{"business_id": businessID.String(), "status": "draft", "published_at": from},
		bson.M{"business_id": otherID.String(), "status": "published", "published_at": from},
	})
	require.NoError(t, err)
	_, err = db.Collection("reviews").InsertMany(ctx, []any{
		bson.M{"business_id": businessID.String(), "reply_status": "replied", "replied_at": from, "dispatch_approval_id": "approved"},
		bson.M{"business_id": businessID.String(), "reply_status": "replied", "replied_at": from, "dispatch_approval_id": nil},
		bson.M{"business_id": businessID.String(), "reply_status": "replied", "replied_at": from, "dispatch_approval_id": ""},
		bson.M{"business_id": businessID.String(), "reply_status": "replied", "created_at": from, "dispatch_approval_id": "approved"},
	})
	require.NoError(t, err)
	_, err = db.Collection("agent_tasks").InsertMany(ctx, []any{
		bson.M{"business_id": businessID.String(), "status": "done", "completed_at": from, "platform": "telegram", "type": "sync_title"},
		bson.M{"business_id": businessID.String(), "status": "done", "completed_at": from, "platform": "vk", "type": "update_group_info", "dispatch_approval_id": "approved"},
		bson.M{"business_id": businessID.String(), "status": "done", "completed_at": from, "platform": "yandex_business", "type": "update_hours", "dispatch_approval_id": "approved"},
		bson.M{"business_id": businessID.String(), "status": "done", "completed_at": from, "platform": "vk", "type": "update_group_info", "dispatch_approval_id": nil},
		bson.M{"business_id": businessID.String(), "status": "done", "completed_at": from, "platform": "telegram", "type": "send_notification"},
		bson.M{"business_id": businessID.String(), "status": "error", "completed_at": from, "platform": "telegram", "type": "sync_title"},
		bson.M{"business_id": businessID.String(), "status": "done", "completed_at": to, "platform": "telegram", "type": "sync_title"},
	})
	require.NoError(t, err)

	got, err := NewWeeklyValueRecapSource(db).CountWeeklyValue(ctx, businessID, from, to)
	require.NoError(t, err)
	assert.Equal(t, 2, got.PublishedPosts)
	assert.Equal(t, 1, got.DispatchedReviewReplies)
	assert.Equal(t, 3, got.CompletedSyncs)
}

func TestEnsureWeeklyValueRecapIndexes(t *testing.T) {
	db := weeklyRecapMongoDB(t)
	require.NoError(t, EnsureWeeklyValueRecapIndexes(context.Background(), db))
	require.NoError(t, EnsureWeeklyValueRecapIndexes(context.Background(), db))
	for collection, name := range map[string]string{
		"posts":       "posts_recap_business_status_published",
		"reviews":     "reviews_recap_business_status_replied_dispatch",
		"agent_tasks": "tasks_recap_business_status_type_platform_completed",
	} {
		cursor, err := db.Collection(collection).Indexes().List(context.Background())
		require.NoError(t, err)
		var indexes []bson.M
		require.NoError(t, cursor.All(context.Background(), &indexes))
		assert.Condition(t, func() bool {
			for _, index := range indexes {
				if index["name"] == name {
					return true
				}
			}
			return false
		}, name)
	}
}
