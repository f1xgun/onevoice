package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestOwnerBriefSuppressesEmptyStatsBeforeGeneration(t *testing.T) {
	router := &recordingRouter{reply: "unused"}
	svc, businessRepo, nc, telemetry, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{})
	require.NoError(t, svc.RunOnce(context.Background()))
	assert.Empty(t, router.requests)
	assert.Zero(t, nc.count())
	assert.Zero(t, businessRepo.updates)
	assert.Empty(t, telemetry.events)
}

func TestOwnerBriefOnlyTargetsPositivePrivateIDs(t *testing.T) {
	for _, id := range []string{"", "0", "-100123", "@channel", "+42", "1e4", "9223372036854775808", "１２３"} {
		t.Run(id, func(t *testing.T) {
			svc, businessRepo, nc, _, businessID := newBriefFixture(t, mondayNineAM(), nil, fakeBriefStats{stats: someStats()})
			svc.integRepo = &fakeBriefIntegRepo{integrations: []domain.Integration{telegramInteg(businessID, map[string]interface{}{"telegram_user_id": id})}}
			require.NoError(t, svc.RunOnce(context.Background()))
			assert.Zero(t, nc.count())
			assert.Zero(t, businessRepo.updates)
		})
	}
	assert.Equal(t, "42", telegramUserIDFromMetadata(map[string]interface{}{"telegram_user_id": " 0042 "}))
}

type unavailableBriefLocker struct{}

func (unavailableBriefLocker) WithOwnerBriefLock(context.Context, func() error) (bool, error) {
	return false, nil
}

func TestOwnerBriefRequiresAnAcquiredPassLock(t *testing.T) {
	svc, _, nc, _, _ := newBriefFixture(t, mondayNineAM(), nil, fakeBriefStats{stats: someStats()})
	svc.integRepo = nil
	svc.locker = nil
	require.ErrorContains(t, svc.RunOnce(context.Background()), "pass lock is not configured")
	svc.locker = unavailableBriefLocker{}
	require.NoError(t, svc.RunOnce(context.Background()))
	assert.Zero(t, nc.count())
}

type selectiveBriefStats struct {
	failedID uuid.UUID
	failure  error
}

func (s selectiveBriefStats) FetchStats(ctx context.Context, id string, _ time.Time) (OwnerBriefStats, error) {
	if id == s.failedID.String() {
		return OwnerBriefStats{}, s.failure
	}
	if _, ok := ctx.Deadline(); !ok {
		return OwnerBriefStats{}, errors.New("missing business deadline")
	}
	return someStats(), nil
}

func TestOwnerBriefReportsPartialFailureAfterHealthyBusinessCompletes(t *testing.T) {
	svc, businesses, nc, _, healthyID := newBriefFixture(t, mondayNineAM(), nil, fakeBriefStats{stats: someStats()})
	failedID := uuid.New()
	businesses.businesses[failedID] = &domain.Business{ID: failedID, Name: "Failed"}
	svc.integRepo = &fakeBriefIntegRepo{integrations: []domain.Integration{telegramInteg(failedID, ownerMeta()), telegramInteg(healthyID, ownerMeta())}}
	failure := errors.New("stats unavailable")
	svc.stats = selectiveBriefStats{failedID: failedID, failure: failure}
	require.ErrorIs(t, svc.RunOnce(context.Background()), failure)
	assert.Equal(t, 1, nc.count())
	assert.Equal(t, 1, businesses.updates)
}
