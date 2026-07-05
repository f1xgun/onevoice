package reviewstats

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Fetcher loads the minimal review fields needed to aggregate a business's
// reputation stats. Implemented by MongoRepo in production; a fake satisfies it
// in tests so the executor can be exercised without a live database.
type Fetcher interface {
	FetchForBusiness(ctx context.Context, businessID string) ([]domain.Review, error)
}

// MongoRepo reads the reviews collection the API service also writes to. The
// orchestrator owns its own connection to the shared database (see
// wire/mongo.go) rather than calling the API, so aggregation stays a single
// in-process query with no duplicated storage.
type MongoRepo struct {
	collection *mongo.Collection
}

// NewMongoRepo binds a MongoRepo to the reviews collection of db.
func NewMongoRepo(db *mongo.Database) *MongoRepo {
	return &MongoRepo{collection: db.Collection("reviews")}
}

// statsProjection restricts the read to the three fields aggregation needs.
// author_name, text, reply_text and draft_* are never fetched, so no personal
// data leaves the collection on this path — the query is aggregate-safe by
// construction, not merely by what the executor chooses to emit.
var statsProjection = bson.D{
	{Key: "rating", Value: 1},
	{Key: "reply_status", Value: 1},
	{Key: "created_at", Value: 1},
	{Key: "_id", Value: 0},
}

// FetchForBusiness returns every review for businessID projected to the
// aggregation fields. The business_id filter is the tenant boundary: the caller
// passes the id from trusted turn context, never an LLM argument. An empty
// businessID is rejected so a missing context cannot fan out to a full-collection
// scan across tenants.
func (r *MongoRepo) FetchForBusiness(ctx context.Context, businessID string) ([]domain.Review, error) {
	if businessID == "" {
		return nil, fmt.Errorf("reviewstats: business id is required")
	}

	cursor, err := r.collection.Find(ctx,
		bson.M{"business_id": businessID},
		options.Find().SetProjection(statsProjection),
	)
	if err != nil {
		return nil, fmt.Errorf("reviewstats: find reviews: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	reviews := make([]domain.Review, 0)
	if err := cursor.All(ctx, &reviews); err != nil {
		return nil, fmt.Errorf("reviewstats: decode reviews: %w", err)
	}
	return reviews, nil
}
