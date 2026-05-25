package email

import (
	"context"
	"errors"
)

// Message is the unit of work a Sender consumes. Fields map 1:1 to the
// email_outbox table columns so the worker can construct it from a
// database row without translation.
type Message struct {
	To       string // single recipient — multi-recipient is YAGNI for Phase 21
	Subject  string
	BodyText string
	BodyHTML string // optional; "" means text-only
}

// Sender abstracts the transactional-email transport. Implementations
// must be safe for concurrent use by the outbox worker (which may
// pipeline multiple Sends if a future change adds parallel drain).
type Sender interface {
	// Send delivers msg synchronously. Returns the provider-side
	// job/message id on success (stored on the outbox row for support
	// lookups). On ErrTransient the worker reschedules with
	// exponential backoff; on ErrPermanent the worker marks the row
	// status='failed' immediately without further retry.
	Send(ctx context.Context, msg Message) (providerJobID string, err error)
}

// ErrTransient marks failures the outbox worker should retry
// (network errors, 5xx HTTP, 429 rate limit, Unisender "service
// unavailable" responses). Implementations wrap the upstream error:
//
//	return "", fmt.Errorf("unisender 502: %w", email.ErrTransient)
var ErrTransient = errors.New("email: transient send failure (retry)")

// ErrPermanent marks failures the worker should NOT retry (invalid
// recipient, hard bounce, malformed-message HTTP 400). Wrapping is the
// same shape as ErrTransient.
var ErrPermanent = errors.New("email: permanent send failure (do not retry)")
