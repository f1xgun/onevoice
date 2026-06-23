package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
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
	defer func() { _ = redisClient.Close() }()

	limit := 5
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "success", rr.Body.String())

		assert.Equal(t, strconv.Itoa(limit), rr.Header().Get("X-RateLimit-Limit"))

		remaining, err := strconv.Atoi(rr.Header().Get("X-RateLimit-Remaining"))
		require.NoError(t, err)
		assert.Equal(t, limit-i-1, remaining)

		assert.NotEmpty(t, rr.Header().Get("X-RateLimit-Reset"))
	}
}

func TestRateLimit_ExceedsLimit(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 3
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "rate limit exceeded", errResp.Error)

	assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_DifferentIPs(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 2
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req.RemoteAddr = "192.168.1.2:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "1", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_DifferentPaths(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 2
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/path1", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	}

	req := httptest.NewRequest("GET", "/api/v1/path2", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "1", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_XForwardedFor(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 2
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req1 := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req1.RemoteAddr = "192.168.1.100:12345"
	req1.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")

	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	assert.Equal(t, http.StatusOK, rr1.Code)
	assert.Equal(t, "1", rr1.Header().Get("X-RateLimit-Remaining"))

	req2 := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req2.RemoteAddr = "192.168.1.200:12345"
	req2.Header.Set("X-Forwarded-For", "203.0.113.1")

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusOK, rr2.Code)
	assert.Equal(t, "0", rr2.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_XRealIP(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 1
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("X-Real-IP", "203.0.113.1")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))

	req2 := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req2.RemoteAddr = "192.168.1.200:12345"
	req2.Header.Set("X-Real-IP", "203.0.113.1")

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")

	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.1", ip)
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.1")

	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.1", ip)
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	ip := getClientIP(req)
	assert.Equal(t, "192.168.1.1", ip)
}

func TestGetClientIP_Priority(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	req.Header.Set("X-Real-IP", "198.51.100.1")

	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.1", ip)
}

func TestRateLimitByUser_WithinLimit(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 5
	window := time.Minute
	userID := uuid.New()

	handler := RateLimitByUser(redisClient, limit, window, "chat")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"
		ctx := context.WithValue(req.Context(), UserIDKey, userID)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, strconv.Itoa(limit), rr.Header().Get("X-RateLimit-Limit"))

		remaining, err := strconv.Atoi(rr.Header().Get("X-RateLimit-Remaining"))
		require.NoError(t, err)
		assert.Equal(t, limit-i-1, remaining)
	}
}

func TestRateLimitByUser_ExceedsLimit(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 3
	window := time.Minute
	userID := uuid.New()

	handler := RateLimitByUser(redisClient, limit, window, "chat")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"
		ctx := context.WithValue(req.Context(), UserIDKey, userID)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	ctx := context.WithValue(req.Context(), UserIDKey, userID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	var errResp rateLimitErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "rate limit exceeded", errResp.Error)
	assert.NotEmpty(t, rr.Header().Get("Retry-After"))
}

func TestRateLimitByUser_FallbackToIP(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 2
	window := time.Minute

	handler := RateLimitByUser(redisClient, limit, window, "chat")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}

func TestRateLimitByUser_DifferentUsers(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 2
	window := time.Minute
	user1 := uuid.New()
	user2 := uuid.New()

	handler := RateLimitByUser(redisClient, limit, window, "chat")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"
		ctx := context.WithValue(req.Context(), UserIDKey, user1)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	req := httptest.NewRequest("GET", "/api/v1/chat", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	ctx := context.WithValue(req.Context(), UserIDKey, user2)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "1", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_RedisDown(t *testing.T) {
	redisClient, mr := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 5
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	mr.Close()

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "success", rr.Body.String())
}

func TestRateLimit_RetryAfterHeader(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 1
	window := time.Minute

	handler := RateLimit(redisClient, limit, window)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	req2 := httptest.NewRequest("GET", "/api/v1/test", http.NoBody)
	req2.RemoteAddr = "192.168.1.1:12345"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)

	retryAfter := rr2.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfter)
	retryAfterInt, err := strconv.Atoi(retryAfter)
	require.NoError(t, err)
	assert.Greater(t, retryAfterInt, 0)
}

func reqWithUser(userID uuid.UUID) *http.Request {
	req := httptest.NewRequest("POST", "/api/v1/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	return req.WithContext(context.WithValue(req.Context(), UserIDKey, userID))
}

func TestRateLimitByUser_NilRedis_NoOp(t *testing.T) {
	handler := RateLimitByUser(nil, 1, time.Minute, "writes")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	user := uuid.New()
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, reqWithUser(user))
		assert.Equal(t, http.StatusOK, rr.Code, "nil redis must disable limiting, never 429")
	}
}

func TestRateLimitByUser_ZeroLimit_NoOp(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	// A zero (unset/forgotten) budget must NOT 429 everything — it disables the
	// limiter, the safe failure mode.
	handler := RateLimitByUser(redisClient, 0, time.Minute, "writes")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	user := uuid.New()
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, reqWithUser(user))
		assert.Equal(t, http.StatusOK, rr.Code, "zero limit must disable limiting, never 429")
	}
}

func TestRateLimitByUser_PerUserBucket(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	limit := 2
	handler := RateLimitByUser(redisClient, limit, time.Minute, "writes")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	userA := uuid.New()
	for i := 0; i < limit; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, reqWithUser(userA))
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, reqWithUser(userA))
	assert.Equal(t, http.StatusTooManyRequests, rr.Code, "userA must be throttled past the limit")

	// A different user has an independent bucket.
	rrB := httptest.NewRecorder()
	handler.ServeHTTP(rrB, reqWithUser(uuid.New()))
	assert.Equal(t, http.StatusOK, rrB.Code, "userB must not inherit userA's bucket")
}

func TestRateLimitByUser_SeparateScopesDoNotShareBucket(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	user := uuid.New()
	writes := RateLimitByUser(redisClient, 1, time.Minute, "writes")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	invites := RateLimitByUser(redisClient, 1, time.Minute, "invitations")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rrW := httptest.NewRecorder()
	writes.ServeHTTP(rrW, reqWithUser(user))
	assert.Equal(t, http.StatusOK, rrW.Code)

	// Same user, different scope → its own bucket, not yet exhausted.
	rrI := httptest.NewRecorder()
	invites.ServeHTTP(rrI, reqWithUser(user))
	assert.Equal(t, http.StatusOK, rrI.Code, "writes and invitations buckets must be independent")
}
