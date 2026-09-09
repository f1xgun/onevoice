package repository

import (
	"context"
	"sync"
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

func TestAgentTaskRepository_RestartVerificationCASAllowsOneRerun(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()
	task := &domain.AgentTask{BusinessID: "biz-cas", Type: "sync_title", Status: "done", Platform: "telegram", VerificationStatus: domain.VerificationVerified, VerificationExpected: map[string]string{"title": "Cafe"}, VerificationTarget: "channel", VerificationAttempt: 1}
	require.NoError(t, repo.Create(ctx, task))
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := repo.RestartVerification(ctx, task.BusinessID, task.ID)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	fetched, err := repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationPending, fetched.VerificationStatus)
	assert.EqualValues(t, 2, fetched.VerificationAttempt)
}

func TestAgentTaskRepository_RecoveryLeavesCurrentOtherInstanceLeaseAlone(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()
	now := time.Now()
	stale := &domain.AgentTask{BusinessID: "biz-stale", Type: "sync_title", Status: "done", Platform: "telegram", VerificationStatus: domain.VerificationRunning, VerificationAttempt: 1, VerificationUpdatedAt: ptrTime(now.Add(-3 * time.Hour))}
	current := &domain.AgentTask{BusinessID: "biz-current", Type: "sync_title", Status: "done", Platform: "telegram", VerificationStatus: domain.VerificationRunning, VerificationAttempt: 1, VerificationUpdatedAt: ptrTime(now)}
	require.NoError(t, repo.Create(ctx, stale))
	require.NoError(t, repo.Create(ctx, current))
	require.NoError(t, repo.RecoverStaleVerifications(ctx, now.Add(-2*time.Hour)))
	gotStale, _ := repo.GetByID(ctx, stale.BusinessID, stale.ID)
	gotCurrent, _ := repo.GetByID(ctx, current.BusinessID, current.ID)
	assert.Equal(t, domain.VerificationError, gotStale.VerificationStatus)
	assert.Equal(t, "interrupted", gotStale.VerificationErrorCode)
	assert.Equal(t, domain.VerificationRunning, gotCurrent.VerificationStatus)
}

func ptrTime(value time.Time) *time.Time { return &value }

// TestAgentTaskRepository_Update_DoneClearsError proves a retried task that
// succeeds no longer carries its prior failure: transitioning to "done" unsets
// error/error_code so the tasks list shows a clean success. Reverting the
// $unset on the done transition leaves the stale error and fails this test.
func TestAgentTaskRepository_Update_DoneClearsError(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.AgentTask{
		BusinessID: "biz-1",
		Type:       "send_channel_post",
		Status:     "error",
		Platform:   "telegram",
		Error:      "temporary network blip",
		ErrorCode:  "transient",
		StartedAt:  &now,
	}
	require.NoError(t, repo.Create(ctx, task))

	completed := now.Add(3 * time.Second)
	require.NoError(t, repo.Update(ctx, &domain.AgentTask{
		ID:          task.ID,
		BusinessID:  task.BusinessID,
		Status:      "done",
		Output:      map[string]interface{}{"message_id": int64(7)},
		CompletedAt: &completed,
	}))

	fetched, err := repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", fetched.Status)
	assert.Empty(t, fetched.Error, "a done retry must clear the prior error text")
	assert.Empty(t, fetched.ErrorCode, "a done retry must clear the prior error code")
}

func TestAgentTaskRepository_SparseCompletionPreservesFrozenVerificationIntent(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()
	task := &domain.AgentTask{BusinessID: "biz-verification", Type: "update_group_info", Status: "running", Platform: "vk", VerificationExpected: map[string]string{"title": "Exact Name", "phone": "+7 900 000-00-00"}, VerificationTarget: "group-42", VerificationAttempt: 7}
	require.NoError(t, repo.Create(ctx, task))
	require.NoError(t, repo.Update(ctx, &domain.AgentTask{ID: task.ID, BusinessID: task.BusinessID, Status: "done", VerificationStatus: domain.VerificationPending}))

	fetched, err := repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.VerificationExpected, fetched.VerificationExpected)
	assert.Equal(t, "group-42", fetched.VerificationTarget)
	assert.EqualValues(t, 7, fetched.VerificationAttempt)
	assert.Equal(t, domain.VerificationPending, fetched.VerificationStatus)

	now := time.Now()
	require.NoError(t, repo.UpdateVerification(ctx, task.BusinessID, task.ID, domain.AgentTaskVerificationUpdate{ExpectedStatus: domain.VerificationPending, Attempt: 7, Status: domain.VerificationError, Fields: []string{"phone", "title"}, ErrorCode: "readback_failed", CheckedAt: &now}))
	fetched, err = repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "readback_failed", fetched.VerificationErrorCode)
	assert.Equal(t, task.VerificationExpected, fetched.VerificationExpected)
}

func TestAgentTaskRepository_FailedUnsupportedWriteClearsVerificationWithoutConflict(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewAgentTaskRepository(db)
	ctx := context.Background()
	task := &domain.AgentTask{BusinessID: "biz-failed", Type: "sync_photo", Status: "running", Platform: "telegram", VerificationStatus: domain.VerificationUnsupported}
	require.NoError(t, repo.Create(ctx, task))
	require.NoError(t, repo.Update(ctx, &domain.AgentTask{ID: task.ID, BusinessID: task.BusinessID, Status: "error", Error: "write failed", VerificationStatus: domain.VerificationUnsupported}))
	fetched, err := repo.GetByID(ctx, task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Empty(t, fetched.VerificationStatus)
	assert.Equal(t, "error", fetched.Status)
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

func TestEnsureAgentTaskIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureAgentTaskIndexes(ctx, db), "first call")
	require.NoError(t, EnsureAgentTaskIndexes(ctx, db), "second call (idempotent)")

	specs, err := db.Collection("agent_tasks").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	assert.True(t, names["agent_tasks_business_created_desc"],
		"named index agent_tasks_business_created_desc must exist")
}
