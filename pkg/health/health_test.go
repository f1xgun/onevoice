package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLiveHandler_Always200(t *testing.T) {
	c := New()
	req := httptest.NewRequest(http.MethodGet, "/health/live", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
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

func TestReadyHandler_AllFailing(t *testing.T) {
	c := New()
	c.AddCheck("db", func(ctx context.Context) error {
		return errors.New("db unreachable")
	})
	c.AddCheck("cache", func(ctx context.Context) error {
		return errors.New("cache timeout")
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
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
	if checks["db"] != "db unreachable" {
		t.Fatalf("expected db error, got %v", checks["db"])
	}
	if checks["cache"] != "cache timeout" {
		t.Fatalf("expected cache error, got %v", checks["cache"])
	}
}

func TestReadyHandler_ContextTimeout(t *testing.T) {
	c := New()
	c.AddCheck("slow", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	// Use a pre-canceled context so the check returns immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody).WithContext(ctx)
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
	slowResult, ok := checks["slow"].(string)
	if !ok {
		t.Fatal("slow check result missing")
	}
	if !strings.Contains(slowResult, "context") {
		t.Fatalf("expected context error, got %q", slowResult)
	}
}

func TestLiveHandler_ContentType(t *testing.T) {
	c := New()
	req := httptest.NewRequest(http.MethodGet, "/health/live", http.NoBody)
	rec := httptest.NewRecorder()

	c.LiveHandler().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", ct)
	}
}

func TestReadyHandler_ContentType(t *testing.T) {
	c := New()
	c.AddCheck("db", func(ctx context.Context) error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	c.ReadyHandler().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", ct)
	}
}

func TestAddCheck_ConcurrentSafety(t *testing.T) {
	c := New()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("check-%d", n)
			c.AddCheck(name, func(ctx context.Context) error { return nil })
		}(i)
	}
	wg.Wait()

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	c.ReadyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field missing or wrong type")
	}

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("check-%d", i)
		if checks[name] != "ok" {
			t.Fatalf("expected %s=ok, got %v", name, checks[name])
		}
	}
}

func TestReadyHandler_MixedResults(t *testing.T) {
	c := New()
	c.AddCheck("db", func(ctx context.Context) error { return nil })
	c.AddCheck("cache", func(ctx context.Context) error { return nil })
	c.AddCheck("queue", func(ctx context.Context) error {
		return errors.New("queue down")
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
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
	if checks["db"] != "ok" {
		t.Fatalf("expected db=ok, got %v", checks["db"])
	}
	if checks["cache"] != "ok" {
		t.Fatalf("expected cache=ok, got %v", checks["cache"])
	}
	if checks["queue"] != "queue down" {
		t.Fatalf("expected queue error, got %v", checks["queue"])
	}
}
