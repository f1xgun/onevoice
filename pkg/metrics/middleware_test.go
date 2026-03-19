package metrics

import (
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

	req := httptest.NewRequest(http.MethodGet, "/test-metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Check http_requests_total has a sample
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

	// Check http_request_duration_seconds has a sample
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

	req := httptest.NewRequest(http.MethodGet, "/items/123", nil)
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

	// Should use route pattern /items/{id}, not /items/123
	patternSample := findSample(mf, map[string]string{
		"method": "GET",
		"path":   "/items/{id}",
		"status": "200",
	})
	if patternSample == nil {
		t.Fatal("expected path label to be '/items/{id}' (route pattern), not the actual URL")
	}

	// Verify that /items/123 is NOT used as the path label
	rawSample := findSample(mf, map[string]string{
		"method": "GET",
		"path":   "/items/123",
		"status": "200",
	})
	if rawSample != nil {
		t.Fatal("path label should be route pattern '/items/{id}', not raw URL '/items/123'")
	}
}
