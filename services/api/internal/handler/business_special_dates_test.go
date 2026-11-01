package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type specialDateCapture struct {
	generic bool
	change  *domain.SpecialDateHoursChange
}

func (s *specialDateCapture) SyncBusiness(*domain.Business) { s.generic = true }
func (s *specialDateCapture) SyncSpecialDates(_ *domain.Business, change domain.SpecialDateHoursChange) {
	s.change = &change
}

func TestExplicitSpecialDatesSavePreservesWeeklyHours(t *testing.T) {
	testBusinessID, testUserID := uuid.New(), uuid.New()
	svc := new(MockBusinessService)
	old := &domain.Business{ID: testBusinessID, Settings: map[string]interface{}{"specialDates": []interface{}{map[string]interface{}{"date": "2026-11-04", "closed": true}}}}
	svc.On("GetByID", mock.Anything, testBusinessID).Return(old, nil)
	svc.On("UpdateSettingsKeys", mock.Anything, testBusinessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
		_, weekly := keys["schedule"]
		return !weekly && keys["specialDates"] != nil
	}), mock.Anything).Return(old, nil)
	syncer := &specialDateCapture{}
	h, err := NewBusinessHandler(svc, syncer, nil)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{"specialDates":[]}`))
	req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
	w := httptest.NewRecorder()
	h.UpdateSchedule(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.False(t, syncer.generic)
	require.NotNil(t, syncer.change)
	require.Equal(t, []domain.SpecialDateHours{{Date: "2026-11-04", Closed: true}}, syncer.change.Previous)
	require.Empty(t, syncer.change.Current)
	svc.AssertExpectations(t)
}

func TestInvalidSpecialDatesAreRejectedBeforePersisting(t *testing.T) {
	testBusinessID, testUserID := uuid.New(), uuid.New()
	svc := new(MockBusinessService)
	h, err := NewBusinessHandler(svc, nil, nil)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{"specialDates":[{"date":"2026-02-30","closed":true}]}`))
	req = withBizCtx(req, bizPerms(testBusinessID, testUserID))
	w := httptest.NewRecorder()
	h.UpdateSchedule(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	svc.AssertNotCalled(t, "UpdateSettingsKeys")
}

func TestEmptyScheduleRequestDoesNotWriteOrSync(t *testing.T) {
	id, userID := uuid.New(), uuid.New()
	svc := new(MockBusinessService)
	syncer := &specialDateCapture{}
	h, err := NewBusinessHandler(svc, syncer, nil)
	require.NoError(t, err)
	req := withBizCtx(httptest.NewRequest(http.MethodPut, "/schedule", bytes.NewBufferString(`{}`)), bizPerms(id, userID))
	w := httptest.NewRecorder()
	h.UpdateSchedule(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, svc.Calls)
	require.False(t, syncer.generic)
	require.Nil(t, syncer.change)
}
