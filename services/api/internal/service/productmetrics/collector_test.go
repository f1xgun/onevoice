package productmetrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

type fakePresence struct {
	calls int
	since time.Time
	stats repository.PresenceStats
	err   error
}

func (f *fakePresence) RecentPresence(_ context.Context, since time.Time) (repository.PresenceStats, error) {
	f.calls++
	f.since = since
	return f.stats, f.err
}

type fakeActivation struct {
	calls int
	since time.Time
	stats repository.ActivationStats
	err   error
}

func (f *fakeActivation) RecentActivation(_ context.Context, since time.Time) (repository.ActivationStats, error) {
	f.calls++
	f.since = since
	return f.stats, f.err
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCollect_QueriesTrailingWindowForBothSources(t *testing.T) {
	presence := &fakePresence{stats: repository.PresenceStats{Updates: 12, ActiveBusinesses: 3}}
	activation := &fakeActivation{stats: repository.ActivationStats{Signups: 20, Activated: 8}}
	c := NewCollector(presence, activation, 0, discard())

	before := time.Now().Add(-northStarWindow)
	c.Collect(context.Background())
	after := time.Now().Add(-northStarWindow)

	require.Equal(t, 1, presence.calls)
	require.Equal(t, 1, activation.calls)
	// `since` anchors to now-7d; assert both land within the call's clock bracket.
	for _, since := range []time.Time{presence.since, activation.since} {
		assert.False(t, since.Before(before.Add(-time.Second)))
		assert.False(t, since.After(after.Add(time.Second)))
	}
}

func TestCollect_OneSourceErrorDoesNotStopTheOther(t *testing.T) {
	presence := &fakePresence{err: errors.New("mongo down")}
	activation := &fakeActivation{stats: repository.ActivationStats{Signups: 5, Activated: 2}}
	c := NewCollector(presence, activation, 0, discard())

	assert.NotPanics(t, func() { c.Collect(context.Background()) })
	assert.Equal(t, 1, presence.calls)
	assert.Equal(t, 1, activation.calls) // activation still ran despite presence failing
}

func TestCollect_NilSourcesAreNoop(t *testing.T) {
	c := NewCollector(nil, nil, 0, discard())
	assert.NotPanics(t, func() { c.Collect(context.Background()) })
}

func TestCollect_PresenceOnly(t *testing.T) {
	presence := &fakePresence{stats: repository.PresenceStats{Updates: 1, ActiveBusinesses: 1}}
	c := NewCollector(presence, nil, 0, discard())
	assert.NotPanics(t, func() { c.Collect(context.Background()) })
	assert.Equal(t, 1, presence.calls)
}

func TestCollect_ActivationOnly(t *testing.T) {
	activation := &fakeActivation{stats: repository.ActivationStats{Signups: 3, Activated: 1}}
	c := NewCollector(nil, activation, 0, discard())
	assert.NotPanics(t, func() { c.Collect(context.Background()) })
	assert.Equal(t, 1, activation.calls)
}

func TestStart_ZeroIntervalRunsOnce(t *testing.T) {
	presence := &fakePresence{stats: repository.PresenceStats{Updates: 1, ActiveBusinesses: 1}}
	c := NewCollector(presence, nil, 0, discard())
	c.Start(context.Background()) // interval<=0 → collect once, return
	assert.Equal(t, 1, presence.calls)
}

func TestStart_TickerStopsOnContextCancel(t *testing.T) {
	presence := &fakePresence{stats: repository.PresenceStats{Updates: 1, ActiveBusinesses: 1}}
	c := NewCollector(presence, nil, time.Hour, discard())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled → immediate one-shot collect, then return
	c.Start(ctx)
	assert.GreaterOrEqual(t, presence.calls, 1)
}
