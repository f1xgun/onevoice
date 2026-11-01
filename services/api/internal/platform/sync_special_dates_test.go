package platform

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestSpecialDatesDispatchDoesNotSendWeeklyHours(t *testing.T) {
	id := uuid.New()
	pub := &fakeTaskPublisher{}
	rec := &fakeTaskRecorder{}
	syncer := newYandexOnlySyncer(&fakeIntegrations{list: []domain.Integration{{Platform: a2a.AgentYandexBusiness, Status: domain.IntegrationStatusActive, ExternalID: "123"}}}, rec, pub)
	change := domain.SpecialDateHoursChange{Previous: []domain.SpecialDateHours{{Date: "2026-11-04", Closed: true}}, Current: []domain.SpecialDateHours{}}
	syncer.SyncSpecialDatesContext(context.Background(), &domain.Business{ID: id, Settings: map[string]interface{}{"schedule": "must not be sent"}}, change)
	require.Len(t, pub.calls, 1)
	require.NotContains(t, pub.calls[0].Args, "hours")
	var actual domain.SpecialDateHoursChange
	require.NoError(t, json.Unmarshal([]byte(pub.calls[0].Args["special_dates"].(string)), &actual))
	require.Equal(t, change, actual)
	require.Equal(t, id.String(), pub.calls[0].BusinessID)
	require.Len(t, rec.snapshot(), 1)
	require.Equal(t, "done", rec.snapshot()[0].Status)
}
