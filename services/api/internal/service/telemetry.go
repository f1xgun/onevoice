// Package service — telemetry.go.
//
// TelemetryService persists frontend product-analytics events, stamping the
// authenticated user server-side so the data is attributable for funnel /
// activation / retention analysis. Decoupled from the HTTP/openapi wire shape.
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

// TelemetryEvent is the service-layer view of one frontend telemetry event.
type TelemetryEvent struct {
	EventType     string
	Action        string
	Page          string
	Metadata      map[string]string
	CorrelationID string
	ClientTS      string
}

// telemetryRepo is the narrow persistence surface TelemetryService depends on.
type telemetryRepo interface {
	InsertBatch(ctx context.Context, rows []repository.TelemetryEventRow) error
}

// TelemetryService persists telemetry batches.
type TelemetryService struct {
	repo           telemetryRepo
	approvalWrites chan struct{}
}

// NewTelemetryService constructs a TelemetryService.
func NewTelemetryService(repo telemetryRepo) *TelemetryService {
	return &TelemetryService{repo: repo, approvalWrites: make(chan struct{}, 8)}
}

// validClientTelemetryEventType is the closed client-ingress taxonomy. Actions
// intentionally remain open: page_view uses the current pathname, api_error
// includes request coordinates, and button/activation actions evolve with the
// UI. Server-only event types must use their dedicated service emitters.
func validClientTelemetryEventType(eventType string) bool {
	switch eventType {
	case "page_view", "api_error", "chat_send", "button_click", "activation", "approval":
		return true
	default:
		return false
	}
}

// Ingest persists a batch of telemetry events, stamping the authenticated
// user_id on every row server-side (never trusting a client-supplied id).
// business_id is left NULL — the telemetry route carries no BusinessContext;
// server-emitted value events attribute the business directly. A uuid.Nil
// userID is stored as NULL. No-op on an empty batch.
func (s *TelemetryService) Ingest(ctx context.Context, userID uuid.UUID, events []TelemetryEvent) error {
	return s.ingest(ctx, userID, nil, events)
}

// IngestForBusiness persists client telemetry attributed to a business whose
// membership was authorized at the HTTP boundary.
func (s *TelemetryService) IngestForBusiness(ctx context.Context, userID, businessID uuid.UUID, events []TelemetryEvent) error {
	return s.ingest(ctx, userID, &businessID, events)
}

func (s *TelemetryService) ingest(ctx context.Context, userID uuid.UUID, businessID *uuid.UUID, events []TelemetryEvent) error {
	if len(events) == 0 {
		return nil
	}
	var userPtr *uuid.UUID
	if userID != uuid.Nil {
		userPtr = &userID
	}
	rows := make([]repository.TelemetryEventRow, 0, len(events))
	for _, e := range events {
		if !validClientTelemetryEventType(e.EventType) {
			continue
		}
		if e.EventType == "approval" {
			metadata, valid := approvaltelemetry.ClientMetadata(e.Action, e.Metadata)
			if !valid {
				continue
			}
			id := metadata["draft_id"]
			rows = append(rows, repository.TelemetryEventRow{
				EventType: e.EventType, Action: e.Action, Page: "/" + metadata["source"],
				Metadata: marshalTelemetryMetadata(metadata), CorrelationID: &id,
			})
			continue
		}
		// Skip malformed events so empty event_type/action don't pollute the
		// funnel store (the openapi type marks both required, but a bare
		// decode does not enforce it).
		if e.EventType == "" || e.Action == "" {
			continue
		}
		row := repository.TelemetryEventRow{
			UserID:     userPtr,
			BusinessID: businessID,
			EventType:  e.EventType,
			Action:     e.Action,
			Page:       e.Page,
			Metadata:   marshalTelemetryMetadata(e.Metadata),
		}
		if e.CorrelationID != "" {
			cid := e.CorrelationID
			row.CorrelationID = &cid
		}
		if e.ClientTS != "" {
			ts := e.ClientTS
			row.ClientTS = &ts
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	if err := s.repo.InsertBatch(ctx, rows); err != nil {
		return fmt.Errorf("telemetry ingest: %w", err)
	}
	return nil
}

// marshalTelemetryMetadata returns nil for an empty map (the repository stores
// it as the empty JSON object) and swallows a marshal error to a nil payload —
// telemetry is best-effort and must never fail the request on a bad map.
func marshalTelemetryMetadata(m map[string]string) []byte {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}
