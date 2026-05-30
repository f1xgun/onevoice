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
	if body["status"] != "unhealthy" {
		t.Fatalf("expected status=unhealthy, got %v", body["status"])
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field missing or wrong type")
	}
	if checks["cache"] != "connection refused" {
		t.Fatalf("expected cache=connection refused, got %v", checks["cache"])
	}
	failed, ok := body["failed"].([]interface{})
	if !ok {
		t.Fatalf("failed field missing or wrong type: %v", body["failed"])
	}
	if len(failed) != 1 || failed[0] != "cache" {
		t.Fatalf("expected failed=[cache], got %v", failed)
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
	if body["status"] != "unhealthy" {
		t.Fatalf("expected status=unhealthy, got %v", body["status"])
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

	failed, ok := body["failed"].([]interface{})
	if !ok {
		t.Fatalf("failed field missing or wrong type: %v", body["failed"])
	}
	// failed[] must be alphabetically sorted for stable serialization.
	if len(failed) != 2 || failed[0] != "cache" || failed[1] != "db" {
		t.Fatalf("expected failed=[cache db] (sorted), got %v", failed)
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
	if body["status"] != "unhealthy" {
		t.Fatalf("expected status=unhealthy, got %v", body["status"])
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
	if body["status"] != "unhealthy" {
		t.Fatalf("expected status=unhealthy, got %v", body["status"])
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

// TestReadyHandler_Concurrent_DoesNotExceedBudget verifies that ReadyHandler
// runs all checks in parallel: 4 checks each sleeping 1.5s must complete
// well under the sum-of-checks (6s) and within roughly the per-check budget.
// Serial execution would exceed 6s; concurrent execution finishes in ~1.5s.
func TestReadyHandler_Concurrent_DoesNotExceedBudget(t *testing.T) {
	c := New() // default WithCheckTimeout(2s)
	for _, name := range []string{"a", "b", "c", "d"} {
		c.AddCheck(name, func(ctx context.Context) error {
			select {
			case <-time.After(1500 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	start := time.Now()
	c.ReadyHandler().ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed > 2500*time.Millisecond {
		t.Fatalf("expected ReadyHandler to finish within 2.5s (concurrent), took %s — checks ran serially", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "ready" {
		t.Fatalf("expected status=ready, got %v (body=%s)", body["status"], rec.Body.String())
	}
}

// TestReadyHandler_AnyFailure_Returns503_WithFailedList asserts that the
// JSON body carries a `failed[]` slice naming the failing dep(s) so
// operators can grep logs without inspecting the full checks map.
func TestReadyHandler_AnyFailure_Returns503_WithFailedList(t *testing.T) {
	c := New()
	c.AddCheck("postgres", func(ctx context.Context) error { return nil })
	c.AddCheck("mongo", func(ctx context.Context) error { return nil })
	c.AddCheck("nats", func(ctx context.Context) error { return nil })
	c.AddCheck("redis", func(ctx context.Context) error {
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
	if body["status"] != "unhealthy" {
		t.Fatalf("expected status=unhealthy, got %v", body["status"])
	}

	failed, ok := body["failed"].([]interface{})
	if !ok {
		t.Fatalf("failed field missing or wrong type: %v", body["failed"])
	}
	if len(failed) != 1 || failed[0] != "redis" {
		t.Fatalf("expected failed=[redis], got %v", failed)
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field missing or wrong type")
	}
	// All four names must appear in the checks map.
	for _, name := range []string{"postgres", "mongo", "nats", "redis"} {
		if _, present := checks[name]; !present {
			t.Fatalf("expected %s to appear in checks map; got %v", name, checks)
		}
	}
	if checks["redis"] != "connection refused" {
		t.Fatalf("expected redis=connection refused, got %v", checks["redis"])
	}
}

// TestReadyHandler_PerCheckTimeoutFiresWhileOthersComplete confirms the
// per-check WithCheckTimeout option is honored: one slow check trips its
// own deadline; the three quick checks still report ok.
func TestReadyHandler_PerCheckTimeoutFiresWhileOthersComplete(t *testing.T) {
	c := New(WithCheckTimeout(1 * time.Second))
	c.AddCheck("slow", func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	for _, name := range []string{"fast1", "fast2", "fast3"} {
		c.AddCheck(name, func(ctx context.Context) error { return nil })
	}

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	start := time.Now()
	c.ReadyHandler().ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed > 1500*time.Millisecond {
		t.Fatalf("expected per-check timeout (~1s) to bound the handler, took %s", elapsed)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (slow check timed out), got %d (body=%s)", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	failed, ok := body["failed"].([]interface{})
	if !ok {
		t.Fatalf("failed field missing or wrong type: %v", body["failed"])
	}
	if len(failed) != 1 || failed[0] != "slow" {
		t.Fatalf("expected failed=[slow], got %v", failed)
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field missing or wrong type")
	}
	for _, fast := range []string{"fast1", "fast2", "fast3"} {
		if checks[fast] != "ok" {
			t.Fatalf("expected %s=ok, got %v", fast, checks[fast])
		}
	}
}
