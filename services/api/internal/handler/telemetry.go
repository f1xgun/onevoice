package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

const maxTelemetryBatchSize = 100

// TelemetryHandler handles frontend telemetry ingestion.
type TelemetryHandler struct{}

// NewTelemetryHandler creates a new TelemetryHandler.
func NewTelemetryHandler() *TelemetryHandler {
	return &TelemetryHandler{}
}

// Ingest accepts an array of frontend telemetry events and logs each as structured JSON.
// The request context carries a correlation_id from CorrelationID middleware,
// which is automatically included by the ContextHandler.
func (h *TelemetryHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	var events []openapi.TelemetryEvent
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if len(events) > maxTelemetryBatchSize {
		http.Error(w, `{"error":"batch size exceeds limit of 100"}`, http.StatusBadRequest)
		return
	}

	for _, e := range events {
		slog.InfoContext(r.Context(), "frontend_event",
			"event_type", e.EventType,
			"page", e.Page,
			"action", e.Action,
			"frontend_correlation_id", strDeref(e.CorrelationId),
			"metadata", e.Metadata,
			"client_ts", e.Timestamp,
		)
	}

	w.WriteHeader(http.StatusNoContent)
}
