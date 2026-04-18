package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

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

func (r *agentTaskRepository) Create(ctx context.Context, task *domain.AgentTask) error {
	if task.ID == "" {
		task.ID = bson.NewObjectID().Hex()
	}
	task.CreatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, task)
	if err != nil {
		return fmt.Errorf("insert agent task: %w", err)
	}
	return nil
}

func (r *agentTaskRepository) Update(ctx context.Context, task *domain.AgentTask) error {
	if task.ID == "" {
		return fmt.Errorf("update agent task: id is required")
	}
	if task.BusinessID == "" {
		return fmt.Errorf("update agent task: business_id is required")
	}

	set := bson.M{"status": task.Status}
	if task.DisplayName != "" {
		set["display_name"] = task.DisplayName
	}
	if task.Output != nil {
		set["output"] = task.Output
	}
	if task.Error != "" {
		set["error"] = task.Error
	}
	if task.StartedAt != nil {
		set["started_at"] = task.StartedAt
	}
	if task.CompletedAt != nil {
		set["completed_at"] = task.CompletedAt
	}

	res, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": task.ID, "business_id": task.BusinessID},
		bson.M{"$set": set},
	)
	if err != nil {
		return fmt.Errorf("update agent task: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrAgentTaskNotFound
	}
	return nil
}

func (r *agentTaskRepository) GetByID(ctx context.Context, businessID, taskID string) (*domain.AgentTask, error) {
	var task domain.AgentTask
	err := r.collection.FindOne(ctx, bson.M{"_id": taskID, "business_id": businessID}).Decode(&task)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrAgentTaskNotFound
		}
		return nil, fmt.Errorf("get agent task: %w", err)
	}
	return &task, nil
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
