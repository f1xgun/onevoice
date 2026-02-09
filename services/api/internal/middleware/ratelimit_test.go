package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return client, mr
}

func TestRateLimit_WithinLimit(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	limit := 5
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	// Make 5 requests (within limit)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "success", rr.Body.String())

		// Check rate limit headers
		assert.Equal(t, strconv.Itoa(limit), rr.Header().Get("X-RateLimit-Limit"))

		remaining, err := strconv.Atoi(rr.Header().Get("X-RateLimit-Remaining"))
		require.NoError(t, err)
		assert.Equal(t, limit-i-1, remaining)

		assert.NotEmpty(t, rr.Header().Get("X-RateLimit-Reset"))
	}
}

func TestRateLimit_ExceedsLimit(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	limit := 3
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	// Make 3 requests (at limit)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "rate limit exceeded", errResp.Error)

	// Check headers
	assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_DifferentIPs(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	limit := 2
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	// IP 1: Make 2 requests (at limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// IP 2: Should still be able to make requests
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.RemoteAddr = "192.168.1.2:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "1", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_DifferentPaths(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	limit := 2
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	// Path 1: Make 2 requests (at limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/path1", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// Path 2: Should have separate limit
	req := httptest.NewRequest("GET", "/api/v1/path2", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "1", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_XForwardedFor(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	limit := 2
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	// Request 1: Use X-Forwarded-For header
	req1 := httptest.NewRequest("GET", "/api/v1/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	req1.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")

	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	assert.Equal(t, http.StatusOK, rr1.Code)
	assert.Equal(t, "1", rr1.Header().Get("X-RateLimit-Remaining"))

	// Request 2: Same X-Forwarded-For IP (should count against same limit)
	req2 := httptest.NewRequest("GET", "/api/v1/test", nil)
	req2.RemoteAddr = "192.168.1.200:12345"
	req2.Header.Set("X-Forwarded-For", "203.0.113.1")

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusOK, rr2.Code)
	assert.Equal(t, "0", rr2.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_XRealIP(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	limit := 1
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("X-Real-IP", "203.0.113.1")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))

	// Second request with same X-Real-IP should be rate limited
	req2 := httptest.NewRequest("GET", "/api/v1/test", nil)
	req2.RemoteAddr = "192.168.1.200:12345"
	req2.Header.Set("X-Real-IP", "203.0.113.1")

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")

	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.1", ip)
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.1")

	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.1", ip)
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	ip := getClientIP(req)
	assert.Equal(t, "192.168.1.1", ip)
}

func TestGetClientIP_Priority(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	req.Header.Set("X-Real-IP", "198.51.100.1")

	// X-Forwarded-For should take priority
	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.1", ip)
}
