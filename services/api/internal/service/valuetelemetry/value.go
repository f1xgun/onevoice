// Package valuetelemetry defines the narrow, server-only product value-event sink.
package valuetelemetry

import (
	"context"

	"github.com/google/uuid"
)

const (
	SignupCompleted      = "signup_completed"
	EmailVerified        = "email_verified"
	OrgCreated           = "org_created"
	IntegrationConnected = "integration_connected"
	PostPublished        = "post_published"
	ReviewReplied        = "review_replied"
)

// Event contains only trusted identifiers and the bounded metadata fields
// allowed for canonical value events. SourceID is used only to derive the
// durable deduplication key and is never stored in metadata.
type Event struct {
	Action     string
	SourceID   string
	UserID     uuid.UUID
	BusinessID uuid.UUID
	Platform   string
	Kind       string
}

// Sink accepts a completed server-side value event. Implementations are
// best-effort: callers must never depend on telemetry delivery for success.
type Sink interface {
	RecordValue(context.Context, Event)
}
