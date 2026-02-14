package natsexec

import (
	"context"

	natslib "github.com/nats-io/nats.go"
)

// NATSConn wraps *nats.Conn to implement Requester.
type NATSConn struct {
	nc *natslib.Conn
}

// NewNATSConn wraps an existing *nats.Conn.
func NewNATSConn(nc *natslib.Conn) *NATSConn {
	return &NATSConn{nc: nc}
}

// Request sends data to subject and waits for a reply (uses context for timeout).
func (c *NATSConn) Request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	msg, err := c.nc.RequestMsgWithContext(ctx, &natslib.Msg{
		Subject: subject,
		Data:    data,
	})
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}
