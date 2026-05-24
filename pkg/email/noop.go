package email

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// NoopSender is the test/dev Sender. It records every Send call in
// memory (Messages accessor for assertions) and returns a fake
// provider job id "noop-N" where N is a monotonic counter.
//
// Safe for concurrent use.
type NoopSender struct {
	mu       sync.Mutex
	messages []Message
	counter  atomic.Int64
}

// Compile-time interface check.
var _ Sender = (*NoopSender)(nil)

// NewNoopSender returns a fresh NoopSender. The internal buffer starts
// empty; callers wanting to reset between scenarios should call Reset.
func NewNoopSender() *NoopSender {
	return &NoopSender{}
}

// Send records msg in the internal buffer and returns a synthetic job
// id of the form "noop-N". Never errors. ctx is ignored — NoopSender
// has no I/O to cancel.
func (n *NoopSender) Send(_ context.Context, msg Message) (string, error) {
	n.mu.Lock()
	n.messages = append(n.messages, msg)
	n.mu.Unlock()
	id := n.counter.Add(1)
	return fmt.Sprintf("noop-%d", id), nil
}

// Messages returns a defensive copy of all recorded messages.
func (n *NoopSender) Messages() []Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Message, len(n.messages))
	copy(out, n.messages)
	return out
}

// Reset clears the recorded message buffer and the job-id counter.
// Useful in tests between scenarios.
func (n *NoopSender) Reset() {
	n.mu.Lock()
	n.messages = nil
	n.mu.Unlock()
	n.counter.Store(0)
}
