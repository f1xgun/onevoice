package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

func TestCorrelationID_GeneratesWhenMissing(t *testing.T) {
	handler := middleware.CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	corrID := rec.Header().Get("X-Correlation-ID")
	if corrID == "" {
		t.Fatal("expected X-Correlation-ID response header to be set")
	}

	// Must be a valid UUID
	if _, err := uuid.Parse(corrID); err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", corrID, err)
	}
}

func TestCorrelationID_PreservesExisting(t *testing.T) {
	handler := middleware.CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Correlation-ID", "test-corr-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	corrID := rec.Header().Get("X-Correlation-ID")
	if corrID != "test-corr-123" {
		t.Fatalf("expected X-Correlation-ID to be test-corr-123, got %q", corrID)
	}
}

func TestCorrelationID_InjectsIntoContext(t *testing.T) {
	var ctxCorrID string

	handler := middleware.CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxCorrID = logger.CorrelationIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Correlation-ID", "ctx-test-456")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if ctxCorrID != "ctx-test-456" {
		t.Fatalf("expected correlation ID in context to be ctx-test-456, got %q", ctxCorrID)
	}
}

func TestCorrelationID_GeneratedIDInContext(t *testing.T) {
	var ctxCorrID string

	handler := middleware.CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxCorrID = logger.CorrelationIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if ctxCorrID == "" {
		t.Fatal("expected generated correlation ID in context")
	}

	// Context value must match response header
	if ctxCorrID != rec.Header().Get("X-Correlation-ID") {
		t.Fatalf("context correlation ID %q does not match response header %q", ctxCorrID, rec.Header().Get("X-Correlation-ID"))
	}
}
