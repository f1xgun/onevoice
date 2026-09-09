package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

const verificationReadTimeout = 90 * time.Second
const verificationStaleAfter = 2 * time.Hour
const verificationRecoveryInterval = 10 * time.Minute

var (
	ErrVerificationBusy        = errors.New("task verification already queued or running")
	ErrVerificationUnavailable = errors.New("task verification unavailable")
	ErrVerificationQueueFull   = errors.New("task verification queue full")
)

type verificationJob struct {
	businessID, taskID string
	attempt            int64
}

// TaskVerification runs bounded readbacks. Start is called exactly once at
// process startup, so its fixed workers can be joined without WaitGroup races.
type TaskVerification struct {
	repo         domain.AgentTaskRepository
	businesses   domain.BusinessRepository
	integrations domain.IntegrationRepository
	readers      map[string]platform.RemoteFetcher
	nc           natsRequester
	hub          *taskhub.Hub
	jobs         chan verificationJob
	lifecycle    context.Context
	mu           sync.Mutex
	accepting    bool
}

func NewTaskVerification(repo domain.AgentTaskRepository, businesses domain.BusinessRepository, integrations domain.IntegrationRepository, readers map[string]platform.RemoteFetcher, nc natsRequester, hub *taskhub.Hub, queueSize int) *TaskVerification {
	if queueSize <= 0 {
		queueSize = 64
	}
	return &TaskVerification{repo: repo, businesses: businesses, integrations: integrations, readers: readers, nc: nc, hub: hub, jobs: make(chan verificationJob, queueSize)}
}

func (v *TaskVerification) Start(ctx context.Context, workers int, wg *sync.WaitGroup) {
	v.lifecycle = ctx
	v.mu.Lock()
	v.accepting = true
	v.mu.Unlock()
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	if err := v.repo.RecoverStaleVerifications(recoveryCtx, time.Now().Add(-verificationStaleAfter)); err != nil {
		slog.ErrorContext(recoveryCtx, "task verification: initial stale recovery failed", "error", err)
	}
	cancel()
	if workers <= 0 {
		workers = 2
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v.worker(ctx) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); v.maintain(ctx) }()
}

func (v *TaskVerification) Enqueue(businessID, taskID string, attempt int64) error {
	job := verificationJob{businessID: businessID, taskID: taskID, attempt: attempt}
	v.mu.Lock()
	if !v.accepting {
		v.mu.Unlock()
		v.recoverJob(job, "scheduler_stopped")
		return ErrVerificationQueueFull
	}
	var enqueueErr error
	select {
	case v.jobs <- job:
	default:
		enqueueErr = ErrVerificationQueueFull
	}
	v.mu.Unlock()
	if enqueueErr != nil {
		v.recoverJob(job, "queue_full")
	}
	return enqueueErr
}

func (v *TaskVerification) maintain(ctx context.Context) {
	ticker := time.NewTicker(verificationRecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := v.repo.RecoverStaleVerifications(recoveryCtx, time.Now().Add(-verificationStaleAfter)); err != nil {
				slog.ErrorContext(recoveryCtx, "task verification: periodic stale recovery failed", "error", err)
			}
			cancel()
		case <-ctx.Done():
			v.mu.Lock()
			v.accepting = false
			queued := make([]verificationJob, 0, len(v.jobs))
			for {
				select {
				case job := <-v.jobs:
					queued = append(queued, job)
				default:
					v.mu.Unlock()
					recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					for _, job := range queued {
						v.recoverJobContext(recoveryCtx, job, "scheduler_stopped")
					}
					cancel()
					return
				}
			}
		}
	}
}

func (v *TaskVerification) recoverJob(job verificationJob, code string) {
	base := context.Background()
	if v.lifecycle != nil {
		base = context.WithoutCancel(v.lifecycle)
	}
	ctx, cancel := context.WithTimeout(base, 3*time.Second)
	defer cancel()
	v.recoverJobContext(ctx, job, code)
}

func (v *TaskVerification) recoverJobContext(ctx context.Context, job verificationJob, code string) {
	task, err := v.repo.GetByID(ctx, job.businessID, job.taskID)
	if err != nil {
		if !errors.Is(err, domain.ErrAgentTaskNotFound) {
			slog.ErrorContext(ctx, "task verification: failed to load queued task for recovery", "business_id", job.businessID, "task_id", job.taskID, "error", err)
		}
		return
	}
	now := time.Now()
	if err := v.repo.UpdateVerification(ctx, job.businessID, job.taskID, domain.AgentTaskVerificationUpdate{ExpectedStatus: domain.VerificationPending, Attempt: job.attempt, Status: domain.VerificationError, Fields: sortedVerificationKeys(task.VerificationExpected), ErrorCode: code, CheckedAt: &now}); err != nil {
		if !errors.Is(err, domain.ErrAgentTaskNotFound) {
			slog.ErrorContext(ctx, "task verification: failed to persist queued task recovery", "business_id", job.businessID, "task_id", job.taskID, "error", err)
		}
		return
	}
	v.publish(ctx, job.businessID, job.taskID)
}

func (v *TaskVerification) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-v.jobs:
			if ctx.Err() != nil {
				v.recoverJob(job, "scheduler_stopped")
				continue
			}
			v.verify(ctx, job)
		}
	}
}

func (v *TaskVerification) verify(parent context.Context, job verificationJob) {
	task, err := v.repo.GetByID(parent, job.businessID, job.taskID)
	if err != nil {
		if parent.Err() != nil {
			v.recoverJob(job, "interrupted")
		}
		return
	}
	if task.VerificationAttempt != job.attempt {
		return
	}
	fields := sortedVerificationKeys(task.VerificationExpected)
	if err := v.repo.UpdateVerification(parent, job.businessID, job.taskID, domain.AgentTaskVerificationUpdate{ExpectedStatus: domain.VerificationPending, Attempt: job.attempt, Status: domain.VerificationRunning, Fields: fields}); err != nil {
		if parent.Err() != nil {
			v.recoverJob(job, "interrupted")
		}
		return
	}
	v.publish(parent, job.businessID, job.taskID)

	status, mismatches, code := domain.VerificationUnverifiable, []string(nil), ""
	if task.VerificationTarget != "" && len(task.VerificationExpected) > 0 {
		ctx, cancel := context.WithTimeout(parent, verificationReadTimeout)
		defer cancel()
		bizID, parseErr := uuid.Parse(job.businessID)
		if parseErr != nil {
			code = "invalid_intent"
		} else {
			biz, bizErr := v.businesses.GetByID(ctx, bizID)
			integ, integErr := v.integrations.GetByBusinessPlatformExternal(ctx, bizID, task.Platform, task.VerificationTarget)
			switch {
			case bizErr != nil || integErr != nil || integ == nil || integ.Status != domain.IntegrationStatusActive:
				status, code = domain.VerificationError, "readback_unavailable"
			case task.Platform == a2a.AgentYandexBusiness:
				snapshot, readErr := v.fetchYandex(ctx, biz, task.VerificationTarget)
				switch {
				case readErr != nil:
					status, code = domain.VerificationError, "readback_failed"
				case snapshot.Err != "":
					status, code = domain.VerificationError, "platform_error"
				case task.Type == "sync_hours" || task.Type == "update_hours":
					status = domain.VerificationUnverifiable
				default:
					status, mismatches = CompareVerification(task.VerificationExpected, snapshot.Fields)
				}
			default:
				reader := v.readers[task.Platform]
				if reader == nil {
					code = "readback_unavailable"
				} else {
					snapshot, readErr := reader.FetchRemote(ctx, biz, *integ)
					switch {
					case readErr != nil:
						status, code = domain.VerificationError, "readback_failed"
					case snapshot.Err != "":
						status, code = domain.VerificationError, "platform_error"
					default:
						status, mismatches = CompareVerification(task.VerificationExpected, snapshot.Fields)
					}
				}
			}
		}
	}
	if parent.Err() != nil {
		status, code = domain.VerificationError, "interrupted"
	}
	now := time.Now()
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer persistCancel()
	if err := v.repo.UpdateVerification(persistCtx, job.businessID, job.taskID, domain.AgentTaskVerificationUpdate{ExpectedStatus: domain.VerificationRunning, Attempt: job.attempt, Status: status, Fields: fields, Mismatches: mismatches, ErrorCode: code, CheckedAt: &now}); err != nil {
		slog.ErrorContext(persistCtx, "task verification: failed to persist terminal result", "business_id", job.businessID, "task_id", job.taskID, "status", status, "error", err)
		return
	}
	v.publish(persistCtx, job.businessID, job.taskID)
}

func (v *TaskVerification) fetchYandex(ctx context.Context, business *domain.Business, target string) (platform.RemoteSnapshot, error) {
	if v.nc == nil {
		return platform.RemoteSnapshot{}, errors.New("nats unavailable")
	}
	resp, err := dispatchTool(ctx, v.nc, a2a.AgentYandexBusiness, tools.YandexBusinessGetInfo, map[string]interface{}{"external_id": target}, business.ID.String(), verificationReadTimeout)
	if err != nil {
		return platform.RemoteSnapshot{}, err
	}
	fields := map[string]string{}
	for key, field := range map[string]string{"name": platform.FieldTitle, "description": platform.FieldDescription, "phone": platform.FieldPhone, "hours": platform.FieldSchedule} {
		if value, present := resp.Result[key].(string); present {
			fields[field] = value
		}
	}
	return platform.RemoteSnapshot{Fields: fields}, nil
}

func (v *TaskVerification) publish(ctx context.Context, businessID, taskID string) {
	if v.hub == nil {
		return
	}
	task, err := v.repo.GetByID(ctx, businessID, taskID)
	if err == nil {
		v.hub.Publish(businessID, taskhub.Event{Kind: taskhub.KindUpdated, Task: *task})
	}
}

// CompareVerification uses exact comparison and requires field presence.
func CompareVerification(expected, remote map[string]string) (status string, mismatches []string) {
	if len(expected) == 0 {
		return domain.VerificationUnverifiable, nil
	}
	mismatch := make([]string, 0)
	for field, want := range expected {
		got, present := remote[field]
		if !present {
			return domain.VerificationUnverifiable, nil
		}
		if got != want {
			mismatch = append(mismatch, field)
		}
	}
	sort.Strings(mismatch)
	if len(mismatch) > 0 {
		return domain.VerificationMismatch, mismatch
	}
	return domain.VerificationVerified, nil
}

func sortedVerificationKeys(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func verificationIntent(platformID, taskType string, input interface{}) (expected map[string]string, target, status string) {
	args, ok := coerceArgs(input)
	if !ok {
		return nil, "", domain.VerificationUnverifiable
	}
	str := func(key string) (string, bool) { value, found := args[key].(string); return value, found }
	switch platformID + "__" + taskType {
	case "telegram__sync_title":
		target, ok1 := str("channel_id")
		value, ok2 := str("name")
		if ok1 && ok2 {
			return map[string]string{platform.FieldTitle: value}, target, domain.VerificationPending
		}
	case "telegram__sync_description":
		target, ok1 := str("channel_id")
		value, ok2 := str("description")
		if ok1 && ok2 {
			return map[string]string{platform.FieldDescription: value}, target, domain.VerificationPending
		}
	case tools.VKUpdateGroupInfo, "vk__sync_info":
		target, ok := str("group_id")
		if !ok {
			break
		}
		expected := map[string]string{}
		for key, field := range map[string]string{"title": platform.FieldTitle, "description": platform.FieldDescription, "website": platform.FieldWebsite} {
			if value, found := str(key); found {
				expected[field] = value
			}
		}
		if phone, found := str("phone"); found && phone != "" {
			expected[platform.FieldPhone] = phone
		}
		if len(expected) > 0 {
			return expected, target, domain.VerificationPending
		}
	case tools.YandexBusinessUpdateHours, "yandex_business__sync_hours":
		target, ok1 := str("permalink")
		hours, ok2 := str("hours")
		if ok1 && ok2 {
			return map[string]string{platform.FieldSchedule: hours}, target, domain.VerificationPending
		}
	}
	return nil, "", domain.VerificationUnsupported
}
