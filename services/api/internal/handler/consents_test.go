package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// fakeConsentsService doubles ConsentsServiceAPI for the handler tests.
type fakeConsentsService struct {
	mu              sync.Mutex
	reconsentErr    error
	withdrawErr     error
	reconsentCalled bool
	withdrawCalled  bool
	lastPolicies    []service.PolicyAccepted
}

func (f *fakeConsentsService) ReConsent(_ context.Context, _ uuid.UUID, _, _ string, policies []service.PolicyAccepted) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconsentCalled = true
	f.lastPolicies = policies
	return f.reconsentErr
}

func (f *fakeConsentsService) WithdrawPDN(_ context.Context, _ uuid.UUID, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withdrawCalled = true
	return f.withdrawErr
}

// fakeConsentsLister doubles ConsentsListerAPI for ListMine tests.
type fakeConsentsLister struct {
	rows []repository.Consent
	err  error
}

func (f *fakeConsentsLister) ListByUser(_ context.Context, _ uuid.UUID) ([]repository.Consent, error) {
	return f.rows, f.err
}

// originAllowed setup helper for the test allow-list.
var testAllowedOrigins = []string{"http://localhost:3000"}

// ctxWithUserID injects a userID into the request context the way the
// middleware does in production. Mirrors auth_test.go pattern.
func ctxWithUserID(r *http.Request, userID uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, userID))
}

// TestReconsentHandler_403_OriginNotAllowed asserts that a missing /
// mismatched Origin header returns 403 origin_not_allowed.
func TestReconsentHandler_403_OriginNotAllowed(t *testing.T) {
	h := NewConsentsHandler(&fakeConsentsService{}, &fakeConsentsLister{}, testAllowedOrigins)
	body := `{"policies":[{"slug":"tos","version":"v1.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
	// No Origin header set.
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.Reconsent(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"origin_not_allowed"`)
}

// TestReconsentHandler_400_ConsentRequired_OnMissing asserts that a
// body with only two of three policies returns 400 with the missing
// slug in `missing`.
func TestReconsentHandler_400_ConsentRequired_OnMissing(t *testing.T) {
	h := NewConsentsHandler(&fakeConsentsService{}, &fakeConsentsLister{}, testAllowedOrigins)
	body := `{"policies":[{"slug":"tos","version":"v1.0"},{"slug":"privacy","version":"v1.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.Reconsent(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp consentRequiredBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "consent_required", string(resp.Code))
	require.Equal(t, []string{"pdn"}, resp.Missing)
}

// TestReconsentHandler_409_VersionMismatch asserts that when the
// service returns ErrConsentVersionMismatch the handler returns 409
// with a version_mismatch + currentVersion body.
func TestReconsentHandler_409_VersionMismatch(t *testing.T) {
	svc := &fakeConsentsService{reconsentErr: domain.ErrConsentVersionMismatch}
	h := NewConsentsHandler(svc, &fakeConsentsLister{}, testAllowedOrigins)
	body := `{"policies":[{"slug":"tos","version":"v0.9"},{"slug":"privacy","version":"v0.9"},{"slug":"pdn","version":"v0.9"}]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.Reconsent(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
	var resp versionMismatchBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "version_mismatch", string(resp.Code))
	require.NotEmpty(t, resp.CurrentVersion)
}

// TestReconsentHandler_204_OnSuccess asserts happy path returns 204.
func TestReconsentHandler_204_OnSuccess(t *testing.T) {
	svc := &fakeConsentsService{}
	h := NewConsentsHandler(svc, &fakeConsentsLister{}, testAllowedOrigins)
	body := `{"policies":[{"slug":"tos","version":"v1.0"},{"slug":"privacy","version":"v1.0"},{"slug":"pdn","version":"v1.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.Reconsent(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, svc.reconsentCalled)
	require.Len(t, svc.lastPolicies, 3)
}

// TestWithdrawPDNHandler_204OnSuccess asserts happy path returns 204.
func TestWithdrawPDNHandler_204OnSuccess(t *testing.T) {
	svc := &fakeConsentsService{}
	h := NewConsentsHandler(svc, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.WithdrawPDN(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, svc.withdrawCalled)
}

// TestWithdrawPDNHandler_423WhenAlreadyPending asserts that
// ErrDeletionAlreadyPending → 423 account_pending_deletion envelope.
func TestWithdrawPDNHandler_423WhenAlreadyPending(t *testing.T) {
	svc := &fakeConsentsService{withdrawErr: domain.ErrDeletionAlreadyPending}
	h := NewConsentsHandler(svc, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.WithdrawPDN(w, req)
	require.Equal(t, http.StatusLocked, w.Code)
	require.Contains(t, w.Body.String(), `"account_pending_deletion"`)
	require.Contains(t, w.Body.String(), `"/settings/account"`)
}

// TestWithdrawPDNHandler_403_OriginNotAllowed asserts CSRF guard fires.
func TestWithdrawPDNHandler_403_OriginNotAllowed(t *testing.T) {
	h := NewConsentsHandler(&fakeConsentsService{}, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
	// No Origin header.
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.WithdrawPDN(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

// TestListMine_Returns200WithRows asserts the handler maps repo rows to
// the JSON envelope shape.
func TestListMine_Returns200WithRows(t *testing.T) {
	userID := uuid.New()
	now := time.Now().UTC()
	lister := &fakeConsentsLister{rows: []repository.Consent{
		{UserID: userID, Purpose: "tos", PolicyVersion: "v1.0", PolicySHA256: "sha-t", AcceptedAt: now},
		{UserID: userID, Purpose: "privacy", PolicyVersion: "v1.0", PolicySHA256: "sha-p", AcceptedAt: now},
		{UserID: userID, Purpose: "pdn", PolicyVersion: "v1.0", PolicySHA256: "sha-d", AcceptedAt: now},
	}}
	h := NewConsentsHandler(&fakeConsentsService{}, lister, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodGet, "/users/me/consents", http.NoBody)
	req = ctxWithUserID(req, userID)
	w := httptest.NewRecorder()
	h.ListMine(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp listConsentsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Consents, 3)
}
