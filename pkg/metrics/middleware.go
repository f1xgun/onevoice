package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Service names — the bounded value set for the {service} label on
// appErrorsTotal. Each is hard-coded at the call site (the panic-recovery
// middleware in each binary); never derive from a runtime variable. See
// pkg/metrics/README.md for the label-cardinality rules.
const (
	ServiceAPI          = "api"
	ServiceOrchestrator = "orchestrator"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	// appErrorsTotal counts unrecovered panics caught by the per-service
	// panic-recovery middleware, labeled by {service}. A non-zero rate means
	// a request handler panicked — always a bug worth paging on. Cardinality
	// is fixed at the closed {api, orchestrator} set via IncAppError.
	appErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "app_errors_total",
		Help: "Unrecovered panics caught by the recovery middleware, labeled by {service}.",
	}, []string{"service"})
)

// IncAppError records one recovered panic for service. service must be one of
// the bounded ServiceAPI / ServiceOrchestrator constants; any other value is
// normalized to "unknown" so a stray caller can never explode label cardinality.
func IncAppError(service string) {
	switch service {
	case ServiceAPI, ServiceOrchestrator:
	default:
		service = "unknown"
	}
	appErrorsTotal.WithLabelValues(service).Inc()
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Unwrap supports http.Flusher detection through middleware.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Flush implements http.Flusher so SSE streaming works through metrics middleware.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware returns a chi-compatible middleware that records HTTP metrics.
// It uses chi's RouteContext to get the URL pattern (not the actual URL) to
// prevent cardinality explosion from path parameters.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)

		next.ServeHTTP(rw, r)

		path := r.URL.Path
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if pattern := rctx.RoutePattern(); pattern != "" {
				path = pattern
			}
		}

		status := strconv.Itoa(rw.statusCode)
		duration := time.Since(start).Seconds()

		httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path, status).Observe(duration)
	})
}
