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
