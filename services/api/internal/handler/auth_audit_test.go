package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// capturingAuditLogger collects every Entry submitted via Log so tests can
// assert on action / business_id / user_id / details. Safe for concurrent use
// — the audit.Logger spawns its own goroutine; tests must wait on the WaitGroup
// before reading entries.
type capturingAuditLogger struct {
	mu      sync.Mutex
	entries []audit.Entry
	wg      sync.WaitGroup
}

// Log records the entry and decrements the WaitGroup so callers can sync.
func (c *capturingAuditLogger) Log(_ context.Context, e audit.Entry) {
	c.mu.Lock()
	c.entries = append(c.entries, e)
	c.mu.Unlock()
	c.wg.Done()
}

func (c *capturingAuditLogger) LogSync(_ context.Context, e audit.Entry) error {
	c.mu.Lock()
	c.entries = append(c.entries, e)
	c.mu.Unlock()
	c.wg.Done()
	return nil
}

// expect prepares the WaitGroup for n upcoming Log calls.
func (c *capturingAuditLogger) expect(n int) { c.wg.Add(n) }

// wait blocks until all expected Log calls have arrived.
func (c *capturingAuditLogger) wait() { c.wg.Wait() }

// snapshot returns a copy of the captured entries.
func (c *capturingAuditLogger) snapshot() []audit.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]audit.Entry, len(c.entries))
	copy(out, c.entries)
	return out
}

// TestAuthHandler_AuditLoginSuccess_RecordsUserIDAndDetails verifies that a
// successful POST /auth/login fires audit.LogLoginSuccess with user_id + IP +
// User-Agent populated in the entry.
func TestAuthHandler_AuditLoginSuccess_RecordsUserIDAndDetails(t *testing.T) {
	userID := uuid.New()
	user := &domain.User{ID: userID, Email: "alice@example.com"}

	mockUser := new(MockUserService)
	mockUser.On("Login", mock.Anything, "alice@example.com", "secret-pwd").
		Return(user, "access-jwt", "refresh-jwt", nil)

	auditLog := &capturingAuditLogger{}
	auditLog.expect(1)

	h, err := NewAuthHandler(mockUser, false, auditLog, testJWTSecret)
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"email":"alice@example.com","password":"secret-pwd"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.RemoteAddr = "192.0.2.10:50000"
	w := httptest.NewRecorder()

	h.Login(w, req)
	auditLog.wait()

	require.Equal(t, http.StatusOK, w.Code, "login should succeed")
	entries := auditLog.snapshot()
	require.Len(t, entries, 1, "expected exactly one audit entry")
	e := entries[0]
	assert.Equal(t, audit.ActionLoginSuccess, e.Action)
	assert.Equal(t, "user", e.Resource)
	require.NotNil(t, e.UserID, "login_success must record user_id")
	assert.Equal(t, userID, *e.UserID)
	assert.Nil(t, e.BusinessID, "auth events are system-wide; business_id must be nil")
	details := string(e.Details)
	assert.Contains(t, details, "192.0.2.10")
	assert.Contains(t, details, "test-agent/1.0")
}

// TestAuthHandler_AuditLoginFailed_NilUserIDWithAttemptedEmail verifies :
// failed login must record user_id=nil and capture the attempted email in
// Details for brute-force analysis.
func TestAuthHandler_AuditLoginFailed_NilUserIDWithAttemptedEmail(t *testing.T) {
	mockUser := new(MockUserService)
	mockUser.On("Login", mock.Anything, "intruder@example.com", "wrong-pwd").
		Return(nil, "", "", domain.ErrInvalidCredentials)

	auditLog := &capturingAuditLogger{}
	auditLog.expect(1)

	h, err := NewAuthHandler(mockUser, false, auditLog, testJWTSecret)
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"email":"intruder@example.com","password":"wrong-pwd"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "scanner/0.1")
	req.RemoteAddr = "198.51.100.5:51000"
	w := httptest.NewRecorder()

	h.Login(w, req)
	auditLog.wait()

	require.Equal(t, http.StatusUnauthorized, w.Code)
	entries := auditLog.snapshot()
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, audit.ActionLoginFailed, e.Action)
	assert.Equal(t, "user", e.Resource)
	assert.Nil(t, e.UserID, "failed login MUST record user_id=nil")
	assert.Nil(t, e.BusinessID)
	details := string(e.Details)
	assert.Contains(t, details, "intruder@example.com", "Details must capture attempted_email")
	assert.Contains(t, details, "198.51.100.5")
	assert.Contains(t, details, "scanner/0.1")
	assert.Contains(t, details, "invalid_credentials")
}

// Silence unused-import warnings — context is referenced by the audit.Logger
// interface but otherwise unused in this file.
var _ context.Context = nil
