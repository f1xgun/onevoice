package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// NATSTaskPublisher dispatches A2A ToolRequests over NATS request/reply.
// Implements TaskPublisher.
type NATSTaskPublisher struct {
	nc *natslib.Conn
}

// NewNATSTaskPublisher wraps a *nats.Conn so the platform Syncer can publish
// agent tasks without depending on the nats package directly.
func NewNATSTaskPublisher(nc *natslib.Conn) *NATSTaskPublisher {
	return &NATSTaskPublisher{nc: nc}
}

// RequestTool serializes req, performs a NATS request on subject with the
// given timeout, and decodes the reply into an a2a.ToolResponse. The caller's
// ctx is honored: if it is canceled before the NATS reply arrives, the wait
// is aborted with ctx.Err().
func (p *NATSTaskPublisher) RequestTool(ctx context.Context, subject string, req a2a.ToolRequest, timeout time.Duration) (*a2a.ToolResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msg, err := p.nc.RequestWithContext(reqCtx, subject, payload)
	if err != nil {
		return nil, fmt.Errorf("nats request: %w", err)
	}

	var resp a2a.ToolResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}
