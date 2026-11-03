package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type testHoursAdopter struct {
	driftLister
	calls           int
	business, actor uuid.UUID
	err             error
}

func (a *testHoursAdopter) AdoptYandexHours(_ context.Context, business, actor uuid.UUID, _ string, _ time.Time) (*domain.Business, error) {
	a.calls++
	a.business = business
	a.actor = actor
	return &domain.Business{ID: business}, a.err
}

type testHoursWriter struct{ full, hours int }

func (w *testHoursWriter) SyncBusiness(*domain.Business)       { w.full++ }
func (w *testHoursWriter) SyncSchedule(*domain.Business) error { w.hours++; return nil }

func TestHoursActionsRequireBusinessUpdate(t *testing.T) {
	h := &IntegrationHandler{}
	for _, action := range []func(http.ResponseWriter, *http.Request){h.AdoptHours, h.RestoreHours} {
		req := httptest.NewRequest(http.MethodPost, "/hours", http.NoBody)
		req = withBizCtx(req, authz.BusinessContext{BusinessID: uuid.New(), UserID: uuid.New(), Permissions: []authz.Permission{authz.PermBusinessRead}})
		response := httptest.NewRecorder()
		action(response, req)
		require.Equal(t, http.StatusForbidden, response.Code)
	}
}

func TestAdoptHoursPassesOwnedContextAndNeverDispatches(t *testing.T) {
	business, actor := uuid.New(), uuid.New()
	adopter, writer := &testHoursAdopter{}, &testHoursWriter{}
	h := &IntegrationHandler{reconciler: adopter, syncTrigger: writer}
	for _, conflict := range []bool{false, true} {
		if conflict {
			adopter.err = domain.ErrSyncStateChanged
		}
		req := httptest.NewRequest(http.MethodPost, "/hours", strings.NewReader(`{"externalId":"org","lastCheckedAt":"2026-11-03T07:00:00+03:00"}`))
		req = withBizCtx(req, bizPerms(business, actor))
		response := httptest.NewRecorder()
		h.AdoptHours(response, req)
		if conflict {
			require.Equal(t, http.StatusConflict, response.Code)
		} else {
			require.Equal(t, http.StatusOK, response.Code)
		}
	}
	require.Equal(t, business, adopter.business)
	require.Equal(t, actor, adopter.actor)
	require.Zero(t, writer.full)
	require.Zero(t, writer.hours)
}

func TestRestoreHoursUsesOnlyScheduleCapability(t *testing.T) {
	business, actor := uuid.New(), uuid.New()
	service := new(MockBusinessService)
	service.On("GetByID", mock.Anything, business).Return(&domain.Business{ID: business}, nil)
	writer := &testHoursWriter{}
	h := &IntegrationHandler{businessService: service, syncTrigger: writer}
	req := withBizCtx(httptest.NewRequest(http.MethodPost, "/hours", http.NoBody), bizPerms(business, actor))
	response := httptest.NewRecorder()
	h.RestoreHours(response, req)
	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, 1, writer.hours)
	require.Zero(t, writer.full)
}
