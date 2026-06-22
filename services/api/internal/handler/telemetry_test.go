package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type fakeTelemetryIngester struct {
	called    int
	gotUserID uuid.UUID
	got       []service.TelemetryEvent
	err       error
}

func (f *fakeTelemetryIngester) Ingest(_ context.Context, userID uuid.UUID, events []service.TelemetryEvent) error {
	f.called++
	f.gotUserID = userID
	f.got = events
	return f.err
}

func TestTelemetryHandler_Ingest_ValidBatch(t *testing.T) {
	fake := &fakeTelemetryIngester{}
	h := NewTelemetryHandler(fake)

	cid := "abc-123"
	meta := map[string]string{"btn": "save"}
	events := []openapi.TelemetryEvent{
		{EventType: "page_view", Page: "/dashboard", Action: "load", Timestamp: "2026-03-22T09:00:00Z"},
		{EventType: "click", Page: "/settings", Action: "save", CorrelationId: &cid, Metadata: &meta, Timestamp: "2026-03-22T09:00:01Z"},
	}
	body, _ := json.Marshal(events)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, fake.called)
	require.Len(t, fake.got, 2)
	assert.Equal(t, "page_view", fake.got[0].EventType)
	assert.Equal(t, "click", fake.got[1].EventType)
	assert.Equal(t, "abc-123", fake.got[1].CorrelationID)
	assert.Equal(t, map[string]string{"btn": "save"}, fake.got[1].Metadata)
}

func TestTelemetryHandler_Ingest_InvalidJSON(t *testing.T) {
	fake := &fakeTelemetryIngester{}
	h := NewTelemetryHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, fake.called)
}

func TestTelemetryHandler_Ingest_EmptyArray(t *testing.T) {
	fake := &fakeTelemetryIngester{}
	h := NewTelemetryHandler(fake)

	body, _ := json.Marshal([]openapi.TelemetryEvent{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestTelemetryHandler_Ingest_ExceedsBatchLimit(t *testing.T) {
	fake := &fakeTelemetryIngester{}
	h := NewTelemetryHandler(fake)

	events := make([]openapi.TelemetryEvent, 101)
	for i := range events {
		events[i] = openapi.TelemetryEvent{EventType: "test", Page: "/", Action: "x", Timestamp: "2026-01-01T00:00:00Z"}
	}
	body, err := json.Marshal(events)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "batch size")
	assert.Zero(t, fake.called)
}

func TestTelemetryHandler_Ingest_ServiceError(t *testing.T) {
	fake := &fakeTelemetryIngester{err: errors.New("db down")}
	h := NewTelemetryHandler(fake)

	body, _ := json.Marshal([]openapi.TelemetryEvent{{EventType: "page_view", Page: "/", Action: "load", Timestamp: "2026-01-01T00:00:00Z"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Ingest(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
