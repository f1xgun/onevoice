package llm_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// countingSpender is a concurrency-safe DailySpender double that counts calls
// and returns a settable (spend, err). Used by the cache tests, including the
// -race concurrency case where the shared fakeDailySpender would itself race.
type countingSpender struct {
	calls atomic.Int64

	mu    sync.Mutex
	spend float64
	err   error
}

func (s *countingSpender) set(spend float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spend, s.err = spend, err
}

func (s *countingSpender) GetDailySpend(_ context.Context, _ uuid.UUID, _ time.Time) (float64, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spend, s.err
}

func (s *countingSpender) count() int64 { return s.calls.Load() }

func TestNewCachedDailySpender_NilInnerReturnsNil(t *testing.T) {
	require.Nil(t, llm.NewCachedDailySpender(nil, 0))
}

func TestCachedDailySpender_HitWithinTTL(t *testing.T) {
	inner := &countingSpender{spend: 4.2}
	// ttl=0 → DefaultDailySpendCacheTTL (10s), so all three calls are warm.
	c := llm.NewCachedDailySpender(inner, 0)
	day := time.Now().UTC()
	biz := uuid.New()

	for i := 0; i < 3; i++ {
		got, err := c.GetDailySpend(context.Background(), biz, day)
		require.NoError(t, err)
		require.InDelta(t, 4.2, got, 1e-9)
	}
	require.Equal(t, int64(1), inner.count(), "second/third reads must come from cache")
}

func TestCachedDailySpender_RefetchAfterTTL(t *testing.T) {
	inner := &countingSpender{spend: 1.0}
	c := llm.NewCachedDailySpender(inner, 20*time.Millisecond)
	day := time.Now().UTC()
	biz := uuid.New()

	_, err := c.GetDailySpend(context.Background(), biz, day)
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)
	_, err = c.GetDailySpend(context.Background(), biz, day)
	require.NoError(t, err)

	require.Equal(t, int64(2), inner.count(), "an expired entry must read through")
}

func TestCachedDailySpender_RefetchOnDayRollover(t *testing.T) {
	inner := &countingSpender{spend: 1.0}
	c := llm.NewCachedDailySpender(inner, time.Hour) // long TTL, so only the day change forces a refetch
	biz := uuid.New()
	day1 := time.Date(2026, 6, 23, 23, 0, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Hour) // next UTC calendar day

	_, _ = c.GetDailySpend(context.Background(), biz, day1)
	_, _ = c.GetDailySpend(context.Background(), biz, day1) // cached
	_, _ = c.GetDailySpend(context.Background(), biz, day2) // new day → refetch

	require.Equal(t, int64(2), inner.count())
}

func TestCachedDailySpender_PerBusinessIsolation(t *testing.T) {
	inner := &countingSpender{spend: 2.0}
	c := llm.NewCachedDailySpender(inner, time.Hour)
	day := time.Now().UTC()
	bizA, bizB := uuid.New(), uuid.New()

	_, _ = c.GetDailySpend(context.Background(), bizA, day)
	_, _ = c.GetDailySpend(context.Background(), bizA, day) // cached
	_, _ = c.GetDailySpend(context.Background(), bizB, day) // distinct business → its own miss
	_, _ = c.GetDailySpend(context.Background(), bizB, day) // cached

	require.Equal(t, int64(2), inner.count(), "each business caches independently")
}

func TestCachedDailySpender_ErrorNotCached(t *testing.T) {
	inner := &countingSpender{}
	inner.set(0, errors.New("boom"))
	c := llm.NewCachedDailySpender(inner, time.Hour)
	day := time.Now().UTC()
	biz := uuid.New()

	_, err := c.GetDailySpend(context.Background(), biz, day)
	require.Error(t, err)

	inner.set(7.5, nil)
	got, err := c.GetDailySpend(context.Background(), biz, day)
	require.NoError(t, err)
	require.InDelta(t, 7.5, got, 1e-9)
	require.Equal(t, int64(2), inner.count(), "a failed lookup must not be cached")
}

func TestCachedDailySpender_ConcurrentSafe(t *testing.T) {
	inner := &countingSpender{spend: 3.0}
	c := llm.NewCachedDailySpender(inner, time.Hour)
	day := time.Now().UTC()
	biz := uuid.New()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := c.GetDailySpend(context.Background(), biz, day)
			require.NoError(t, err)
			require.InDelta(t, 3.0, got, 1e-9)
		}()
	}
	wg.Wait()
	// A cold-miss race may double-fetch, but the warm cache must collapse the
	// vast majority — far fewer than 64 inner calls.
	require.Less(t, inner.count(), int64(64))
}
