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

// taskStatusDone is the terminal success status. On this transition Update
// clears any stale error/error_code so a retried task that succeeds no longer
// carries its prior failure in the tasks list.
const taskStatusDone = "done"

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
	if task.DisplayNameKey != "" {
		set["display_name_key"] = task.DisplayNameKey
	}
	if task.Output != nil {
		set["output"] = task.Output
	}
	if task.Error != "" {
		set["error"] = task.Error
	}
	if task.ErrorCode != "" {
		set["error_code"] = task.ErrorCode
	}
	if task.StartedAt != nil {
		set["started_at"] = task.StartedAt
	}
	if task.CompletedAt != nil {
		set["completed_at"] = task.CompletedAt
	}
	if task.VerificationStatus != "" && task.Status != "error" {
		set["verification_status"] = task.VerificationStatus
		set["verification_updated_at"] = time.Now()
	}
	if task.VerificationExpected != nil {
		set["verification_expected"] = task.VerificationExpected
	}
	if task.VerificationTarget != "" {
		set["verification_target"] = task.VerificationTarget
	}
	if task.VerificationAttempt > 0 {
		set["verification_attempt"] = task.VerificationAttempt
	}
	if task.VerificationFields != nil {
		set["verification_fields"] = task.VerificationFields
	}
	if task.VerificationExpected != nil && task.VerificationTarget == "" {
		set["verification_target"] = task.VerificationTarget
	}

	update := bson.M{"$set": set}
	switch task.Status {
	case taskStatusDone:
		update["$unset"] = bson.M{"error": "", "error_code": ""}
	case domain.AgentTaskStatusError:
		update["$unset"] = bson.M{"verification_status": "", "verification_mismatches": "", "verification_error_code": "", "verification_checked_at": ""}
	}

	res, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": task.ID, "business_id": task.BusinessID},
		update,
	)
	if err != nil {
		return fmt.Errorf("update agent task: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrAgentTaskNotFound
	}
	return nil
}

func (r *agentTaskRepository) UpdateVerification(ctx context.Context, businessID, taskID string, v domain.AgentTaskVerificationUpdate) error {
	filter := bson.M{"_id": taskID, "business_id": businessID, "verification_attempt": v.Attempt}
	if v.ExpectedStatus != "" {
		filter["verification_status"] = v.ExpectedStatus
	}
	set := bson.M{"verification_status": v.Status, "verification_fields": v.Fields, "verification_updated_at": time.Now()}
	unset := bson.M{}
	if v.CheckedAt != nil {
		set["verification_checked_at"] = v.CheckedAt
	}
	if len(v.Mismatches) > 0 {
		set["verification_mismatches"] = v.Mismatches
	} else {
		unset["verification_mismatches"] = ""
	}
	if v.ErrorCode != "" {
		set["verification_error_code"] = v.ErrorCode
	} else {
		unset["verification_error_code"] = ""
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("update agent task verification: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrAgentTaskNotFound
	}
	return nil
}

func (r *agentTaskRepository) RestartVerification(ctx context.Context, businessID, taskID string) (*domain.AgentTask, error) {
	filter := bson.M{"_id": taskID, "business_id": businessID, "status": taskStatusDone,
		"verification_status": bson.M{"$nin": []string{domain.VerificationPending, domain.VerificationRunning}}}
	update := bson.M{"$set": bson.M{"verification_status": domain.VerificationPending, "verification_updated_at": time.Now()}, "$inc": bson.M{"verification_attempt": 1},
		"$unset": bson.M{"verification_mismatches": "", "verification_error_code": "", "verification_checked_at": ""}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var task domain.AgentTask
	if err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&task); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrAgentTaskNotFound
		}
		return nil, fmt.Errorf("restart agent task verification: %w", err)
	}
	return &task, nil
}

func (r *agentTaskRepository) RecoverStaleVerifications(ctx context.Context, before time.Time) error {
	filter := bson.M{"verification_status": bson.M{"$in": []string{domain.VerificationPending, domain.VerificationRunning}}, "verification_updated_at": bson.M{"$lt": before}}
	update := bson.M{"$set": bson.M{"verification_status": domain.VerificationError, "verification_error_code": "interrupted", "verification_checked_at": time.Now(), "verification_updated_at": time.Now()}}
	if _, err := r.collection.UpdateMany(ctx, filter, update); err != nil {
		return fmt.Errorf("recover stale task verifications: %w", err)
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

// EnsureAgentTaskIndexes creates the agent_tasks collection's compound index
// idempotently at API startup. {business_id, created_at} serves ListByBusinessID
// (filter business_id, sort created_at desc) and prefixes its CountDocuments;
// the optional platform/status/type filters are low-cardinality, so a single
// business_id+created_at index keeps the sort indexed without index sprawl.
// created_at is stamped once at insert and never mutated, so indexing it is safe.
func EnsureAgentTaskIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("agent_tasks")
	model := mongo.IndexModel{
		Keys: bson.D{
			{Key: "business_id", Value: 1},
			{Key: "created_at", Value: -1},
		},
		Options: options.Index().SetName("agent_tasks_business_created_desc"),
	}
	if _, err := coll.Indexes().CreateOne(ctx, model); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("ensure agent task indexes: %w", err)
	}
	return nil
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
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}})

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
