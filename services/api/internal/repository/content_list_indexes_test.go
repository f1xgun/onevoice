package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestContentListIndexesAvoidBlockingSort(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI is required for isolated MongoDB integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("ov_content_indexes_" + uuid.NewString())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		require.NoError(t, db.Drop(cleanupCtx))
		require.NoError(t, client.Disconnect(cleanupCtx))
	})
	for _, collection := range []string{"posts", "reviews"} {
		_, err = db.Collection(collection).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "business_id", Value: 1}, {Key: "created_at", Value: -1}}})
		require.NoError(t, err)
		docs := make([]interface{}, 4000)
		created := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
		for i := range docs {
			businessID := "organization-a"
			if i%2 == 0 {
				businessID = "organization-b"
			}
			docs[i] = bson.M{"_id": fmt.Sprintf("item-%05d", i), "business_id": businessID, "created_at": created, "status": "published", "reply_status": "pending", "platform": "vk"}
		}
		_, err = db.Collection(collection).InsertMany(ctx, docs)
		require.NoError(t, err)
	}
	require.NoError(t, EnsureContentListIndexes(ctx, db))
	require.NoError(t, EnsureContentListIndexes(ctx, db))
	for _, tc := range []struct{ collection, field, value string }{
		{"posts", "", ""}, {"posts", "status", "published"}, {"reviews", "", ""}, {"reviews", "reply_status", "pending"}, {"reviews", "platform", "vk"},
	} {
		t.Run(tc.collection+"/"+tc.field, func(t *testing.T) {
			filter := bson.M{"business_id": "organization-a"}
			if tc.field != "" {
				filter[tc.field] = tc.value
			}
			command := bson.D{
				{Key: "explain", Value: bson.D{{Key: "find", Value: tc.collection}, {Key: "filter", Value: filter}, {Key: "sort", Value: bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}}, {Key: "skip", Value: 1000}, {Key: "limit", Value: 20}}},
				{Key: "verbosity", Value: "executionStats"},
			}
			var explain struct {
				Stats struct {
					Returned int    `bson:"nReturned"`
					Stages   bson.M `bson:"executionStages"`
				} `bson:"executionStats"`
			}
			require.NoError(t, db.RunCommand(ctx, command).Decode(&explain))
			require.Equal(t, 20, explain.Stats.Returned)
			require.False(t, contentPlanContainsStage(explain.Stats.Stages, "SORT"), "%v", explain)
			require.False(t, contentPlanContainsStage(explain.Stats.Stages, "COLLSCAN"), "%v", explain)
		})
	}
}

func contentPlanContainsStage(value interface{}, stage string) bool {
	switch node := value.(type) {
	case bson.M:
		if node["stage"] == stage {
			return true
		}
		for _, child := range node {
			if contentPlanContainsStage(child, stage) {
				return true
			}
		}
	case bson.D:
		for _, child := range node {
			if child.Key == "stage" && child.Value == stage {
				return true
			}
			if contentPlanContainsStage(child.Value, stage) {
				return true
			}
		}
	case bson.A:
		for _, child := range node {
			if contentPlanContainsStage(child, stage) {
				return true
			}
		}
	}
	return false
}
