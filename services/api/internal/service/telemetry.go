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
	repo telemetryRepo
}

// NewTelemetryService constructs a TelemetryService.
func NewTelemetryService(repo telemetryRepo) *TelemetryService {
	return &TelemetryService{repo: repo}
}

// Ingest persists a batch of telemetry events, stamping the authenticated
// user_id on every row server-side (never trusting a client-supplied id).
// business_id is left NULL — the telemetry route carries no BusinessContext;
// server-emitted value events attribute the business directly. A uuid.Nil
// userID is stored as NULL. No-op on an empty batch.
func (s *TelemetryService) Ingest(ctx context.Context, userID uuid.UUID, events []TelemetryEvent) error {
	if len(events) == 0 {
		return nil
	}
	var userPtr *uuid.UUID
	if userID != uuid.Nil {
		userPtr = &userID
	}
	rows := make([]repository.TelemetryEventRow, 0, len(events))
	for _, e := range events {
		// Skip malformed events so empty event_type/action don't pollute the
		// funnel store (the openapi type marks both required, but a bare
		// decode does not enforce it).
		if e.EventType == "" || e.Action == "" {
			continue
		}
		row := repository.TelemetryEventRow{
			UserID:    userPtr,
			EventType: e.EventType,
			Action:    e.Action,
			Page:      e.Page,
			Metadata:  marshalTelemetryMetadata(e.Metadata),
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
