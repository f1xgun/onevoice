package natsexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// Requester abstracts NATS request-reply for testability.
// The real implementation wraps *nats.Conn.
type Requester interface {
	Request(ctx context.Context, subject string, data []byte) ([]byte, error)
}

// NATSExecutor implements tools.Executor by sending tool requests
// to a platform agent via NATS request-reply.
type NATSExecutor struct {
	agentID  a2a.AgentID
	toolName string
	req      Requester
}

// New creates a NATSExecutor for the given agent and specific tool name.
func New(agentID a2a.AgentID, toolName string, requester Requester) *NATSExecutor {
	return &NATSExecutor{agentID: agentID, toolName: toolName, req: requester}
}

// Execute sends a ToolRequest to the agent and returns its result. It
// implements tools.Executor. Delegates to ExecuteWithApproval with an
// empty approvalID — legacy behavior for auto-floor tools that never
// pass through HITL approval (backward-compat shim).
func (e *NATSExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return e.ExecuteWithApproval(ctx, args, "")
}

// ExecuteWithApproval sends a ToolRequest with the given approvalID
// propagated on the ToolRequest.ApprovalID field. When approvalID is empty
// the behavior is byte-identical to the legacy Execute path — the omitempty
// JSON tag on ApprovalID means the field is not emitted on the wire, so
// agents that have not yet been upgraded decode the payload cleanly.
//
// Agents use approvalID + business_id as the Redis dedupe key
// (pkg/hitldedupe) so a retry of an already-dispatched call returns the
// cached response instead of executing twice. The resume path
// generates approvalID as "<batch_id>-<call_id>" so each approved call
// in a batch has a unique key.
func (e *NATSExecutor) ExecuteWithApproval(ctx context.Context, args map[string]interface{}, approvalID string) (interface{}, error) {
	req := a2a.ToolRequest{
		TaskID:     uuid.New().String(),
		Tool:       e.toolName,
		Args:       args,
		BusinessID: a2a.BusinessIDFromContext(ctx),
		RequestID:  logger.CorrelationIDFromContext(ctx),
		ApprovalID: approvalID,
	}
	return e.dispatch(ctx, req)
}

// dispatch serializes a fully-populated ToolRequest, sends it over NATS,
// decodes the response, and returns the result. Factored out so Execute
// and ExecuteWithApproval share the exact same transport logic — any
// future retry / tracing / metric change lands here once.
func (e *NATSExecutor) dispatch(ctx context.Context, req a2a.ToolRequest) (interface{}, error) {
	if hit, key := violatesDenyList(req.Args); hit {
		metrics.IncNATSPublishRejected("denylist_key")
		slog.WarnContext(ctx, "natsexec: deny-list key in tool args — refused publish",
			"tool", e.toolName,
			"agent", e.agentID,
			"denied_key", key,
		)
		return nil, ErrDenyListed
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("natsexec: marshal request: %w", err)
	}

	subject := a2a.Subject(e.agentID)

	start := time.Now()
	replyData, err := e.req.Request(ctx, subject, data)
	elapsed := time.Since(start)

	result := "ok"
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result = "timeout"
		} else {
			result = "error"
		}
	}
	metrics.RecordNATSPublish(subject, result, elapsed)

	if err != nil {
		slog.ErrorContext(ctx, "natsexec: tool request failed",
			"tool", e.toolName,
			"agent", e.agentID,
			"business_id", req.BusinessID,
			"duration_ms", elapsed.Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("natsexec: request to %s: %w", subject, err)
	}

	var resp a2a.ToolResponse
	if err := json.Unmarshal(replyData, &resp); err != nil {
		slog.ErrorContext(ctx, "natsexec: decode response failed",
			"tool", e.toolName,
			"agent", e.agentID,
			"business_id", req.BusinessID,
			"duration_ms", elapsed.Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("natsexec: decode response: %w", err)
	}

	if !resp.Success {
		slog.WarnContext(ctx, "natsexec: tool returned error",
			"tool", e.toolName,
			"agent", e.agentID,
			"business_id", req.BusinessID,
			"duration_ms", elapsed.Milliseconds(),
			"agent_error", resp.Error,
			"agent_code", resp.Code,
		)
		wrapped := fmt.Errorf("natsexec: agent %s error: %s", e.agentID, resp.Error)
		if resp.Code != "" {
			return nil, a2a.NewCodedError(resp.Code, wrapped)
		}
		return nil, wrapped
	}

	slog.InfoContext(ctx, "natsexec: tool request completed",
		"tool", e.toolName,
		"agent", e.agentID,
		"business_id", req.BusinessID,
		"duration_ms", elapsed.Milliseconds(),
	)

	return resp.Result, nil
}
