package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
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

// fakeAccountDeletion doubles AccountDeletionServiceAPI for the consents
// handler tests; only GetScheduledDeletionAt is exercised here.
type fakeAccountDeletion struct {
	scheduledAt  time.Time
	scheduledErr error
}

func (f *fakeAccountDeletion) RequestDeletion(_ context.Context, _ uuid.UUID, _, _, _, _ string) error {
	return nil
}

func (f *fakeAccountDeletion) CancelDeletion(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}

func (f *fakeAccountDeletion) GetScheduledDeletionAt(_ context.Context, _ uuid.UUID) (time.Time, error) {
	return f.scheduledAt, f.scheduledErr
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
	h := NewConsentsHandler(&fakeConsentsService{}, nil, &fakeConsentsLister{}, testAllowedOrigins)
	body := `{"policies":[{"slug":"tos","version":"v1.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
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
	h := NewConsentsHandler(&fakeConsentsService{}, nil, &fakeConsentsLister{}, testAllowedOrigins)
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
	h := NewConsentsHandler(svc, nil, &fakeConsentsLister{}, testAllowedOrigins)
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
	h := NewConsentsHandler(svc, nil, &fakeConsentsLister{}, testAllowedOrigins)
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

// TestReconsentHandler_OversizedBody_Rejected asserts the body cap on POST
// /auth/consents. All three policies are present and the bulk lives in an
// unknown padding field, so the decoder must scan past the cap to finish the
// object. The service would otherwise run, so removing the MaxBytesReader line
// flips the response to 204 and invokes ReConsent.
func TestReconsentHandler_OversizedBody_Rejected(t *testing.T) {
	svc := &fakeConsentsService{}
	h := NewConsentsHandler(svc, nil, &fakeConsentsLister{}, testAllowedOrigins)
	filler := strings.Repeat("z", maxConsentBodyBytes+1)
	body := `{"policies":[{"slug":"tos","version":"v1.0"},{"slug":"privacy","version":"v1.0"},{"slug":"pdn","version":"v1.0"}],"_pad":"` + filler + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.Reconsent(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.False(t, svc.reconsentCalled, "service must not run when the body exceeds the cap")
}

// TestReconsentHandler_SmallBodyAccepted asserts a normal under-cap body still
// succeeds, so the cap does not reject legitimate requests.
func TestReconsentHandler_SmallBodyAccepted(t *testing.T) {
	svc := &fakeConsentsService{}
	h := NewConsentsHandler(svc, nil, &fakeConsentsLister{}, testAllowedOrigins)
	body := `{"policies":[{"slug":"tos","version":"v1.0"},{"slug":"privacy","version":"v1.0"},{"slug":"pdn","version":"v1.0"}]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/consents", bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.Reconsent(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, svc.reconsentCalled)
}

// TestWithdrawPDNHandler_200OnSuccess asserts the happy path returns 200
// with the real scheduled deletion date in the response envelope.
func TestWithdrawPDNHandler_200OnSuccess(t *testing.T) {
	scheduled := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	svc := &fakeConsentsService{}
	del := &fakeAccountDeletion{scheduledAt: scheduled}
	h := NewConsentsHandler(svc, del, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.WithdrawPDN(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, svc.withdrawCalled)
	var resp openapi.PendingDeletionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "account_pending_deletion", string(resp.Code))
	require.Equal(t, scheduled.Format(time.RFC3339), resp.DeletionDate)
	require.NotEqual(t, "—", resp.DeletionDate)
	require.Equal(t, "/settings/account", resp.RestoreUrl)
}

// TestWithdrawPDNHandler_200_EmptyDateWhenNoDeletionService asserts the
// envelope still renders when the deletion service can't resolve a date.
func TestWithdrawPDNHandler_200_EmptyDateWhenNoDeletionService(t *testing.T) {
	svc := &fakeConsentsService{}
	h := NewConsentsHandler(svc, nil, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.WithdrawPDN(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp openapi.PendingDeletionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, resp.DeletionDate)
}

// TestWithdrawPDNHandler_423WhenAlreadyPending asserts that
// ErrDeletionAlreadyPending → 423 account_pending_deletion envelope
// carrying the real scheduled deletion date.
func TestWithdrawPDNHandler_423WhenAlreadyPending(t *testing.T) {
	scheduled := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	svc := &fakeConsentsService{withdrawErr: domain.ErrDeletionAlreadyPending}
	del := &fakeAccountDeletion{scheduledAt: scheduled}
	h := NewConsentsHandler(svc, del, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	req = ctxWithUserID(req, uuid.New())
	w := httptest.NewRecorder()
	h.WithdrawPDN(w, req)
	require.Equal(t, http.StatusLocked, w.Code)
	var resp openapi.PendingDeletionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "account_pending_deletion", string(resp.Code))
	require.Equal(t, scheduled.Format(time.RFC3339), resp.DeletionDate)
	require.Equal(t, "/settings/account", resp.RestoreUrl)
}

// TestWithdrawPDNHandler_403_OriginNotAllowed asserts CSRF guard fires.
func TestWithdrawPDNHandler_403_OriginNotAllowed(t *testing.T) {
	h := NewConsentsHandler(&fakeConsentsService{}, nil, &fakeConsentsLister{}, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodPost, "/users/me/consents/pdn/withdraw", http.NoBody)
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
	h := NewConsentsHandler(&fakeConsentsService{}, nil, lister, testAllowedOrigins)
	req := httptest.NewRequest(http.MethodGet, "/users/me/consents", http.NoBody)
	req = ctxWithUserID(req, userID)
	w := httptest.NewRecorder()
	h.ListMine(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp listConsentsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Consents, 3)
}
