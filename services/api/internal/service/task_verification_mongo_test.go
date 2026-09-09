package service

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

func TestTaskVerificationShutdownRejectsAndTerminalizesPendingMongoJob(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("requires explicit MONGODB_TEST_URI")
	}
	ctx, cancelConnect := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelConnect()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()
	db := client.Database("ov_verification_shutdown_" + uuid.NewString())
	defer func() { _ = db.Drop(context.Background()) }()
	repo := repository.NewAgentTaskRepository(db)
	task := &domain.AgentTask{BusinessID: "biz-shutdown", Type: "sync_title", Status: "done", Platform: "telegram", VerificationStatus: domain.VerificationPending, VerificationExpected: map[string]string{"title": "Cafe"}, VerificationTarget: "channel", VerificationAttempt: 1, VerificationUpdatedAt: ptrVerificationTime(time.Now())}
	require.NoError(t, repo.Create(ctx, task))
	runner := NewTaskVerification(repo, nil, nil, nil, nil, nil, 1)
	lifecycle, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	runner.Start(lifecycle, 1, &wg)
	cancel()
	wg.Wait()
	require.ErrorIs(t, runner.Enqueue(task.BusinessID, task.ID, 1), ErrVerificationQueueFull)
	fetched, err := repo.GetByID(context.Background(), task.BusinessID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationError, fetched.VerificationStatus)
	assert.Equal(t, "scheduler_stopped", fetched.VerificationErrorCode)

	dequeued := &domain.AgentTask{BusinessID: "biz-dequeued", Type: "sync_title", Status: "done", Platform: "telegram", VerificationStatus: domain.VerificationPending, VerificationExpected: map[string]string{"title": "Cafe"}, VerificationTarget: "channel", VerificationAttempt: 1, VerificationUpdatedAt: ptrVerificationTime(time.Now())}
	require.NoError(t, repo.Create(context.Background(), dequeued))
	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	runner.verify(canceled, verificationJob{businessID: dequeued.BusinessID, taskID: dequeued.ID, attempt: 1})
	gotDequeued, err := repo.GetByID(context.Background(), dequeued.BusinessID, dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationError, gotDequeued.VerificationStatus)
	assert.Equal(t, "interrupted", gotDequeued.VerificationErrorCode)
}

func ptrVerificationTime(value time.Time) *time.Time { return &value }
