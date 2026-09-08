package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

// RecordApproval persists a closed server event without user identifiers or payloads.
// Failures are best-effort and never change approval/dispatch outcomes.
func (s *TelemetryService) RecordApproval(ctx context.Context, events ...approvaltelemetry.Event) {
	rows := make([]repository.TelemetryEventRow, 0, len(events))
	for _, event := range events {
		row, ok := approvalTelemetryRow(event)
		if ok {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return
	}
	select {
	case s.approvalWrites <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-s.approvalWrites }()
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := s.repo.InsertBatch(saveCtx, rows); err != nil {
			slog.WarnContext(saveCtx, "approval telemetry write failed")
		}
	}()
}

func approvalTelemetryRow(event approvaltelemetry.Event) (repository.TelemetryEventRow, bool) {
	if event.DraftID == "" || event.ApprovalID == "" || !approvaltelemetry.ValidKind(event.Kind) || !approvaltelemetry.ValidSource(event.Source) {
		return repository.TelemetryEventRow{}, false
	}
	switch event.Action {
	case "edit_saved":
		if event.Outcome != "" {
			return repository.TelemetryEventRow{}, false
		}
	case "decision_recorded":
		if event.Outcome != "approve" && event.Outcome != "edit" && event.Outcome != "reject" {
			return repository.TelemetryEventRow{}, false
		}
	case "send_result":
		if event.Outcome != "success" && event.Outcome != "error" && event.Outcome != "not_dispatched" {
			return repository.TelemetryEventRow{}, false
		}
	default:
		return repository.TelemetryEventRow{}, false
	}
	id := approvaltelemetry.ID(event.DraftID)
	metadata := map[string]string{
		"draft_id": id, "approval_id": approvaltelemetry.ID(event.ApprovalID),
		"kind": event.Kind, "source": event.Source,
	}
	if event.Outcome != "" {
		metadata["outcome"] = event.Outcome
	}
	if event.BatchID != "" {
		metadata["batch_id"] = approvaltelemetry.ID(event.BatchID)
	}
	return repository.TelemetryEventRow{
		EventType: "approval_server", Action: event.Action, Page: "/" + event.Source,
		CorrelationID: &id, Metadata: marshalTelemetryMetadata(metadata),
	}, true
}
