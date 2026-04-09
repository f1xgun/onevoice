package natsexec

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
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

// Execute sends a ToolRequest to the agent and returns its result.
// It implements tools.Executor.
func (e *NATSExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := a2a.ToolRequest{
		TaskID:     uuid.New().String(),
		Tool:       e.toolName,
		Args:       args,
		BusinessID: a2a.BusinessIDFromContext(ctx),
		RequestID:  logger.CorrelationIDFromContext(ctx),
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("natsexec: marshal request: %w", err)
	}

	subject := a2a.Subject(e.agentID)

	start := time.Now()
	replyData, err := e.req.Request(ctx, subject, data)
	elapsed := time.Since(start)

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
		)
		return nil, fmt.Errorf("natsexec: agent %s error: %s", e.agentID, resp.Error)
	}

	slog.InfoContext(ctx, "natsexec: tool request completed",
		"tool", e.toolName,
		"agent", e.agentID,
		"business_id", req.BusinessID,
		"duration_ms", elapsed.Milliseconds(),
	)

	return resp.Result, nil
}
