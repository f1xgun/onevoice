package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeBusinessDeletionService is an in-memory BusinessDeletionServiceAPI double.
type fakeBusinessDeletionService struct {
	requestErr  error
	cancelErr   error
	scheduledAt time.Time
}

func (f *fakeBusinessDeletionService) RequestDeletion(_ context.Context, _, _ uuid.UUID, _, _ string) error {
	return f.requestErr
}

func (f *fakeBusinessDeletionService) CancelDeletion(_ context.Context, _, _ uuid.UUID, _, _ string) error {
	return f.cancelErr
}

func (f *fakeBusinessDeletionService) GetScheduledDeletionAt(_ context.Context, _ uuid.UUID) (time.Time, error) {
	return f.scheduledAt, nil
}

func newBizDeletionReq(t *testing.T, method, path, origin string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, http.NoBody)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	bc := bizPerms(uuid.New(), uuid.New())
	return withBizCtx(r, bc)
}

// ----- Delete -----

func TestBusinessDeletion_Delete_Success204(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Delete(w, newBizDeletionReq(t, http.MethodDelete, "/businesses/x", ""))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestBusinessDeletion_Delete_NotOwner403(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{requestErr: domain.ErrNotBusinessOwner}, nil)
	w := httptest.NewRecorder()
	h.Delete(w, newBizDeletionReq(t, http.MethodDelete, "/businesses/x", ""))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "not_organization_owner")
}

func TestBusinessDeletion_Delete_AlreadyPending423(t *testing.T) {
	at := time.Now().Add(30 * 24 * time.Hour)
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{
		requestErr:  domain.ErrBusinessDeletionAlreadyPending,
		scheduledAt: at,
	}, nil)
	w := httptest.NewRecorder()
	h.Delete(w, newBizDeletionReq(t, http.MethodDelete, "/businesses/x", ""))
	require.Equal(t, http.StatusLocked, w.Code)
	require.Contains(t, w.Body.String(), "business_pending_deletion")
	require.Contains(t, w.Body.String(), "/business")
}

func TestBusinessDeletion_Delete_NotFound404(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{requestErr: domain.ErrBusinessNotFound}, nil)
	w := httptest.NewRecorder()
	h.Delete(w, newBizDeletionReq(t, http.MethodDelete, "/businesses/x", ""))
	require.Equal(t, http.StatusNotFound, w.Code)
}

// ----- Restore -----

func TestBusinessDeletion_Restore_Success204(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Restore(w, newBizDeletionReq(t, http.MethodPost, "/businesses/x/restore", "https://app.test"))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestBusinessDeletion_Restore_OriginNotAllowed403(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Restore(w, newBizDeletionReq(t, http.MethodPost, "/businesses/x/restore", "https://evil.test"))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "origin_not_allowed")
}

func TestBusinessDeletion_Restore_MissingOrigin403(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Restore(w, newBizDeletionReq(t, http.MethodPost, "/businesses/x/restore", ""))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestBusinessDeletion_Restore_NotOwner403(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{cancelErr: domain.ErrNotBusinessOwner}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Restore(w, newBizDeletionReq(t, http.MethodPost, "/businesses/x/restore", "https://app.test"))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "not_organization_owner")
}

func TestBusinessDeletion_Restore_NoPending404(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{cancelErr: domain.ErrNoBusinessDeletionPending}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Restore(w, newBizDeletionReq(t, http.MethodPost, "/businesses/x/restore", "https://app.test"))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "no_deletion_pending")
}

func TestBusinessDeletion_Restore_TooOld410(t *testing.T) {
	h := NewBusinessDeletionHandler(&fakeBusinessDeletionService{cancelErr: domain.ErrBusinessAlreadyPurged}, []string{"https://app.test"})
	w := httptest.NewRecorder()
	h.Restore(w, newBizDeletionReq(t, http.MethodPost, "/businesses/x/restore", "https://app.test"))
	require.Equal(t, http.StatusGone, w.Code)
	require.Contains(t, w.Body.String(), "deletion_too_old")
}
