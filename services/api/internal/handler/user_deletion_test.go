package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeUserDeletionService is an in-memory AccountDeletionServiceAPI double
// with configurable RequestDeletion behavior for the Delete handler tests.
type fakeUserDeletionService struct {
	requestErr    error
	requestCalled bool
	scheduledAt   time.Time
	scheduledErr  error
}

func (f *fakeUserDeletionService) RequestDeletion(_ context.Context, _ uuid.UUID, _, _, _, _ string) error {
	f.requestCalled = true
	return f.requestErr
}

func (f *fakeUserDeletionService) CancelDeletion(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}

func (f *fakeUserDeletionService) GetScheduledDeletionAt(_ context.Context, _ uuid.UUID) (time.Time, error) {
	return f.scheduledAt, f.scheduledErr
}

func newUserDeleteReq(t *testing.T, origin string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/users/me", bytes.NewBufferString(`{"password":"pw"}`))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return ctxWithUserID(r, uuid.New())
}

func TestUserDeletion_Delete_Success204(t *testing.T) {
	svc := &fakeUserDeletionService{}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	w := httptest.NewRecorder()
	h.Delete(w, newUserDeleteReq(t, "http://localhost:3000"))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, svc.requestCalled)
}

func TestUserDeletion_Delete_OriginNotAllowed403(t *testing.T) {
	svc := &fakeUserDeletionService{}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	w := httptest.NewRecorder()
	h.Delete(w, newUserDeleteReq(t, "https://evil.test"))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "origin_not_allowed")
	require.False(t, svc.requestCalled, "deletion must not run on a disallowed Origin")
}

func TestUserDeletion_Delete_MissingOrigin403(t *testing.T) {
	svc := &fakeUserDeletionService{}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	w := httptest.NewRecorder()
	h.Delete(w, newUserDeleteReq(t, ""))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "origin_not_allowed")
	require.False(t, svc.requestCalled, "deletion must not run on a missing Origin")
}

func TestUserDeletion_Delete_InvalidPassword401(t *testing.T) {
	svc := &fakeUserDeletionService{requestErr: domain.ErrInvalidCredentials}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	w := httptest.NewRecorder()
	h.Delete(w, newUserDeleteReq(t, "http://localhost:3000"))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "password_invalid")
}

func TestUserDeletion_Delete_AlreadyPending423(t *testing.T) {
	at := time.Now().Add(30 * 24 * time.Hour)
	svc := &fakeUserDeletionService{requestErr: domain.ErrDeletionAlreadyPending, scheduledAt: at}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	w := httptest.NewRecorder()
	h.Delete(w, newUserDeleteReq(t, "http://localhost:3000"))
	require.Equal(t, http.StatusLocked, w.Code)
	require.Contains(t, w.Body.String(), "account_pending_deletion")
}

func TestUserDeletion_Delete_NotFound404(t *testing.T) {
	svc := &fakeUserDeletionService{requestErr: domain.ErrUserNotFound}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	w := httptest.NewRecorder()
	h.Delete(w, newUserDeleteReq(t, "http://localhost:3000"))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserDeletion_Restore_Success204(t *testing.T) {
	svc := &fakeUserDeletionService{}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	r := httptest.NewRequest(http.MethodPost, "/users/me/restore", http.NoBody)
	r.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.Restore(w, ctxWithUserID(r, uuid.New()))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestUserDeletion_Restore_OriginNotAllowed403(t *testing.T) {
	svc := &fakeUserDeletionService{}
	h := NewUserDeletionHandler(svc, []string{"http://localhost:3000"})
	r := httptest.NewRequest(http.MethodPost, "/users/me/restore", http.NoBody)
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	h.Restore(w, ctxWithUserID(r, uuid.New()))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "origin_not_allowed")
}
