package a2a

import (
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// NATSTransport adapts *nats.Conn to the Transport interface.
type NATSTransport struct {
	nc *natslib.Conn
}

// NewNATSTransport wraps an existing *nats.Conn.
func NewNATSTransport(nc *natslib.Conn) *NATSTransport {
	return &NATSTransport{nc: nc}
}

// Subscribe registers a message handler on the given NATS subject. It blocks
// until the server has acknowledged the subscription (via Flush), so callers
// that publish to the same subject immediately after Subscribe returns do
// not race the server-side SUB registration. Subscribe is invoked once per
// agent at startup — the extra round-trip is negligible in production and
// eliminates a class of "no responders available" CI flakes.
//
// The user handler is wrapped so handler execution time is observed via
// metrics.RecordNATSHandler with result="ok". Handler errors surface via
// the ToolResponse reply payload (see tool_dispatch_total) and are not
// distinguishable at the transport layer.
func (t *NATSTransport) Subscribe(subject string, handler func(subject, reply string, data []byte)) error {
	_, err := t.nc.Subscribe(subject, func(msg *natslib.Msg) {
		start := time.Now()
		handler(msg.Subject, msg.Reply, msg.Data)
		metrics.RecordNATSHandler(msg.Subject, "ok", time.Since(start))
	})
	if err != nil {
		return err
	}
	return t.nc.Flush()
}

// Publish sends data to a NATS subject (used for replying to requests).
// Publish duration and result are recorded via metrics.RecordNATSPublish.
func (t *NATSTransport) Publish(subject string, data []byte) error {
	start := time.Now()
	err := t.nc.Publish(subject, data)
	result := "ok"
	if err != nil {
		result = "error"
	}
	metrics.RecordNATSPublish(subject, result, time.Since(start))
	return err
}

// Close initiates graceful shutdown by draining the NATS connection.
// Drain is asynchronous and errors are intentionally ignored per the Transport interface contract.
// Callers should allow time for in-flight messages to complete before process exit.
func (t *NATSTransport) Close() {
	_ = t.nc.Drain()
}
