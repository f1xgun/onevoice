package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCORS_AllowedOrigin(t *testing.T) {
	allowedOrigins := []string{"http://localhost:3000", "http://localhost:3001"}

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")

	rr := httptest.NewRecorder()

	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "http://localhost:3000", rr.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Authorization, Content-Type", rr.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "300", rr.Header().Get("Access-Control-Max-Age"))
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	allowedOrigins := []string{"http://localhost:3000"}

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Origin", "http://evil.com")

	rr := httptest.NewRecorder()

	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_NoOriginHeader(t *testing.T) {
	allowedOrigins := []string{"http://localhost:3000"}

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	// No Origin header

	rr := httptest.NewRecorder()

	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_WildcardOrigin(t *testing.T) {
	allowedOrigins := []string{"*"}

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Origin", "http://any-origin.com")

	rr := httptest.NewRecorder()

	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "http://any-origin.com", rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_PreflightRequest(t *testing.T) {
	allowedOrigins := []string{"http://localhost:3000"}

	req := httptest.NewRequest("OPTIONS", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")

	rr := httptest.NewRecorder()

	handlerCalled := false
	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.False(t, handlerCalled, "handler should not be called for OPTIONS request")
	assert.Equal(t, "http://localhost:3000", rr.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORS_CaseInsensitiveOrigin(t *testing.T) {
	allowedOrigins := []string{"http://localhost:3000"}

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Origin", "HTTP://LOCALHOST:3000")

	rr := httptest.NewRecorder()

	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "HTTP://LOCALHOST:3000", rr.Header().Get("Access-Control-Allow-Origin"))
}
