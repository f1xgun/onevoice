package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// findMetric searches gathered metric families for a metric matching the given name.
func findMetric(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range families {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

// findSample searches metrics for one matching the given label values.
func findSample(mf *dto.MetricFamily, labels map[string]string) *dto.Metric {
	for _, m := range mf.GetMetric() {
		match := true
		for _, lp := range m.GetLabel() {
			if want, ok := labels[lp.GetName()]; ok {
				if lp.GetValue() != want {
					match = false
					break
				}
			}
		}
		if match {
			return m
		}
	}
	return nil
}

func TestHTTPMiddleware_RecordsMetrics(t *testing.T) {
	r := chi.NewRouter()
	r.Use(HTTPMiddleware)
	r.Get("/test-metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test-metrics", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	mf := findMetric(families, "http_requests_total")
	if mf == nil {
		t.Fatal("http_requests_total metric family not found")
	}

	sample := findSample(mf, map[string]string{
		"method": "GET",
		"path":   "/test-metrics",
		"status": "200",
	})
	if sample == nil {
		t.Fatal("http_requests_total sample with expected labels not found")
	}
	if sample.GetCounter().GetValue() < 1 {
		t.Errorf("expected counter >= 1, got %f", sample.GetCounter().GetValue())
	}

	dMf := findMetric(families, "http_request_duration_seconds")
	if dMf == nil {
		t.Fatal("http_request_duration_seconds metric family not found")
	}
}

func TestHTTPMiddleware_UsesRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(HTTPMiddleware)
	r.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/items/123", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	mf := findMetric(families, "http_requests_total")
	if mf == nil {
		t.Fatal("http_requests_total metric family not found")
	}

	patternSample := findSample(mf, map[string]string{
		"method": "GET",
		"path":   "/items/{id}",
		"status": "200",
	})
	if patternSample == nil {
		t.Fatal("expected path label to be '/items/{id}' (route pattern), not the actual URL")
	}

	rawSample := findSample(mf, map[string]string{
		"method": "GET",
		"path":   "/items/123",
		"status": "200",
	})
	if rawSample != nil {
		t.Fatal("path label should be route pattern '/items/{id}', not raw URL '/items/123'")
	}
}

func TestHTTPMiddleware_UnmatchedPathCollapsesToSentinel(t *testing.T) {
	r := chi.NewRouter()
	r.Use(HTTPMiddleware)
	r.Get("/registered", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const rawPath = "/health/" + "attacker-controlled-unique-garbage-9f3c"

	req := httptest.NewRequest(http.MethodGet, rawPath, http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	mf := findMetric(families, "http_requests_total")
	if mf == nil {
		t.Fatal("http_requests_total metric family not found")
	}

	if findSample(mf, map[string]string{"method": "GET", "path": rawPath}) != nil {
		t.Fatalf("unmatched request must not create a {path=%q} series; raw URL would let an unauthenticated attacker explode metric cardinality", rawPath)
	}

	if findSample(mf, map[string]string{"method": "GET", "path": "<unmatched>"}) == nil {
		t.Fatal("expected unmatched request to collapse into the single {path=\"<unmatched>\"} bucket")
	}
}

func TestNormalizeHTTPMethod(t *testing.T) {
	for _, m := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace,
	} {
		if got := normalizeHTTPMethod(m); got != m {
			t.Errorf("normalizeHTTPMethod(%q) = %q, want %q", m, got, m)
		}
	}

	for _, m := range []string{"BREW123", "PROPFIND", "", "get", "lowercase-token", "MKCOL"} {
		if got := normalizeHTTPMethod(m); got != labelOther {
			t.Errorf("normalizeHTTPMethod(%q) = %q, want %q", m, got, labelOther)
		}
	}
}

// TestHTTPMiddleware_UnknownMethodCollapsesToOther proves the {method} label is
// bounded to the standard HTTP method set. An attacker can send arbitrary RFC
// 7230 method tokens; without normalization each fresh token mints a new label
// series and leaks memory. This test fails if normalizeHTTPMethod is reverted.
func TestHTTPMiddleware_UnknownMethodCollapsesToOther(t *testing.T) {
	r := chi.NewRouter()
	r.Use(HTTPMiddleware)
	r.Handle("/method-bound", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const hostile = "BREW123"
	req := httptest.NewRequest(hostile, "/method-bound", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	mf := findMetric(families, "http_requests_total")
	if mf == nil {
		t.Fatal("http_requests_total metric family not found")
	}

	if findSample(mf, map[string]string{"method": hostile}) != nil {
		t.Fatalf("attacker method %q must not create a raw {method=%q} series; it would let an unauthenticated attacker explode metric cardinality", hostile, hostile)
	}

	if findSample(mf, map[string]string{"method": "other"}) == nil {
		t.Fatal("expected unknown method to collapse into the single {method=\"other\"} bucket")
	}
}

// TestHTTPMiddleware_MethodLabelSetStaysBounded proves that hammering the
// middleware with many distinct attacker-controlled method tokens does not grow
// the {method} label set without bound. Reverting normalizeHTTPMethod makes each
// raw token appear as its own series and fails this assertion.
func TestHTTPMiddleware_MethodLabelSetStaysBounded(t *testing.T) {
	r := chi.NewRouter()
	r.Use(HTTPMiddleware)
	r.Handle("/bounded", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 200; i++ {
		req := httptest.NewRequest(fmt.Sprintf("ZZTOKEN%d", i), "/bounded", http.NoBody)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	mf := findMetric(families, "http_requests_total")
	if mf == nil {
		t.Fatal("http_requests_total metric family not found")
	}

	distinctMethods := map[string]struct{}{}
	for _, m := range mf.GetMetric() {
		for _, lp := range m.GetLabel() {
			if lp.GetName() == "method" {
				distinctMethods[lp.GetValue()] = struct{}{}
			}
		}
	}

	if len(distinctMethods) > len(allowedHTTPMethods)+1 {
		t.Fatalf("method label cardinality unbounded: %d distinct values seen %v; expected <= %d (allowlist + \"other\")", len(distinctMethods), distinctMethods, len(allowedHTTPMethods)+1)
	}
	if _, ok := distinctMethods["ZZTOKEN0"]; ok {
		t.Fatal("raw attacker method token leaked into the method label set")
	}
}

func TestIncAppError_NormalizesUnknownService(t *testing.T) {
	const hostile = "'; DROP TABLE x; --"
	IncAppError(ServiceAPI)
	IncAppError(ServiceOrchestrator)
	IncAppError(hostile)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	mf := findMetric(families, "app_errors_total")
	if mf == nil {
		t.Fatal("app_errors_total metric family not found")
	}

	for _, service := range []string{ServiceAPI, ServiceOrchestrator, "unknown"} {
		if findSample(mf, map[string]string{"service": service}) == nil {
			t.Errorf("expected app_errors_total sample for service=%q", service)
		}
	}

	if findSample(mf, map[string]string{"service": hostile}) != nil {
		t.Error("hostile service value must collapse to 'unknown', not create a raw label series")
	}
}
