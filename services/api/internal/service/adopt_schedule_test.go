package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type adoptionStateRepo struct {
	domain.SyncStateRepository
	state domain.SyncState
	saved []byte
	calls int
}

func (r *adoptionStateRepo) ListByBusinessID(context.Context, uuid.UUID) ([]domain.SyncState, error) {
	return []domain.SyncState{r.state}, nil
}
func (r *adoptionStateRepo) AdoptSchedule(_ context.Context, _ uuid.UUID, state domain.SyncState, schedule []byte) error {
	r.calls++
	r.saved = schedule
	if !state.LastCheckedAt.Equal(*r.state.LastCheckedAt) {
		panic("adoption changed the probe time")
	}
	return nil
}

func TestAdoptRemoteHoursUsesHealthyOwnedSnapshotOnly(t *testing.T) {
	for _, scenario := range []string{"valid", "stale", "failed", "disconnected", "foreign", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			business, actor := uuid.New(), uuid.New()
			checked := time.Now().Add(-time.Hour)
			source := map[string]interface{}{}
			for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
				source[day] = map[string]string{"open": "10:00", "close": "21:00"}
			}
			raw, err := json.Marshal(source)
			require.NoError(t, err)
			states := &adoptionStateRepo{state: domain.SyncState{ID: uuid.New(), BusinessID: business, Platform: "yandex_business", ExternalID: "org", LastCheckedAt: &checked, LastRemoteSnapshot: map[string]string{"schedule": string(raw)}}}
			integration := domain.Integration{BusinessID: business, Platform: "yandex_business", ExternalID: "org", Status: "active"}
			if scenario == "disconnected" {
				integration.Status = "token_expired"
			}
			if scenario == "stale" {
				checked = checked.Add(time.Minute)
			}
			if scenario == "failed" {
				states.state.ConsecutiveFailures = 1
			}
			if scenario == "foreign" {
				states.state.BusinessID = uuid.New()
			}
			if scenario == "partial" {
				states.state.LastRemoteSnapshot["schedule"] = `{"monday":"closed"}`
			}
			repo := adoptionIntegrationRepo{integrations: []domain.Integration{integration}}
			service := NewReconciliationService(states, repo, &fakeBusinessRepo{business: &domain.Business{ID: business}}, nil, nil, nil)
			original := *states.state.LastCheckedAt
			if scenario == "stale" {
				original = original.Add(-time.Minute)
			}
			_, err = service.AdoptYandexHours(context.Background(), business, actor, "org", original)
			if scenario == "valid" {
				require.NoError(t, err)
				require.Equal(t, 1, states.calls)
				require.Contains(t, string(states.saved), `"day":"sat"`)
				require.Contains(t, string(states.saved), `"open":"10:00"`)
			} else {
				require.ErrorIs(t, err, domain.ErrSyncStateChanged)
				require.Zero(t, states.calls)
			}
		})
	}
}

type adoptionIntegrationRepo struct {
	domain.IntegrationRepository
	integrations []domain.Integration
}

func (r adoptionIntegrationRepo) ListByBusinessID(context.Context, uuid.UUID) ([]domain.Integration, error) {
	return r.integrations, nil
}
