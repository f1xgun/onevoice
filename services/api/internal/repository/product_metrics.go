// Package repository — product_metrics.go.
//
// PresenceSource reads the North-Star signal straight from the durable
// per-business record: the Mongo `posts` collection that chatturn writes after a
// publishing tool call. It is read-only analytics — no new write path — so the
// North-Star is derived from data the product already persists rather than a
// duplicate event stream.
package repository

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// successfulPostStatuses are the Post.Status values that count as a completed
// presence update (an immediate publish or a scheduled one). Mirrors the closed
// result set in pkg/metrics/product.go (minus "error").
var successfulPostStatuses = bson.A{"published", "scheduled"}

// PresenceStats is one North-Star reading over a time window: the number of
// successful presence updates and the number of distinct businesses that
// produced them (the North-Star numerator and denominator).
type PresenceStats struct {
	Updates          int
	ActiveBusinesses int
}

// PresenceRepository aggregates the posts collection for North-Star stats.
type PresenceRepository struct {
	collection *mongo.Collection
}

// NewPresenceRepository constructs the presence-stats repo over db.posts.
func NewPresenceRepository(db *mongo.Database) *PresenceRepository {
	return &PresenceRepository{collection: db.Collection("posts")}
}

// RecentPresence returns the count of successful presence updates and the number
// of distinct active businesses for posts created at or after `since`. An empty
// window yields a zero-valued PresenceStats (no error).
func (r *PresenceRepository) RecentPresence(ctx context.Context, since time.Time) (PresenceStats, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "created_at", Value: bson.D{{Key: "$gte", Value: since}}},
			{Key: "status", Value: bson.D{{Key: "$in", Value: successfulPostStatuses}}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$business_id"},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "updates", Value: bson.D{{Key: "$sum", Value: "$n"}}},
			{Key: "active", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return PresenceStats{}, fmt.Errorf("aggregate presence: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var rows []struct {
		Updates int `bson:"updates"`
		Active  int `bson:"active"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return PresenceStats{}, fmt.Errorf("decode presence: %w", err)
	}
	if len(rows) == 0 {
		return PresenceStats{}, nil // empty window
	}
	return PresenceStats{Updates: rows[0].Updates, ActiveBusinesses: rows[0].Active}, nil
}
