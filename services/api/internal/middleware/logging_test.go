package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequestLogger_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	logOutput := buf.String()

	// Check that both "request started" and "request completed" logs exist
	assert.Contains(t, logOutput, "request started")
	assert.Contains(t, logOutput, "request completed")
	assert.Contains(t, logOutput, `"method":"GET"`)
	assert.Contains(t, logOutput, `"path":"/api/v1/businesses"`)
	assert.Contains(t, logOutput, `"status":200`)
	assert.Contains(t, logOutput, `"remote_addr":"192.168.1.1:12345"`)
	assert.Contains(t, logOutput, "duration_ms")
}

func TestRequestLogger_ErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/unknown", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"status":404`)
}

func TestRequestLogger_PostRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))

	req := httptest.NewRequest("POST", "/api/v1/businesses", strings.NewReader(`{"name":"test"}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"method":"POST"`)
	assert.Contains(t, logOutput, `"status":201`)
}

func TestRequestLogger_ImplicitStatusOK(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't explicitly call WriteHeader - should default to 200
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"status":200`)
}

func TestResponseWriter_WriteHeaderOnce(t *testing.T) {
	rr := httptest.NewRecorder()
	wrapped := wrapResponseWriter(rr)

	// First WriteHeader should be recorded
	wrapped.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, wrapped.status)

	// Second WriteHeader should be ignored
	wrapped.WriteHeader(http.StatusInternalServerError)
	assert.Equal(t, http.StatusCreated, wrapped.status, "status should not change after first WriteHeader")
}

func TestResponseWriter_WriteWithoutWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	wrapped := wrapResponseWriter(rr)

	// Write without calling WriteHeader should default to 200
	n, err := wrapped.Write([]byte("test"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, http.StatusOK, wrapped.status)
}

// --- : / PITFALLS §1.4 query-string scrubbing -----------

// TestRequestLogger_StripsTokenFromConfirmPath asserts the access-log
// belt-and-suspenders defense: requests to /auth/password-reset/confirm
// must NEVER leak the ?token=… query string downstream — neither in
// this logger's own output nor via the mutated r.URL.RawQuery the next
// handler in the chain sees.
func TestRequestLogger_StripsTokenFromConfirmPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	var rawQueryAtHandlerTime string
	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture what the inner handler sees — RawQuery must already
		// be scrubbed by the time we reach here.
		rawQueryAtHandlerTime = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm?token=secret123", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.NotContains(t, buf.String(), "secret123",
		"access log must not surface the ?token=… fragment")
	assert.Equal(t, "", rawQueryAtHandlerTime,
		"downstream handler must observe an empty RawQuery for the confirm path")
}

// TestRequestLogger_PassThroughRawQueryForOtherPaths asserts the scrub
// is path-scoped — unrelated requests' query strings stay intact for
// the downstream handler chain (e.g. cursor pagination on /businesses).
func TestRequestLogger_PassThroughRawQueryForOtherPaths(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	var observedRawQuery string
	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/businesses?cursor=abc", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "cursor=abc", observedRawQuery,
		"non-reset paths must preserve their query string for downstream handlers")
}
