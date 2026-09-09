package repository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// EnsureContentListIndexes covers the complete descending list order, including
// its ID tie-breaker. New index names preserve compatibility with existing
// installations and with the shorter indexes created by migrations/mongo/init.js.
func EnsureContentListIndexes(ctx context.Context, db *mongo.Database) error {
	for _, spec := range []struct {
		collection string
		prefix     string
		name       string
	}{
		{"posts", "", "posts_business_created_id_desc"},
		{"posts", "status", "posts_business_status_created_id_desc"},
		{"reviews", "", "reviews_business_created_id_desc"},
		{"reviews", "reply_status", "reviews_business_reply_status_created_id_desc"},
		{"reviews", "platform", "reviews_business_platform_created_id_desc"},
	} {
		keys := bson.D{{Key: "business_id", Value: 1}}
		if spec.prefix != "" {
			keys = append(keys, bson.E{Key: spec.prefix, Value: 1})
		}
		keys = append(keys, bson.E{Key: "created_at", Value: -1}, bson.E{Key: "_id", Value: -1})
		model := mongo.IndexModel{Keys: keys, Options: options.Index().SetName(spec.name)}
		if _, err := db.Collection(spec.collection).Indexes().CreateOne(ctx, model); err != nil {
			return fmt.Errorf("ensure %s list index: %w", spec.collection, err)
		}
	}
	return nil
}
