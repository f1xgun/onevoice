package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/logger"
)

// appErrorsValue scrapes the current app_errors_total{service="api"} value from
// the default Prometheus gatherer. The counter is unexported in pkg/metrics, so
// the test reads it the same way Prometheus does — by gathering and matching on
// the metric name + label — rather than reaching into the package internals.
func appErrorsValue(t *testing.T, service string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() != "app_errors_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelValue(m, "service") == service {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func TestRecoverer_PanicYields500WithEnvelopeMetricAndLog(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(logger.NewContextHandler(slog.NewJSONHandler(&buf, nil))))

	const corrID = "test-corr-id-123"
	before := appErrorsValue(t, "api")

	handler := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
	req = req.WithContext(logger.WithCorrelationID(req.Context(), corrID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "internal server error", body.Error,
		"recovered 500 must reuse the existing error envelope, not a new shape")

	after := appErrorsValue(t, "api")
	assert.InDelta(t, before+1, after, 0.0001,
		"a recovered panic must increment app_errors_total{service=\"api\"}")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "recovered from panic")
	assert.Contains(t, logOutput, corrID,
		"the panic log must carry the request correlation_id")
	assert.Contains(t, logOutput, `"correlation_id":"`+corrID+`"`)
}

func TestRecoverer_NoPanicPassesThrough(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestRecoverer_ReraisesAbortHandler(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
	rr := httptest.NewRecorder()

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		handler.ServeHTTP(rr, req)
	}, "ErrAbortHandler must propagate so the stdlib server can handle it")
}
