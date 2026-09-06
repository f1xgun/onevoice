package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// ActionActivationRepository reads the first-action activation signal from durable records.
type ActionActivationRepository struct{ db *mongo.Database }

// NewActionActivationRepository constructs the organization-wide action reader.
func NewActionActivationRepository(db *mongo.Database) *ActionActivationRepository {
	return &ActionActivationRepository{db: db}
}

func successfulAnswerFilter() bson.M {
	return bson.M{
		"successful_outcome": true,
		"role":               domain.MessageRoleAssistant,
		"status":             domain.MessageStatusComplete,
		"error_code":         bson.M{"$in": bson.A{nil, ""}},
		"content":            bson.M{"$regex": `\S`},
		"tool_calls.0":       bson.M{"$exists": false},
	}
}

// HasFirstSuccessfulAction uses indexed organization/status lookups without a history window.
func (r *ActionActivationRepository) HasFirstSuccessfulAction(ctx context.Context, businessID uuid.UUID) (bool, error) {
	if r.db == nil {
		return false, fmt.Errorf("action activation database unavailable")
	}
	answer := successfulAnswerFilter()
	answer["business_id"] = businessID.String()
	review := bson.M{
		"business_id": businessID.String(),
		"$or": bson.A{
			bson.M{"successful_action": true},
			bson.M{
				"draft_status": domain.ReviewDraftStatusReady,
				"draft_reply":  bson.M{"$regex": `\S`},
				"draft_error":  bson.M{"$in": bson.A{nil, ""}},
			},
		},
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"business_id": businessID.String(), "status": taskStatusDone}}},
		{{Key: "$limit", Value: 1}},
		{{Key: "$project", Value: bson.M{"_id": 1}}},
		{{Key: "$unionWith", Value: bson.M{"coll": "messages", "pipeline": mongo.Pipeline{
			{{Key: "$match", Value: answer}},
			{{Key: "$limit", Value: 1}},
			{{Key: "$project", Value: bson.M{"_id": 1}}},
		}}}},
		{{Key: "$unionWith", Value: bson.M{"coll": "reviews", "pipeline": mongo.Pipeline{
			{{Key: "$match", Value: review}},
			{{Key: "$limit", Value: 1}},
			{{Key: "$project", Value: bson.M{"_id": 1}}},
		}}}},
		{{Key: "$limit", Value: 1}},
	}
	cursor, err := r.db.Collection("agent_tasks").Aggregate(ctx, pipeline)
	if err != nil {
		return false, fmt.Errorf("query first successful action: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	found := cursor.Next(ctx)
	if err := cursor.Err(); err != nil {
		return false, fmt.Errorf("read first successful action: %w", err)
	}
	return found, nil
}

// EnsureActionActivation backfills message organization IDs and installs the read indexes.
// Existing messages retain their status; ambiguous legacy answers never become successes.
func EnsureActionActivation(ctx context.Context, db *mongo.Database) error {
	if err := backfillActionOrganizations(ctx, db); err != nil {
		return err
	}
	for _, collection := range []string{"messages", "agent_tasks"} {
		_, err := db.Collection(collection).Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: "business_id", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName("activation_business_status"),
		})
		if err != nil {
			return fmt.Errorf("index action activation %s: %w", collection, err)
		}
	}
	for _, field := range []string{"successful_action", "draft_status"} {
		_, err := db.Collection("reviews").Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: "business_id", Value: 1}, {Key: field, Value: 1}},
			Options: options.Index().SetName("activation_business_" + field),
		})
		if err != nil {
			return fmt.Errorf("index review activation %s: %w", field, err)
		}
	}
	return nil
}

func backfillActionOrganizations(ctx context.Context, db *mongo.Database) error {
	const markerID = "message-action-organizations"
	markers := db.Collection("schema_migrations")
	err := markers.FindOne(ctx, bson.M{"_id": markerID}).Err()
	if err == nil {
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("read action backfill marker: %w", err)
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"business_id": bson.M{"$exists": false}}}},
		{{Key: "$lookup", Value: bson.M{"from": "conversations", "localField": "conversation_id", "foreignField": "_id", "as": "organization"}}},
		{{Key: "$unwind", Value: "$organization"}},
		{{Key: "$project", Value: bson.M{"_id": 1, "business_id": "$organization.business_id"}}},
	}
	messages := db.Collection("messages")
	cursor, err := messages.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("find message organizations: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	for cursor.Next(ctx) {
		var row struct {
			ID         string `bson:"_id"`
			BusinessID string `bson:"business_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			return fmt.Errorf("decode message organization: %w", err)
		}
		_, err := messages.UpdateOne(ctx, bson.M{"_id": row.ID, "business_id": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"business_id": row.BusinessID}})
		if err != nil {
			return fmt.Errorf("backfill message organization: %w", err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("read message organizations: %w", err)
	}
	_, err = markers.UpdateOne(ctx, bson.M{"_id": markerID}, bson.M{"$set": bson.M{"complete": true}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("record action backfill: %w", err)
	}
	return nil
}
