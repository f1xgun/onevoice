package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// stubAgentTaskRepo serves a single task by (business_id, id) and records the
// last Update so a test can assert the persisted retry outcome. Only the methods
// Retry touches are implemented; the embedded interface leaves the rest
// unimplemented so an unexpected call panics loudly.
type stubAgentTaskRepo struct {
	domain.AgentTaskRepository
	task    *domain.AgentTask
	updates []domain.AgentTask
}

func (s *stubAgentTaskRepo) GetByID(_ context.Context, businessID, taskID string) (*domain.AgentTask, error) {
	if s.task == nil || s.task.BusinessID != businessID || s.task.ID != taskID {
		return nil, domain.ErrAgentTaskNotFound
	}
	out := *s.task
	return &out, nil
}

func (s *stubAgentTaskRepo) Update(_ context.Context, task *domain.AgentTask) error {
	s.updates = append(s.updates, *task)
	s.task.Status = task.Status
	s.task.Output = task.Output
	s.task.Error = task.Error
	s.task.ErrorCode = task.ErrorCode
	s.task.CompletedAt = task.CompletedAt
	return nil
}

// dedupeRequester is a NATS stand-in that runs the REAL HITL dedupe gate
// (pkg/agentbase.Dispatcher backed by pkg/hitldedupe + miniredis) in front of a
// counted execution. It is the production idempotency path: the FIRST dispatch
// for an ApprovalID executes and caches its result; a SECOND dispatch with the
// SAME (business_id, approval_id) is served from the dedupe cache and the
// underlying execution never runs a second time.
type dedupeRequester struct {
	dispatcher agentbase.Dispatcher
	execs      int
	lastApp    string
	lastTool   string
	result     map[string]interface{}
}

func newDedupeRequester(t *testing.T, result map[string]interface{}) *dedupeRequester {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	d := &dedupeRequester{result: result}
	d.dispatcher = agentbase.NewDispatcher(hitldedupe.New(rdb), nil)
	return d
}

func (d *dedupeRequester) RequestMsgWithContext(ctx context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	var req a2a.ToolRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, err
	}
	d.lastApp = req.ApprovalID
	d.lastTool = req.Tool
	resp, err := d.dispatcher.Dispatch(ctx, req, func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error) {
		d.execs++
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true, Result: d.result}, nil
	})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &natslib.Msg{Data: data}, nil
}

func failedTask(businessID uuid.UUID, errorCode string) *domain.AgentTask {
	return &domain.AgentTask{
		ID:         "task-1",
		BusinessID: businessID.String(),
		Platform:   a2a.AgentTelegram,
		Type:       "send_channel_post",
		Status:     "error",
		ErrorCode:  errorCode,
		Error:      "boom",
		Input:      map[string]interface{}{"channel_id": "-100", "text": "hi"},
	}
}

// TestRetry_RetryableReDispatchesAndPersistsSuccess asserts a transient-failed
// task re-dispatches the reconstructed tool request and persists the new "done"
// outcome onto the task. Reverting the retryable-code gate (so no task ever
// re-dispatches) or the persistRetryOutcome write fails this test.
func TestRetry_RetryableReDispatchesAndPersistsSuccess(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "transient")}
	nc := newDedupeRequester(t, map[string]interface{}{"message_id": float64(42)})
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	got, err := svc.Retry(context.Background(), biz, "task-1")
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs, "a retryable task must re-dispatch to the platform exactly once")
	require.Equal(t, a2a.AgentTelegram+"__send_channel_post", nc.lastTool, "the retry must reconstruct the original tool name from platform+type")
	require.Equal(t, "done", got.Status, "a successful retry must flip the task to done")
	require.NotEmpty(t, repo.updates, "the retry outcome must be persisted onto the task")
	require.Equal(t, "done", repo.updates[0].Status)
}

// TestRetry_IdempotentReDispatchIsDeduped is the paramount safety property: a
// retry of a call that ALREADY LANDED at the platform does NOT execute a second
// external action. The first dispatch executes and the agent caches the result
// under the stable (business_id, task-retry-<id>) key; a second retry re-sends
// the SAME key and is served from the dedupe cache, so the underlying execution
// count stays at one. Reverting retryApprovalID to a fresh/random key per retry
// (which would bypass the dedupe gate) fails this test.
func TestRetry_IdempotentReDispatchIsDeduped(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "rate_limit_exceeded")}
	nc := newDedupeRequester(t, map[string]interface{}{"ok": true})
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err := svc.Retry(context.Background(), biz, "task-1")
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs)

	repo.task.Status = "error"
	repo.task.ErrorCode = "transient"

	_, err = svc.Retry(context.Background(), biz, "task-1")
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs, "a retry of an already-landed call must be deduped, not executed twice")
	require.Equal(t, "task-retry-task-1", nc.lastApp, "the retry must reuse the stable dedupe key")
}

// TestRetry_ReusesOriginalDispatchApprovalID asserts a task carrying a persisted
// DispatchApprovalID (the "<batch_id>-<call_id>" the approved dispatch first ran
// under) re-dispatches its retry under that SAME key, not the per-task fallback.
// Reverting the reuse in retryApprovalID (so the retry keys on "task-retry-<id>")
// fails this test.
func TestRetry_ReusesOriginalDispatchApprovalID(t *testing.T) {
	biz := uuid.New()
	task := failedTask(biz, "transient")
	task.DispatchApprovalID = "batch-xyz-call-1"
	repo := &stubAgentTaskRepo{task: task}
	nc := newDedupeRequester(t, map[string]interface{}{"ok": true})
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err := svc.Retry(context.Background(), biz, "task-1")
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs)
	require.Equal(t, "batch-xyz-call-1", nc.lastApp,
		"the retry must reuse the original dispatch key so an already-landed call is deduped")
}

// TestRetry_RetryOfLandedTaskIsDedupedAgainstOriginal proves the retry-vs-ORIGINAL
// idempotency property: the original approved dispatch executed under its
// "<batch_id>-<call_id>" key and the agent cached the result; a retry of the
// same task (whose DispatchApprovalID persisted that key) re-sends it and is
// served from the dedupe cache, so the platform execution count stays at one.
// Reverting the key persistence/reuse keys the retry on the per-task fallback,
// which never matches the original dispatch, bypasses the dedupe, and executes a
// second irreversible action — failing this test.
func TestRetry_RetryOfLandedTaskIsDedupedAgainstOriginal(t *testing.T) {
	biz := uuid.New()
	originalKey := "batch-orig-call-5"
	nc := newDedupeRequester(t, map[string]interface{}{"message_id": float64(7)})

	origReq := a2a.ToolRequest{
		TaskID:     "orig",
		Tool:       a2a.AgentTelegram + "__send_channel_post",
		Args:       map[string]interface{}{"channel_id": "-100", "text": "hi"},
		BusinessID: biz.String(),
		ApprovalID: originalKey,
	}
	data, err := json.Marshal(origReq)
	require.NoError(t, err)
	_, err = nc.RequestMsgWithContext(context.Background(), &natslib.Msg{Data: data})
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs, "the original approved dispatch executes once")

	task := failedTask(biz, "transient")
	task.DispatchApprovalID = originalKey
	repo := &stubAgentTaskRepo{task: task}
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err = svc.Retry(context.Background(), biz, "task-1")
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs,
		"a retry of an already-landed task must be deduped against the original dispatch, not executed twice")
	require.Equal(t, originalKey, nc.lastApp, "the retry must re-send the original dispatch key")
}

// TestRetry_TokenInvalidSignalsReconnect asserts an integration_token_invalid
// failure is refused with the reconnect reason and never dispatches. Reverting
// the non-retryable gate lets a doomed re-dispatch fire and fails this test.
func TestRetry_TokenInvalidSignalsReconnect(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "integration_token_invalid")}
	nc := newDedupeRequester(t, nil)
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err := svc.Retry(context.Background(), biz, "task-1")
	require.ErrorIs(t, err, domain.ErrAgentTaskNotRetryable)
	var rejected *RetryRejectedError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, RetryReasonReconnect, rejected.Reason)
	require.Zero(t, nc.execs, "a token-invalid failure must not re-dispatch")
	require.Empty(t, repo.updates, "a rejected retry must not persist a new outcome")
}

// TestRetry_PermanentFailureNotRetryable asserts a media_too_large failure is
// refused with the permanent reason and never dispatches.
func TestRetry_PermanentFailureNotRetryable(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "media_too_large")}
	nc := newDedupeRequester(t, nil)
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err := svc.Retry(context.Background(), biz, "task-1")
	var rejected *RetryRejectedError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, RetryReasonPermanent, rejected.Reason)
	require.Zero(t, nc.execs)
}

// TestRetry_NonFailedTaskRejected asserts a task that is not in the failed state
// is refused and never dispatches.
func TestRetry_NonFailedTaskRejected(t *testing.T) {
	biz := uuid.New()
	task := failedTask(biz, "transient")
	task.Status = "done"
	repo := &stubAgentTaskRepo{task: task}
	nc := newDedupeRequester(t, nil)
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err := svc.Retry(context.Background(), biz, "task-1")
	require.ErrorIs(t, err, domain.ErrAgentTaskNotFailed)
	require.Zero(t, nc.execs)
}

// TestRetry_UnknownTaskRejected asserts an unknown / cross-business task id
// resolves to ErrAgentTaskNotFound (the repository (business_id, id) key is the
// ownership gate).
func TestRetry_UnknownTaskRejected(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "transient")}
	nc := newDedupeRequester(t, nil)
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	_, err := svc.Retry(context.Background(), uuid.New(), "task-1")
	require.ErrorIs(t, err, domain.ErrAgentTaskNotFound)
	require.Zero(t, nc.execs, "a cross-business task must not re-dispatch")
}

// TestRetry_SoftDeletedBusinessBlocksReDispatch asserts a retry for a
// soft-deleted organization returns domain.ErrBusinessNotFound and never
// dispatches. Reverting the gateBusiness call lets the re-dispatch fire for a
// business inside its deletion grace window and fails this test.
func TestRetry_SoftDeletedBusinessBlocksReDispatch(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "transient")}
	nc := newDedupeRequester(t, nil)
	svc := &agentTaskService{
		repo:            repo,
		businessService: &softDeletedBusinessService{},
		nc:              nc,
		dispatchTimeout: time.Second,
	}

	_, err := svc.Retry(context.Background(), biz, "task-1")
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.Zero(t, nc.execs, "a soft-deleted business must not re-dispatch")
	require.Empty(t, repo.updates)
}

// TestRetry_FailedDispatchPersistsNewClassifier asserts that when the
// re-dispatch itself fails at the platform, the task stays in "error" with the
// fresh classifier code preserved (so the row still shows a retryable state).
func TestRetry_FailedDispatchPersistsNewClassifier(t *testing.T) {
	biz := uuid.New()
	repo := &stubAgentTaskRepo{task: failedTask(biz, "transient")}
	nc := &codedFailRequester{code: "channel_not_found"}
	svc := &agentTaskService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	got, err := svc.Retry(context.Background(), biz, "task-1")
	require.NoError(t, err)
	require.Equal(t, "error", got.Status)
	require.Equal(t, "channel_not_found", got.ErrorCode, "the fresh failure classifier must be persisted")
}

// codedFailRequester always replies with a coded agent failure so a test can
// assert the retry outcome persists the new classifier.
type codedFailRequester struct {
	code string
}

func (c *codedFailRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	var req a2a.ToolRequest
	_ = json.Unmarshal(msg.Data, &req)
	resp, _ := json.Marshal(a2a.ToolResponse{TaskID: req.TaskID, Success: false, Error: "not found", Code: c.code})
	return &natslib.Msg{Data: resp}, nil
}
