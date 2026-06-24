package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// dispatchTool sends a single A2A tool request to the platform agent over NATS
// and returns the decoded response. Preserves the historical error wording so
// callers can wrap it unchanged. The caller reads resp.Result on success.
func dispatchTool(ctx context.Context, nc natsRequester, platform, tool string, args map[string]interface{}, businessID string, timeout time.Duration) (a2a.ToolResponse, error) {
	return dispatchToolWithApproval(ctx, nc, platform, tool, args, businessID, "", timeout)
}

// dispatchToolWithApproval is dispatchTool with an explicit ApprovalID. A
// non-empty approvalID makes the request idempotent at the agent: the agent's
// dedupe gate keys on (business_id, approval_id), so a retry returns the cached
// result instead of repeating an irreversible side effect (e.g. a public
// review reply). Pass "" for the auto/sync paths where every dispatch is a
// distinct call.
func dispatchToolWithApproval(ctx context.Context, nc natsRequester, platform, tool string, args map[string]interface{}, businessID, approvalID string, timeout time.Duration) (a2a.ToolResponse, error) {
	req := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       tool,
		Args:       args,
		BusinessID: businessID,
		ApprovalID: approvalID,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return a2a.ToolResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msg, err := nc.RequestMsgWithContext(reqCtx, &natslib.Msg{
		Subject: a2a.Subject(platform),
		Data:    data,
	})
	if err != nil {
		return a2a.ToolResponse{}, fmt.Errorf("nats request to %s: %w", a2a.Subject(platform), err)
	}

	var resp a2a.ToolResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return a2a.ToolResponse{}, fmt.Errorf("decode agent response: %w", err)
	}
	if !resp.Success {
		return a2a.ToolResponse{}, fmt.Errorf("agent error: %s", resp.Error)
	}
	return resp, nil
}
