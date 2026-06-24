package llm

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultDailySpendCacheTTL bounds how long a per-business daily-spend reading
// is reused before a fresh lookup. The daily-spend gate runs once per LLM
// iteration (up to MaxIterations per turn), so without a cache a single
// tool-using turn fans out that many GetDailySpend round-trips — on the
// orchestrator path each is an HTTP call to the api plus a PG SUM over
// usage_logs. A few seconds of staleness trades a small, bounded overspend
// window (a budget-blown business keeps passing for at most one TTL) for
// removing that per-iteration round-trip; the cap is a soft daily ceiling and
// billing is already accumulated asynchronously, so the reading lags regardless.
const DefaultDailySpendCacheTTL = 10 * time.Second

type dailySpendEntry struct {
	day       string // UTC calendar day (YYYY-MM-DD) the reading is for
	spend     float64
	fetchedAt time.Time
}

// CachedDailySpender wraps a DailySpender with a short-TTL, per-business
// in-process cache. Safe for concurrent use. It holds at most one entry per
// business (keyed by business ID); a day rollover or an expired TTL forces a
// fresh read, so map growth is bounded by the active tenant count. Errors are
// never cached — a failed lookup falls through to the wrapped spender's policy.
type CachedDailySpender struct {
	inner DailySpender
	ttl   time.Duration

	mu    sync.Mutex
	cache map[uuid.UUID]dailySpendEntry
}

// NewCachedDailySpender wraps inner with a short-TTL cache. A non-positive ttl
// falls back to DefaultDailySpendCacheTTL. It returns nil when inner is nil, so
// callers can wrap unconditionally without re-checking (a nil DailySpender means
// "no daily-spend gate configured").
func NewCachedDailySpender(inner DailySpender, ttl time.Duration) DailySpender {
	if inner == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = DefaultDailySpendCacheTTL
	}
	return &CachedDailySpender{
		inner: inner,
		ttl:   ttl,
		cache: make(map[uuid.UUID]dailySpendEntry),
	}
}

// GetDailySpend returns the cached reading for businessID when it is for the
// same UTC day and younger than the TTL; otherwise it reads through to the
// wrapped spender and refreshes the entry. The wrapped read runs WITHOUT the
// lock held, so a slow lookup for one business never blocks another (a rare
// concurrent cold-miss may double-fetch — harmless, both store the same value).
func (c *CachedDailySpender) GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	dayKey := day.UTC().Format("2006-01-02")

	c.mu.Lock()
	if e, ok := c.cache[businessID]; ok && e.day == dayKey && time.Since(e.fetchedAt) < c.ttl {
		spend := e.spend
		c.mu.Unlock()
		return spend, nil
	}
	c.mu.Unlock()

	spend, err := c.inner.GetDailySpend(ctx, businessID, day)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.cache[businessID] = dailySpendEntry{day: dayKey, spend: spend, fetchedAt: time.Now()}
	c.mu.Unlock()

	return spend, nil
}
