package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// mockPasswordResetService satisfies handler.PasswordResetServiceAPI for
// the handler-level tests. We don't pull in testify/mock to keep this
// double trivially auditable.
type mockPasswordResetService struct {
	requestCalls       int
	requestLastEmail   string
	requestLastIP      string
	requestLastUA      string
	confirmCalls       int
	confirmLastToken   string
	confirmLastNewPass string
	confirmErr         error
}

func (m *mockPasswordResetService) RequestReset(_ context.Context, emailAddr, clientIP, userAgent string) error {
	m.requestCalls++
	m.requestLastEmail = emailAddr
	m.requestLastIP = clientIP
	m.requestLastUA = userAgent
	return nil // RequestReset always returns nil (PITFALLS §1.1)
}

func (m *mockPasswordResetService) ConfirmReset(_ context.Context, plaintextToken, newPassword, _, _ string) error {
	m.confirmCalls++
	m.confirmLastToken = plaintextToken
	m.confirmLastNewPass = newPassword
	return m.confirmErr
}

// newTestAuthHandler returns an AuthHandler with the password-reset
// service double pre-injected. Other dependencies are zero values /
// noops — these tests only exercise the password-reset routes.
func newTestAuthHandler(t *testing.T, prs PasswordResetServiceAPI) *AuthHandler {
	t.Helper()
	h, err := NewAuthHandler(&MockUserService{}, false, audit.Nop(), testJWTSecret)
	require.NoError(t, err)
	if prs != nil {
		h.SetPasswordResetService(prs)
	}
	return h
}

// --- TestRequestPasswordReset_* -----------------------------------------

func TestRequestPasswordReset_UnknownEmail_Returns204(t *testing.T) {
	prs := &mockPasswordResetService{}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(RequestPasswordResetRequest{Email: "nobody@example.com"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/request", bytes.NewBuffer(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RequestPasswordReset(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, prs.requestCalls)
	require.Equal(t, "nobody@example.com", prs.requestLastEmail)
}

func TestRequestPasswordReset_KnownEmail_Returns204(t *testing.T) {
	prs := &mockPasswordResetService{}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(RequestPasswordResetRequest{Email: "alice@example.com"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/request", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.RequestPasswordReset(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, prs.requestCalls)
}

func TestRequestPasswordReset_MalformedJSON_Returns400(t *testing.T) {
	h := newTestAuthHandler(t, &mockPasswordResetService{})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/request", bytes.NewBufferString("{not-json"))
	w := httptest.NewRecorder()

	h.RequestPasswordReset(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRequestPasswordReset_InvalidEmail_Returns400(t *testing.T) {
	prs := &mockPasswordResetService{}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(RequestPasswordResetRequest{Email: "not-an-email"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/request", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.RequestPasswordReset(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 0, prs.requestCalls, "service must not be invoked on invalid email")
}

// --- TestConfirmPasswordReset_* -----------------------------------------

func TestConfirmPasswordReset_Valid_Returns204(t *testing.T) {
	prs := &mockPasswordResetService{}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(ConfirmPasswordResetRequest{Token: "valid-token-abc", NewPassword: "newpassword456"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.ConfirmPasswordReset(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, prs.confirmCalls)
	require.Equal(t, "valid-token-abc", prs.confirmLastToken)
}

func TestConfirmPasswordReset_InvalidToken_Returns400_CodeResetTokenInvalid(t *testing.T) {
	prs := &mockPasswordResetService{confirmErr: service.ErrResetTokenInvalid}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(ConfirmPasswordResetRequest{Token: "garbage", NewPassword: "newpassword456"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.ConfirmPasswordReset(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "reset_token_invalid", resp["code"])
}

func TestConfirmPasswordReset_DomainExpired_Returns400_CodeResetTokenExpired(t *testing.T) {
	prs := &mockPasswordResetService{confirmErr: domain.ErrResetTokenExpired}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(ConfirmPasswordResetRequest{Token: "expired", NewPassword: "newpassword456"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.ConfirmPasswordReset(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "reset_token_expired", resp["code"])
}

func TestConfirmPasswordReset_ShortPassword_Returns400_CodePasswordTooWeak(t *testing.T) {
	// Handler-level validation tag (min=8) catches this BEFORE the
	// service runs — so we never see the code; we see the validator's
	// generic 400. The service-level ErrPasswordTooWeak path is
	// exercised when a request bypasses validation (e.g. integration
	// tests with a tampered request) — that branch is covered below.
	prs := &mockPasswordResetService{}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(ConfirmPasswordResetRequest{Token: "tok", NewPassword: "short"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.ConfirmPasswordReset(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, 0, prs.confirmCalls, "validator caught the short password before service ran")
}

// TestConfirmPasswordReset_ServicePasswordTooWeak_Returns400_CodePasswordTooWeak
// exercises the service-level ErrPasswordTooWeak error-mapping path —
// the case where validation has been bypassed (e.g. an internal caller
// constructed a request manually).
func TestConfirmPasswordReset_ServicePasswordTooWeak_Returns400_CodePasswordTooWeak(t *testing.T) {
	prs := &mockPasswordResetService{confirmErr: service.ErrPasswordTooWeak}
	h := newTestAuthHandler(t, prs)
	body, _ := json.Marshal(ConfirmPasswordResetRequest{Token: "tok", NewPassword: "longenoughpassword"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.ConfirmPasswordReset(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "password_too_weak", resp["code"])
}

func TestConfirmPasswordReset_MalformedJSON_Returns400(t *testing.T) {
	h := newTestAuthHandler(t, &mockPasswordResetService{})
	r := httptest.NewRequest("POST", "/api/v1/auth/password-reset/confirm", bytes.NewBufferString("{broken"))
	w := httptest.NewRecorder()

	h.ConfirmPasswordReset(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
