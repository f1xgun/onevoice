package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeaders_SetsAllHeaders(t *testing.T) {
	handler := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rr.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rr.Header().Get("Referrer-Policy"))
	assert.Equal(t, "camera=(), microphone=()", rr.Header().Get("Permissions-Policy"))

	csp := rr.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'self'")
	assert.Contains(t, csp, "script-src 'self'")
}

func TestSecurityHeaders_CallsNextHandler(t *testing.T) {
	called := false
	handler := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, called, "next handler should be called")
	assert.Equal(t, http.StatusTeapot, rr.Code)
}

func TestSecurityHeaders_PresentOnEveryMethod(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	handler := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/", http.NoBody)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.NotEmpty(t, rr.Header().Get("X-Frame-Options"), "X-Frame-Options missing for %s", method)
			assert.NotEmpty(t, rr.Header().Get("X-Content-Type-Options"), "X-Content-Type-Options missing for %s", method)
		})
	}
}
