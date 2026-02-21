package repository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type agentTaskRepository struct {
	collection *mongo.Collection
}

func NewAgentTaskRepository(db *mongo.Database) domain.AgentTaskRepository {
	return &agentTaskRepository{
		collection: db.Collection("agent_tasks"),
	}
}

func (r *agentTaskRepository) ListByBusinessID(ctx context.Context, businessID string, filter domain.TaskFilter) ([]domain.AgentTask, int, error) {
	tasks := make([]domain.AgentTask, 0)

	f := bson.M{"business_id": businessID}
	if filter.Platform != "" {
		f["platform"] = filter.Platform
	}
	if filter.Status != "" {
		f["status"] = filter.Status
	}
	if filter.Type != "" {
		f["type"] = filter.Type
	}

	total, err := r.collection.CountDocuments(ctx, f)
	if err != nil {
		return tasks, 0, fmt.Errorf("count agent tasks: %w", err)
	}

	opts := options.Find().
		SetLimit(int64(filter.Limit)).
		SetSkip(int64(filter.Offset)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return tasks, 0, fmt.Errorf("find agent tasks: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	if err := cursor.All(ctx, &tasks); err != nil {
		return tasks, 0, fmt.Errorf("decode agent tasks: %w", err)
	}

	return tasks, int(total), nil
}
