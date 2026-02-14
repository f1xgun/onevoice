package a2a

import (
	natslib "github.com/nats-io/nats.go"
)

// NATSTransport adapts *nats.Conn to the Transport interface.
type NATSTransport struct {
	nc *natslib.Conn
}

// NewNATSTransport wraps an existing *nats.Conn.
func NewNATSTransport(nc *natslib.Conn) *NATSTransport {
	return &NATSTransport{nc: nc}
}

// Subscribe registers a message handler on the given NATS subject.
func (t *NATSTransport) Subscribe(subject string, handler func(subject, reply string, data []byte)) error {
	_, err := t.nc.Subscribe(subject, func(msg *natslib.Msg) {
		handler(msg.Subject, msg.Reply, msg.Data)
	})
	return err
}

// Publish sends data to a NATS subject (used for replying to requests).
func (t *NATSTransport) Publish(subject string, data []byte) error {
	return t.nc.Publish(subject, data)
}

// Close initiates graceful shutdown by draining the NATS connection.
// Drain is asynchronous and errors are intentionally ignored per the Transport interface contract.
// Callers should allow time for in-flight messages to complete before process exit.
func (t *NATSTransport) Close() {
	_ = t.nc.Drain()
}
