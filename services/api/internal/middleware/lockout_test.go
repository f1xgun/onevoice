package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

func newTestLock(t *testing.T) (*lockout.Lockout, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return lockout.New(client, lockout.Config{}), mr
}

// helper: build a login request with a JSON body.
func newLoginReq(email string) *http.Request {
	body, _ := json.Marshal(map[string]string{"email": email, "password": "secret-1234"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	r.RemoteAddr = "1.2.3.4:12345"
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestLockoutMiddleware_PassesNormal(t *testing.T) {
	lock, _ := newTestLock(t)
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	var downstreamRan bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamRan = true
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Contains(t, string(bodyBytes), "alice@example.com")
		w.WriteHeader(http.StatusOK)
	})

	h := middleware.LockoutMiddleware(lock)(next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newLoginReq("alice@example.com"))

	assert.True(t, downstreamRan, "downstream handler must run on TierNormal")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, middleware.CaptchaRequired(context.Background()), "no captcha ctx on TierNormal")
}

func TestLockoutMiddleware_AnnotatesCaptchaContext(t *testing.T) {
	lock, _ := newTestLock(t)
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_, err := lock.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
		require.NoError(t, err)
	}

	var downstreamFlag bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamFlag = middleware.CaptchaRequired(r.Context())
		assert.Equal(t, "alice@example.com", middleware.LoginEmail(r.Context()))
		assert.Equal(t, "1.2.3.4", middleware.LoginClientIP(r.Context()))
		w.WriteHeader(http.StatusOK)
	})

	h := middleware.LockoutMiddleware(lock)(next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newLoginReq("alice@example.com"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, downstreamFlag, "TierCaptcha must annotate ctx with CaptchaRequiredKey=true")
}

func TestLockoutMiddleware_Returns423OnLock(t *testing.T) {
	lock, _ := newTestLock(t)
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, err := lock.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
		require.NoError(t, err)
	}

	var downstreamRan bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamRan = true
	})

	h := middleware.LockoutMiddleware(lock)(next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newLoginReq("alice@example.com"))

	assert.False(t, downstreamRan, "TierLocked MUST short-circuit before downstream")
	assert.Equal(t, http.StatusLocked, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "account_locked", body["code"])
	retryAfter, ok := body["retry_after_seconds"].(float64)
	require.True(t, ok, "retry_after_seconds must be numeric")
	assert.Positive(t, retryAfter, "retry_after_seconds must be > 0 while locked")
}

func TestLockoutMiddleware_KeyUsesEmailHashAndNet16(t *testing.T) {
	lock, mr := newTestLock(t)
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, err := lock.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
		require.NoError(t, err)
	}

	keys := mr.Keys()
	var matched int
	for _, k := range keys {
		if strings.HasPrefix(k, "onevoice:lockout:") && strings.HasSuffix(k, ":1.2.0.0/16") {
			matched++
		}
	}
	assert.Equal(t, 1, matched, "exactly one (email_hash, /16) key must exist")

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not run for a locked tuple")
	})
	rec := httptest.NewRecorder()
	middleware.LockoutMiddleware(lock)(next).ServeHTTP(rec, newLoginReq("alice@example.com"))
	assert.Equal(t, http.StatusLocked, rec.Code)
}

func TestLockoutMiddleware_DifferentNet16_NotLocked(t *testing.T) {
	lock, _ := newTestLock(t)
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, err := lock.RecordFailure(ctx, "alice@example.com", "9.9.0.0/16")
		require.NoError(t, err)
	}

	var downstreamRan bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamRan = true
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	middleware.LockoutMiddleware(lock)(next).ServeHTTP(rec, newLoginReq("alice@example.com"))

	assert.True(t, downstreamRan, "victim in a different /16 must not be locked out")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLockoutMiddleware_InvalidJSON_PassesThrough(t *testing.T) {
	lock, _ := newTestLock(t)
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	var downstreamRan bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamRan = true
		w.WriteHeader(http.StatusBadRequest)
	})

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString("not json"))
	r.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()
	middleware.LockoutMiddleware(lock)(next).ServeHTTP(rec, r)

	assert.True(t, downstreamRan, "invalid JSON must pass through to downstream")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
