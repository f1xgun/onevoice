package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestAgentTaskRepository_Update_StampsErrorCode(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.AgentTask{
		BusinessID: "biz-1",
		Type:       "send_channel_post",
		Status:     "running",
		Platform:   "telegram",
		StartedAt:  &now,
	}
	require.NoError(t, repo.Create(ctx, task))
	require.NotEmpty(t, task.ID)

	completed := now.Add(2 * time.Second)
	update := &domain.AgentTask{
		ID:          task.ID,
		BusinessID:  task.BusinessID,
		Status:      "error",
		Error:       "Unauthorized: bot kicked",
		ErrorCode:   "integration_token_invalid",
		CompletedAt: &completed,
	}
	require.NoError(t, repo.Update(ctx, update))

	fetched, err := repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", fetched.Status)
	assert.Equal(t, "Unauthorized: bot kicked", fetched.Error)
	assert.Equal(t, "integration_token_invalid", fetched.ErrorCode)
}

func TestAgentTaskRepository_Update_EmptyErrorCode_LeavesExisting(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.AgentTask{
		BusinessID: "biz-1",
		Type:       "send_channel_post",
		Status:     "error",
		Platform:   "telegram",
		Error:      "first failure",
		ErrorCode:  "rate_limit_exceeded",
		StartedAt:  &now,
	}
	require.NoError(t, repo.Create(ctx, task))

	update := &domain.AgentTask{
		ID:         task.ID,
		BusinessID: task.BusinessID,
		Status:     "running",
	}
	require.NoError(t, repo.Update(ctx, update))

	fetched, err := repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "running", fetched.Status)
	assert.Equal(t, "rate_limit_exceeded", fetched.ErrorCode,
		"empty ErrorCode on update must preserve the existing value")
}

func TestAgentTaskRepository_Find_Queryable_ByErrorCode(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	matching := &domain.AgentTask{
		BusinessID: "biz-q",
		Type:       "send_channel_post",
		Status:     "running",
		Platform:   "telegram",
		StartedAt:  &now,
	}
	require.NoError(t, repo.Create(ctx, matching))

	other := &domain.AgentTask{
		BusinessID: "biz-q",
		Type:       "send_channel_post",
		Status:     "running",
		Platform:   "vk",
		StartedAt:  &now,
	}
	require.NoError(t, repo.Create(ctx, other))

	completed := now.Add(time.Second)
	require.NoError(t, repo.Update(ctx, &domain.AgentTask{
		ID:          matching.ID,
		BusinessID:  matching.BusinessID,
		Status:      "error",
		Error:       "Unauthorized",
		ErrorCode:   "integration_token_invalid",
		CompletedAt: &completed,
	}))

	cursor, err := db.Collection("agent_tasks").Find(ctx, bson.M{
		"business_id": "biz-q",
		"error_code":  "integration_token_invalid",
	})
	require.NoError(t, err)
	var out []domain.AgentTask
	require.NoError(t, cursor.All(ctx, &out))
	require.Len(t, out, 1)
	assert.Equal(t, matching.ID, out[0].ID)
}
