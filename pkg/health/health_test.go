package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveHandler_Always200(t *testing.T) {
	c := New()
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()

	c.LiveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "alive" {
		t.Fatalf("expected status=alive, got %q", body["status"])
	}
}

func TestReadyHandler_AllHealthy(t *testing.T) {
	c := New()
	c.AddCheck("db", func(ctx context.Context) error { return nil })
	c.AddCheck("cache", func(ctx context.Context) error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	c.ReadyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "ready" {
		t.Fatalf("expected status=ready, got %v", body["status"])
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field missing or wrong type")
	}
	if checks["db"] != "ok" {
		t.Fatalf("expected db=ok, got %v", checks["db"])
	}
	if checks["cache"] != "ok" {
		t.Fatalf("expected cache=ok, got %v", checks["cache"])
	}
}

func TestReadyHandler_OneFailing(t *testing.T) {
	c := New()
	c.AddCheck("db", func(ctx context.Context) error { return nil })
	c.AddCheck("cache", func(ctx context.Context) error {
		return errors.New("connection refused")
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	c.ReadyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("expected status=not_ready, got %v", body["status"])
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field missing or wrong type")
	}
	if checks["cache"] != "connection refused" {
		t.Fatalf("expected cache=connection refused, got %v", checks["cache"])
	}
}

func TestReadyHandler_NoChecks(t *testing.T) {
	c := New()

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	c.ReadyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "ready" {
		t.Fatalf("expected status=ready, got %v", body["status"])
	}
}
