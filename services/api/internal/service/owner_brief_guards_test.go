package service

import (
	"context"
	"testing"

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
