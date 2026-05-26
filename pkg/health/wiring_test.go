package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/health"
)

// TestRegisterDefaultChecks_RegistersAllFour confirms that for the deps the
// helper is given (non-nil arg), an AddCheck call lands under the expected
// canonical name. Heavy clients (pgxpool, mongo, nats) are exercised via the
// nil-skip branch in the companion test; here we pass a real redis (miniredis)
// to prove the redis branch actually wires the named check.
func TestRegisterDefaultChecks_RegistersAllFour(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	c := health.New()
	health.RegisterDefaultChecks(c, nil, nil, rdb, nil)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()
	c.ReadyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatalf("checks field missing or wrong type: %v", body)
	}
	if got := checks["redis"]; got != "ok" {
		t.Fatalf("expected redis=ok, got %v (full=%v)", got, checks)
	}
	// Only the registered dep should appear — pg/mongo/nats were nil and
	// MUST have been skipped.
	if _, present := checks["postgres"]; present {
		t.Fatalf("postgres should NOT be registered when pg arg is nil")
	}
	if _, present := checks["mongo"]; present {
		t.Fatalf("mongo should NOT be registered when mongoClient arg is nil")
	}
	if _, present := checks["nats"]; present {
		t.Fatalf("nats should NOT be registered when natsConn arg is nil")
	}
	if len(checks) != 1 {
		t.Fatalf("expected exactly 1 check registered (redis), got %d: %v", len(checks), checks)
	}
}

// TestRegisterDefaultChecks_SkipsNilDeps asserts the helper tolerates every
// argument being nil (used by services that own none of the standard deps,
// or during early bootstrap before deps connect).
func TestRegisterDefaultChecks_SkipsNilDeps(t *testing.T) {
	c := health.New()

	// Must not panic on nil-everywhere.
	health.RegisterDefaultChecks(c, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()
	c.ReadyHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no checks, got %d (body=%s)", rec.Code, rec.Body.String())
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
		t.Fatalf("checks field missing or wrong type: %v", body)
	}
	if len(checks) != 0 {
		t.Fatalf("expected empty checks map, got %v", checks)
	}
}
