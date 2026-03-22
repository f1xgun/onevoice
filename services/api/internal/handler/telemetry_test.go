package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryHandler_Ingest_ValidBatch(t *testing.T) {
	// Capture log output to verify structured logging
	var logBuf bytes.Buffer
	logHandler := slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(logHandler))
	defer slog.SetDefault(origLogger)

	h := NewTelemetryHandler()

	events := []TelemetryEvent{
		{EventType: "page_view", Page: "/dashboard", Action: "load", Timestamp: "2026-03-22T09:00:00Z"},
		{EventType: "click", Page: "/settings", Action: "save", CorrelationID: "abc-123", Metadata: map[string]string{"btn": "save"}, Timestamp: "2026-03-22T09:00:01Z"},
	}
	body, _ := json.Marshal(events)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify both events were logged
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "frontend_event")
	assert.Contains(t, logOutput, "page_view")
	assert.Contains(t, logOutput, "click")
}

func TestTelemetryHandler_Ingest_InvalidJSON(t *testing.T) {
	h := NewTelemetryHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTelemetryHandler_Ingest_EmptyArray(t *testing.T) {
	h := NewTelemetryHandler()

	body, _ := json.Marshal([]TelemetryEvent{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestTelemetryHandler_Ingest_ExceedsBatchLimit(t *testing.T) {
	h := NewTelemetryHandler()

	events := make([]TelemetryEvent, 101)
	for i := range events {
		events[i] = TelemetryEvent{EventType: "test", Page: "/", Action: "x", Timestamp: "2026-01-01T00:00:00Z"}
	}
	body, err := json.Marshal(events)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "batch size")
}
