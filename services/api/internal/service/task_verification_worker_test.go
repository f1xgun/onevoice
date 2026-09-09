package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

type verificationTaskRepo struct {
	domain.AgentTaskRepository
	mu   sync.Mutex
	task domain.AgentTask
}

type blockingRecoveryRepo struct {
	domain.AgentTaskRepository
	calls atomic.Int64
}

func (r *blockingRecoveryRepo) GetByID(ctx context.Context, _, _ string) (*domain.AgentTask, error) {
	r.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *verificationTaskRepo) GetByID(_ context.Context, businessID, taskID string) (*domain.AgentTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.task.BusinessID != businessID || r.task.ID != taskID {
		return nil, domain.ErrAgentTaskNotFound
	}
	out := r.task
	return &out, nil
}
func (r *verificationTaskRepo) UpdateVerification(_ context.Context, businessID, taskID string, update domain.AgentTaskVerificationUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.task.BusinessID != businessID || r.task.ID != taskID || r.task.VerificationAttempt != update.Attempt || update.ExpectedStatus != "" && r.task.VerificationStatus != update.ExpectedStatus {
		return domain.ErrAgentTaskNotFound
	}
	r.task.VerificationStatus, r.task.VerificationFields, r.task.VerificationMismatches, r.task.VerificationErrorCode, r.task.VerificationCheckedAt = update.Status, update.Fields, update.Mismatches, update.ErrorCode, update.CheckedAt
	return nil
}

type verificationBusinessRepo struct {
	domain.BusinessRepository
	business *domain.Business
	err      error
}

func (r verificationBusinessRepo) GetByID(context.Context, uuid.UUID) (*domain.Business, error) {
	return r.business, r.err
}

type verificationIntegrationRepo struct {
	domain.IntegrationRepository
	integration *domain.Integration
	err         error
}

func (r verificationIntegrationRepo) GetByBusinessPlatformExternal(context.Context, uuid.UUID, string, string) (*domain.Integration, error) {
	return r.integration, r.err
}

type verificationReader struct {
	snapshot     platform.RemoteSnapshot
	err          error
	calls        int
	seenBusiness string
}

func (r *verificationReader) FetchRemote(_ context.Context, business *domain.Business, _ domain.Integration) (platform.RemoteSnapshot, error) {
	r.calls++
	r.seenBusiness = business.Name
	return r.snapshot, r.err
}

func verificationFixture(snapshot platform.RemoteSnapshot, readErr error) (*TaskVerification, *verificationTaskRepo, *verificationReader, <-chan taskhub.Event) {
	businessID := uuid.New()
	repo := &verificationTaskRepo{task: domain.AgentTask{ID: "task", BusinessID: businessID.String(), Type: "sync_title", Status: "done", Platform: a2a.AgentTelegram, VerificationStatus: domain.VerificationPending, VerificationExpected: map[string]string{platform.FieldTitle: "Frozen name"}, VerificationTarget: "channel", VerificationAttempt: 1}}
	reader := &verificationReader{snapshot: snapshot, err: readErr}
	hub := taskhub.New()
	events, _ := hub.Subscribe(businessID.String())
	verifier := NewTaskVerification(repo, verificationBusinessRepo{business: &domain.Business{ID: businessID, Name: "Changed current name"}}, verificationIntegrationRepo{integration: &domain.Integration{BusinessID: businessID, Platform: a2a.AgentTelegram, ExternalID: "channel", Status: domain.IntegrationStatusActive}}, map[string]platform.RemoteFetcher{a2a.AgentTelegram: reader}, nil, hub, 1)
	return verifier, repo, reader, events
}

func TestTaskVerificationWorkerUsesFrozenIntentAndPublishesFreshMismatch(t *testing.T) {
	verifier, repo, reader, events := verificationFixture(platform.RemoteSnapshot{Fields: map[string]string{platform.FieldTitle: "Changed current name"}}, nil)
	verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
	assert.Equal(t, "Changed current name", reader.seenBusiness)
	assert.Equal(t, "done", repo.task.Status, "readback must preserve the successful write result")
	assert.Equal(t, domain.VerificationMismatch, repo.task.VerificationStatus)
	assert.Equal(t, []string{platform.FieldTitle}, repo.task.VerificationMismatches)
	running := <-events
	terminal := <-events
	assert.Equal(t, domain.VerificationRunning, running.Task.VerificationStatus)
	assert.Equal(t, domain.VerificationMismatch, terminal.Task.VerificationStatus)
}

func TestTaskVerificationWorkerClassifiesMissingAndReadFailure(t *testing.T) {
	t.Run("missing field", func(t *testing.T) {
		verifier, repo, _, _ := verificationFixture(platform.RemoteSnapshot{Fields: map[string]string{}}, nil)
		verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
		assert.Equal(t, domain.VerificationUnverifiable, repo.task.VerificationStatus)
	})
	t.Run("reader error", func(t *testing.T) {
		verifier, repo, _, _ := verificationFixture(platform.RemoteSnapshot{}, errors.New("offline"))
		verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
		assert.Equal(t, domain.VerificationError, repo.task.VerificationStatus)
		assert.Equal(t, "readback_failed", repo.task.VerificationErrorCode)
	})
}

func TestTaskVerificationWorkerDoesNotReadDeletedOrDisconnectedTarget(t *testing.T) {
	t.Run("deleted business", func(t *testing.T) {
		verifier, repo, reader, _ := verificationFixture(platform.RemoteSnapshot{}, nil)
		verifier.businesses = verificationBusinessRepo{err: domain.ErrBusinessNotFound}
		verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
		assert.Zero(t, reader.calls)
		assert.Equal(t, domain.VerificationError, repo.task.VerificationStatus)
	})
	t.Run("disconnected target", func(t *testing.T) {
		verifier, repo, reader, _ := verificationFixture(platform.RemoteSnapshot{}, nil)
		verifier.integrations = verificationIntegrationRepo{err: domain.ErrIntegrationNotFound}
		verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
		assert.Zero(t, reader.calls)
		assert.Equal(t, domain.VerificationError, repo.task.VerificationStatus)
	})
	t.Run("non-active target", func(t *testing.T) {
		verifier, repo, reader, _ := verificationFixture(platform.RemoteSnapshot{}, nil)
		verifier.integrations = verificationIntegrationRepo{integration: &domain.Integration{Status: domain.IntegrationStatusTokenExpired}}
		verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
		assert.Zero(t, reader.calls)
		assert.Equal(t, domain.VerificationError, repo.task.VerificationStatus)
		assert.Equal(t, "readback_unavailable", repo.task.VerificationErrorCode)
	})
}

func TestTaskVerificationShutdownBoundsRecoveryAcrossFullQueue(t *testing.T) {
	repo := &blockingRecoveryRepo{}
	verifier := NewTaskVerification(repo, nil, nil, nil, nil, nil, 64)
	verifier.accepting = true
	for i := 0; i < cap(verifier.jobs); i++ {
		verifier.jobs <- verificationJob{businessID: "business", taskID: string(rune(i)), attempt: 1}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	verifier.maintain(ctx)

	assert.Less(t, time.Since(started), 6*time.Second)
	assert.Equal(t, int64(64), repo.calls.Load())
}

type yandexVerificationRequester struct {
	request   a2a.ToolRequest
	remaining time.Duration
	decodeErr error
}

func (r *yandexVerificationRequester) RequestMsgWithContext(ctx context.Context, msg *nats.Msg) (*nats.Msg, error) {
	r.decodeErr = json.Unmarshal(msg.Data, &r.request)
	if deadline, ok := ctx.Deadline(); ok {
		r.remaining = time.Until(deadline)
	}
	response, err := json.Marshal(a2a.ToolResponse{TaskID: r.request.TaskID, Success: true, Result: map[string]interface{}{"hours": "09:00–18:00"}})
	if err != nil {
		return nil, err
	}
	return &nats.Msg{Data: response}, nil
}

func TestTaskVerificationYandexUsesFrozenTargetAndLeavesHoursUnverifiable(t *testing.T) {
	businessID := uuid.New()
	repo := &verificationTaskRepo{task: domain.AgentTask{ID: "hours", BusinessID: businessID.String(), Type: "update_hours", Status: "done", Platform: a2a.AgentYandexBusiness, VerificationStatus: domain.VerificationPending, VerificationExpected: map[string]string{platform.FieldSchedule: `{"monday":"09:00-18:00"}`}, VerificationTarget: "frozen-permalink", VerificationAttempt: 1}}
	requester := &yandexVerificationRequester{}
	verifier := NewTaskVerification(repo, verificationBusinessRepo{business: &domain.Business{ID: businessID}}, verificationIntegrationRepo{integration: &domain.Integration{BusinessID: businessID, Platform: a2a.AgentYandexBusiness, ExternalID: "frozen-permalink", Status: domain.IntegrationStatusActive}}, nil, requester, nil, 1)
	verifier.verify(context.Background(), verificationJob{businessID: repo.task.BusinessID, taskID: repo.task.ID, attempt: 1})
	require.NoError(t, requester.decodeErr)
	require.Equal(t, a2a.AgentYandexBusiness+"__get_info", requester.request.Tool)
	assert.Equal(t, "frozen-permalink", requester.request.Args["external_id"])
	assert.Greater(t, requester.remaining, 89*time.Second)
	assert.LessOrEqual(t, requester.remaining, verificationReadTimeout)
	assert.Equal(t, domain.VerificationUnverifiable, repo.task.VerificationStatus)
}
