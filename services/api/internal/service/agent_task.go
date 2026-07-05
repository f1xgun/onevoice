package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// retryDispatchTimeout caps the per-platform NATS request budget for a task
// retry. It mirrors the review-reply dispatch budget: 90s leaves room for an
// RPA agent (Yandex.Business is the slow path) to chase a CAPTCHA / 2FA
// challenge before the request is abandoned.
const retryDispatchTimeout = 90 * time.Second

// taskStatusError is the AgentTask status written for a failed tool dispatch —
// the same literal the chat-turn postal fanout stamps. Only tasks in this
// status may be retried.
const taskStatusError = "error"

// RetryReason enumerates why a retry was accepted or refused. It is the
// machine-readable classifier the handler maps to an HTTP status and the FE
// resolves to localized copy — the caller never parses free text.
type RetryReason string

const (
	// RetryReasonReconnect: the original failure was a rejected token/session.
	// A blind re-dispatch would fail identically; the integration must be
	// reconnected first, so the retry is refused with this signal.
	RetryReasonReconnect RetryReason = "reconnect_required"
	// RetryReasonPermanent: the original failure is permanent (oversized media,
	// malformed input). It cannot succeed on re-dispatch and is refused.
	RetryReasonPermanent RetryReason = "not_retryable"
)

// retryableErrorCodes is the set of platform failure classifiers a retry may
// safely re-dispatch: transient/network failures, rate limits (retry after
// backoff), and a channel/community that was not found at dispatch time (the
// owner may have recreated or reconfigured it since). The enum is locked in
// pkg/a2a: integration_token_invalid and media_too_large are deliberately
// absent — the first needs a reconnect, the second is permanent.
var retryableErrorCodes = map[string]struct{}{
	"transient":           {},
	"rate_limit_exceeded": {},
	"channel_not_found":   {},
}

// RetryRejectedError reports that a retry was refused because the failure is not
// retryable. It wraps domain.ErrAgentTaskNotRetryable so callers can match the
// class with errors.Is, while Reason carries the typed classifier the handler
// maps to an HTTP body (reconnect vs permanent) without parsing free text.
type RetryRejectedError struct {
	Reason RetryReason
}

func (e *RetryRejectedError) Error() string {
	return fmt.Sprintf("%s: %s", domain.ErrAgentTaskNotRetryable, e.Reason)
}

func (e *RetryRejectedError) Unwrap() error { return domain.ErrAgentTaskNotRetryable }

// newRetryRejection maps a non-retryable failure classifier to the typed
// rejection surfaced to the caller. integration_token_invalid routes to a
// reconnect signal (not a blind retry); every other non-retryable code is
// permanent.
func newRetryRejection(errorCode string) *RetryRejectedError {
	if errorCode == "integration_token_invalid" {
		return &RetryRejectedError{Reason: RetryReasonReconnect}
	}
	return &RetryRejectedError{Reason: RetryReasonPermanent}
}

// AgentTaskService defines the interface for agent task operations
type AgentTaskService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
	// Retry re-dispatches a RETRYABLE failed task to its platform agent without
	// the LLM, reusing the same HITL dedupe path so a call that actually landed
	// is not executed twice. It returns the task with its refreshed outcome.
	Retry(ctx context.Context, businessID uuid.UUID, taskID string) (*domain.AgentTask, error)
}

type agentTaskService struct {
	repo            domain.AgentTaskRepository
	businessService BusinessService
	nc              natsRequester // nil = no platform dispatch (Mongo-only mode)
	dispatchTimeout time.Duration
}

// Compile-time check that agentTaskService implements AgentTaskService
var _ AgentTaskService = (*agentTaskService)(nil)

// NewAgentTaskService creates a new agent task service instance. nc may be nil —
// in that mode Retry rejects with an error because a retry cannot reach the
// platform agents. businessService gates soft-deleted organizations out of the
// retry path (nil disables that gate for in-process callers).
func NewAgentTaskService(repo domain.AgentTaskRepository, businessService BusinessService, nc *natslib.Conn) AgentTaskService {
	var requester natsRequester
	if nc != nil {
		requester = nc
	}
	return &agentTaskService{
		repo:            repo,
		businessService: businessService,
		nc:              requester,
		dispatchTimeout: retryDispatchTimeout,
	}
}

func (s *agentTaskService) List(ctx context.Context, businessID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error) {
	tasks, total, err := s.repo.ListByBusinessID(ctx, businessID.String(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list agent tasks: %w", err)
	}

	return tasks, total, nil
}

// Retry rebuilds the ToolRequest from a stored failed task and re-dispatches it
// to the platform agent over NATS. The ownership check is the (business_id,
// task_id) repository key; a cross-business or unknown id resolves to
// domain.ErrAgentTaskNotFound. A soft-deleted organization is rejected before
// any platform work. Only a task in the "error" status whose failure classifier
// is retryable re-dispatches; every other case returns a typed error. The
// refreshed task (with its new outcome persisted) is returned.
func (s *agentTaskService) Retry(ctx context.Context, businessID uuid.UUID, taskID string) (*domain.AgentTask, error) {
	if err := s.gateBusiness(ctx, businessID); err != nil {
		return nil, err
	}

	task, err := s.repo.GetByID(ctx, businessID.String(), taskID)
	if err != nil {
		return nil, fmt.Errorf("get agent task: %w", err)
	}

	if task.Status != taskStatusError {
		return nil, domain.ErrAgentTaskNotFailed
	}
	if _, ok := retryableErrorCodes[task.ErrorCode]; !ok {
		return nil, newRetryRejection(task.ErrorCode)
	}

	if s.nc == nil {
		return nil, fmt.Errorf("task retry is not configured")
	}

	toolName, args, err := rebuildToolRequest(task)
	if err != nil {
		return nil, err
	}

	resp, dispatchErr := dispatchToolWithApproval(ctx, s.nc, task.Platform, toolName, args,
		task.BusinessID, retryApprovalID(task), s.dispatchTimeout)

	return s.persistRetryOutcome(ctx, task, resp.Result, dispatchErr)
}

// gateBusiness re-loads the target business through the soft-delete-aware
// GetByID (deleted_at IS NULL) and rejects with domain.ErrBusinessNotFound when
// the organization is soft-deleted (pending erasure). The retry path funnels
// through it before touching an external platform, so a business inside its
// deletion grace window — whose membership row still satisfies
// authz.RequireBusinessAccess — cannot drive fresh platform work. It is a no-op
// when no business service is attached (in-process callers) or when businessID
// is the nil UUID. Mirrors reviewService.gateBusiness.
func (s *agentTaskService) gateBusiness(ctx context.Context, businessID uuid.UUID) error {
	if s.businessService == nil || businessID == uuid.Nil {
		return nil
	}
	if _, err := s.businessService.GetByID(ctx, businessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return domain.ErrBusinessNotFound
		}
		return fmt.Errorf("load task business: %w", err)
	}
	return nil
}

// retryApprovalID derives the HITL dedupe key for a task retry. It reuses the
// ORIGINAL approved dispatch's ApprovalID ("<batch_id>-<call_id>") persisted on
// the task at first-dispatch time, so a retry of a call that ALREADY LANDED
// re-sends the identical key and the agent's (business_id, approval_id) Redis
// dedupe gate (pkg/hitldedupe) returns the cached result instead of repeating
// an irreversible side effect (a public post/reply) — closing the
// retry-vs-original double-post window. Legacy rows that predate the persisted
// field fall back to the stable per-task key, which stays retry-vs-retry safe.
func retryApprovalID(task *domain.AgentTask) string {
	if task.DispatchApprovalID != "" {
		return task.DispatchApprovalID
	}
	return "task-retry-" + task.ID
}

// rebuildToolRequest reconstructs the platform tool name and argument map from a
// stored task. Type is the tool's action suffix and Platform its prefix (the
// onToolCall record splits "{platform}__{action}" into these two fields), so the
// original "{platform}__{action}" tool name is Platform + "__" + Type. Input is
// the argument map the tool was originally dispatched with. Returns an error
// when the stored Input is not a decodable argument map — a malformed task is
// refused rather than dispatched with empty args.
func rebuildToolRequest(task *domain.AgentTask) (toolName string, args map[string]interface{}, err error) {
	if task.Platform == "" || task.Type == "" {
		return "", nil, fmt.Errorf("task %q: missing platform/type, cannot rebuild tool request", task.ID)
	}
	args, ok := coerceArgs(task.Input)
	if !ok {
		return "", nil, fmt.Errorf("task %q: input is not a tool-argument map", task.ID)
	}
	return task.Platform + "__" + task.Type, args, nil
}

// coerceArgs normalizes a stored task Input into a tool-argument map. A task
// created in-process carries a map[string]interface{}; a task round-tripped
// through BSON decodes into the same shape, so both are accepted. A nil Input
// (no arguments) is a valid empty map. Any other shape is rejected.
func coerceArgs(input interface{}) (map[string]interface{}, bool) {
	if input == nil {
		return map[string]interface{}{}, true
	}
	if m, ok := input.(map[string]interface{}); ok {
		return m, true
	}
	return nil, false
}

// persistRetryOutcome writes the re-dispatch result back onto the task so the
// tasks list reflects the retry: success flips it to "done" with the fresh
// output and clears the prior error; a failure keeps it in "error" with the new
// message + classifier. CompletedAt is re-stamped either way. The refreshed task
// is reloaded and returned so the handler renders the updated row.
func (s *agentTaskService) persistRetryOutcome(ctx context.Context, task *domain.AgentTask, resp interface{}, dispatchErr error) (*domain.AgentTask, error) {
	now := time.Now()
	update := &domain.AgentTask{
		ID:          task.ID,
		BusinessID:  task.BusinessID,
		Status:      "done",
		CompletedAt: &now,
	}
	if dispatchErr != nil {
		update.Status = taskStatusError
		update.Error = dispatchErr.Error()
		update.ErrorCode = a2a.CodeOf(dispatchErr)
	} else if result, ok := resp.(map[string]interface{}); ok {
		update.Output = result
	}

	if err := s.repo.Update(ctx, update); err != nil {
		return nil, fmt.Errorf("persist retry outcome: %w", err)
	}

	fresh, err := s.repo.GetByID(ctx, task.BusinessID, task.ID)
	if err != nil {
		slog.WarnContext(ctx, "task retry: outcome persisted but reload failed",
			"task_id", task.ID, "error", err)
		return update, nil
	}
	return fresh, nil
}
