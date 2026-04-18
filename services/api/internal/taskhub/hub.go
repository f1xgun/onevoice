// Package taskhub implements an in-process pub/sub hub for agent task events.
// Subscribers are scoped per business; publishers emit created/updated events
// that the SSE handler forwards to connected clients.
package taskhub

import (
	"log/slog"
	"sync"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Event kinds.
const (
	KindCreated = "task.created"
	KindUpdated = "task.updated"
)

// Event is a single task lifecycle notification.
type Event struct {
	Kind string           `json:"kind"`
	Task domain.AgentTask `json:"task"`
}

// subscriberBuffer is the per-subscriber channel capacity. Sized so a transient
// burst (e.g. rapid tool_call/tool_result pairs) does not block publishers.
const subscriberBuffer = 64

// Hub multiplexes events from publishers to per-business subscribers.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[*subscription]struct{}
}

type subscription struct {
	ch chan Event
}

// New creates an empty Hub.
func New() *Hub {
	return &Hub{subs: make(map[string]map[*subscription]struct{})}
}

// Subscribe registers a new subscriber for the given businessID.
// The caller reads from the returned channel until unsub is called.
// After unsub, the channel is closed and must not be read from again.
func (h *Hub) Subscribe(businessID string) (events <-chan Event, unsub func()) {
	sub := &subscription{ch: make(chan Event, subscriberBuffer)}

	h.mu.Lock()
	if _, ok := h.subs[businessID]; !ok {
		h.subs[businessID] = make(map[*subscription]struct{})
	}
	h.subs[businessID][sub] = struct{}{}
	h.mu.Unlock()

	events = sub.ch
	unsub = func() {
		h.mu.Lock()
		if set, ok := h.subs[businessID]; ok {
			if _, exists := set[sub]; exists {
				delete(set, sub)
				close(sub.ch)
				if len(set) == 0 {
					delete(h.subs, businessID)
				}
			}
		}
		h.mu.Unlock()
	}
	return events, unsub
}

// Publish sends an event to all subscribers of businessID. The call never
// blocks — if a subscriber's buffer is full, the event is dropped for that
// subscriber and a warning is logged. Slow clients must reconcile via REST.
func (h *Hub) Publish(businessID string, ev Event) {
	h.mu.RLock()
	set := h.subs[businessID]
	targets := make([]*subscription, 0, len(set))
	for s := range set {
		targets = append(targets, s)
	}
	h.mu.RUnlock()

	for _, s := range targets {
		select {
		case s.ch <- ev:
		default:
			slog.Warn("taskhub: dropped event, subscriber channel full",
				"business_id", businessID,
				"kind", ev.Kind,
				"task_id", ev.Task.ID,
			)
		}
	}
}
