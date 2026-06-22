package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

const maxTelemetryBatchSize = 100

// maxTelemetryPayloadBytes caps the request body. A 100-event batch of small
// events stays well under this; the cap defends the analytics store against
// flooding.
const maxTelemetryPayloadBytes = 256 * 1024

// telemetryIngester is the narrow service surface the handler depends on.
type telemetryIngester interface {
	Ingest(ctx context.Context, userID uuid.UUID, events []service.TelemetryEvent) error
}

// TelemetryHandler handles frontend telemetry ingestion.
type TelemetryHandler struct {
	svc telemetryIngester
}

// NewTelemetryHandler creates a new TelemetryHandler.
func NewTelemetryHandler(svc telemetryIngester) *TelemetryHandler {
	return &TelemetryHandler{svc: svc}
}

// Ingest accepts an array of frontend telemetry events and persists them with
// the authenticated user_id stamped server-side. The request context carries a
// correlation_id from CorrelationID middleware.
func (h *TelemetryHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTelemetryPayloadBytes)

	var events []openapi.TelemetryEvent
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if len(events) > maxTelemetryBatchSize {
		http.Error(w, `{"error":"batch size exceeds limit of 100"}`, http.StatusBadRequest)
		return
	}

	// userID is best-effort: the route is JWT-gated so it is normally present,
	// but a missing id degrades to a NULL-user row rather than dropping the
	// event.
	userID, _ := middleware.GetUserID(r.Context())

	if err := h.svc.Ingest(r.Context(), userID, toServiceTelemetry(events)); err != nil {
		slog.ErrorContext(r.Context(), "telemetry ingest failed", "error", err)
		http.Error(w, `{"error":"internal_server_error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// toServiceTelemetry maps the openapi wire events to service-layer events,
// keeping HTTP/wire concerns out of the service.
func toServiceTelemetry(events []openapi.TelemetryEvent) []service.TelemetryEvent {
	out := make([]service.TelemetryEvent, 0, len(events))
	for _, e := range events {
		ev := service.TelemetryEvent{
			EventType:     e.EventType,
			Action:        e.Action,
			Page:          e.Page,
			CorrelationID: strDeref(e.CorrelationId),
			ClientTS:      e.Timestamp,
		}
		if e.Metadata != nil {
			ev.Metadata = *e.Metadata
		}
		out = append(out, ev)
	}
	return out
}
