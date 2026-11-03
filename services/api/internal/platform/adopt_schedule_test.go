package platform

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestScheduleFromYandexSnapshotRoundTrip(t *testing.T) {
	days := make([]map[string]interface{}, 0, len(dayOrder))
	for _, key := range dayOrder {
		days = append(days, map[string]interface{}{"day": key, "open": "08:00", "close": "21:00", "closed": key == "sun"})
	}
	raw := scheduleToYandexJSON(map[string]interface{}{"schedule": days})
	converted, err := ScheduleFromYandexSnapshot(raw)
	require.NoError(t, err)
	var schedule interface{}
	require.NoError(t, json.Unmarshal(converted, &schedule))
	require.Equal(t, raw, scheduleToYandexJSON(map[string]interface{}{"schedule": schedule}))
	for _, invalid := range []string{"{}", "not json", `{"monday":"closed"}`} {
		_, err := ScheduleFromYandexSnapshot(invalid)
		require.Error(t, err)
	}
}

type scheduleCapabilityProbe struct{ titles, schedules int }

func (p *scheduleCapabilityProbe) SyncTitle(context.Context, *domain.Business, domain.Integration) error {
	p.titles++
	return nil
}
func (p *scheduleCapabilityProbe) SyncSchedule(context.Context, *domain.Business, domain.Integration) error {
	p.schedules++
	return nil
}

func TestScheduleRepairDoesNotDispatchOtherCapabilities(t *testing.T) {
	probe := &scheduleCapabilityProbe{}
	integrations := &fakeIntegrations{list: []domain.Integration{{Platform: "yandex_business", Status: "active"}}}
	tasks := &fakeTaskRecorder{}
	syncer := NewSyncer(integrations, tasks, nil, map[string]any{"yandex_business": probe})
	business := &domain.Business{ID: uuid.New(), Settings: map[string]interface{}{"schedule": []map[string]interface{}{{"day": "mon", "open": "08:00", "close": "21:00", "closed": false}}}}
	syncer.SyncScheduleContext(context.Background(), business)
	require.Equal(t, 1, probe.schedules)
	require.Zero(t, probe.titles)
	syncer.SyncBusinessContext(context.Background(), business)
	require.Equal(t, 2, probe.schedules)
	require.Equal(t, 1, probe.titles, "ordinary profile sync keeps its existing behavior")
}

func TestScheduleQueueDoesNotAcknowledgeUnavailableOrFullQueue(t *testing.T) {
	runner := NewSyncRunner(nil, 1)
	business := &domain.Business{ID: uuid.New()}
	require.ErrorIs(t, runner.SyncSchedule(business), domain.ErrServiceUnavailable)
	runner.ctx = context.Background()
	require.NoError(t, runner.SyncSchedule(business))
	require.ErrorIs(t, runner.SyncSchedule(business), domain.ErrServiceUnavailable)
	require.True(t, (<-runner.jobs).scheduleOnly)
}
