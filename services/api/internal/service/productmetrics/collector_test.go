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

type fakeSource struct {
	calls int
	since time.Time
	stats repository.PresenceStats
	err   error
}

func (f *fakeSource) RecentPresence(_ context.Context, since time.Time) (repository.PresenceStats, error) {
	f.calls++
	f.since = since
	return f.stats, f.err
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCollect_QueriesTrailingWindow(t *testing.T) {
	src := &fakeSource{stats: repository.PresenceStats{Updates: 12, ActiveBusinesses: 3}}
	c := NewCollector(src, 0, discard())

	before := time.Now().Add(-northStarWindow)
	c.Collect(context.Background())
	after := time.Now().Add(-northStarWindow)

	require.Equal(t, 1, src.calls)
	// `since` anchors to now-7d; assert it lands within the call's clock bracket.
	assert.False(t, src.since.Before(before.Add(-time.Second)))
	assert.False(t, src.since.After(after.Add(time.Second)))
}

func TestCollect_ErrorDoesNotPanic(t *testing.T) {
	src := &fakeSource{err: errors.New("mongo down")}
	c := NewCollector(src, 0, discard())
	assert.NotPanics(t, func() { c.Collect(context.Background()) })
	assert.Equal(t, 1, src.calls)
}

func TestCollect_NilSourceIsNoop(t *testing.T) {
	c := NewCollector(nil, 0, discard())
	assert.NotPanics(t, func() { c.Collect(context.Background()) })
}

func TestStart_ZeroIntervalRunsOnce(t *testing.T) {
	src := &fakeSource{stats: repository.PresenceStats{Updates: 1, ActiveBusinesses: 1}}
	c := NewCollector(src, 0, discard())
	c.Start(context.Background()) // interval<=0 → collect once, return
	assert.Equal(t, 1, src.calls)
}

func TestStart_TickerStopsOnContextCancel(t *testing.T) {
	src := &fakeSource{stats: repository.PresenceStats{Updates: 1, ActiveBusinesses: 1}}
	c := NewCollector(src, time.Hour, discard())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled → immediate one-shot collect, then return
	c.Start(ctx)
	assert.GreaterOrEqual(t, src.calls, 1)
}
