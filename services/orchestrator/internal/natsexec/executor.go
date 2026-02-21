package natsexec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// Requester abstracts NATS request-reply for testability.
// The real implementation wraps *nats.Conn.
type Requester interface {
	Request(ctx context.Context, subject string, data []byte) ([]byte, error)
}

// NATSExecutor implements tools.Executor by sending tool requests
// to a platform agent via NATS request-reply.
type NATSExecutor struct {
	agentID a2a.AgentID
	req     Requester
}

// New creates a NATSExecutor for the given agent.
func New(agentID a2a.AgentID, requester Requester) *NATSExecutor {
	return &NATSExecutor{agentID: agentID, req: requester}
}

// Execute sends a ToolRequest to the agent and returns its result.
// It implements tools.Executor.
func (e *NATSExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := a2a.ToolRequest{
		TaskID: uuid.New().String(),
		Tool:   e.agentID,
		Args:   args,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("natsexec: marshal request: %w", err)
	}

	subject := a2a.Subject(e.agentID)
	replyData, err := e.req.Request(ctx, subject, data)
	if err != nil {
		return nil, fmt.Errorf("natsexec: request to %s: %w", subject, err)
	}

	var resp a2a.ToolResponse
	if err := json.Unmarshal(replyData, &resp); err != nil {
		return nil, fmt.Errorf("natsexec: decode response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("natsexec: agent %s error: %s", e.agentID, resp.Error)
	}

	return resp.Result, nil
}
