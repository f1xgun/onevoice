package planresolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type fakeStore struct {
	activeCalls int
	freeCalls   int
	activePlan  Plan
	activeErr   error
	freePlan    Plan
	freeErr     error
}

func (f *fakeStore) ActivePlanForBusiness(_ context.Context, _ uuid.UUID) (Plan, error) {
	f.activeCalls++
	return f.activePlan, f.activeErr
}

func (f *fakeStore) FreePlan(_ context.Context) (Plan, error) {
	f.freeCalls++
	return f.freePlan, f.freeErr
}

// withClock swaps the resolver's clock for a controllable one and returns the
// pointer so the test can advance time.
func withClock(r *Resolver) *time.Time {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	clock := &now
	r.now = func() time.Time { return *clock }
	return clock
}

func TestResolve_ActiveSubscription(t *testing.T) {
	store := &fakeStore{activePlan: Plan{Code: "pro", RateLimitTier: "pro", MonthlyCredits: 2000}}
	r := New(store, time.Minute)

	got := r.Resolve(context.Background(), uuid.New())
	require.Equal(t, "pro", got.Code)
	require.Equal(t, "pro", got.RateLimitTier)
	require.Equal(t, 1, store.activeCalls)
	require.Equal(t, 0, store.freeCalls)
}

func TestResolve_CacheHit_OneStoreCall(t *testing.T) {
	store := &fakeStore{activePlan: Plan{Code: "pro", RateLimitTier: "pro"}}
	r := New(store, time.Minute)
	withClock(r)
	id := uuid.New()

	_ = r.Resolve(context.Background(), id)
	_ = r.Resolve(context.Background(), id)

	require.Equal(t, 1, store.activeCalls, "second Resolve must be served from cache")
}

func TestResolve_TTLExpiry_Refetches(t *testing.T) {
	store := &fakeStore{activePlan: Plan{Code: "pro", RateLimitTier: "pro"}}
	r := New(store, time.Minute)
	clock := withClock(r)
	id := uuid.New()

	_ = r.Resolve(context.Background(), id)
	require.Equal(t, 1, store.activeCalls)

	*clock = clock.Add(time.Minute + time.Second) // past TTL
	_ = r.Resolve(context.Background(), id)
	require.Equal(t, 2, store.activeCalls, "expired entry must refetch")
}

func TestResolve_NoSubscription_FallsBackToFree(t *testing.T) {
	store := &fakeStore{
		activeErr: domain.ErrSubscriptionNotFound,
		freePlan:  Plan{Code: "free", RateLimitTier: "free", MonthlyCredits: 100},
	}
	r := New(store, time.Minute)

	got := r.Resolve(context.Background(), uuid.New())
	require.Equal(t, "free", got.Code)
	require.Equal(t, "free", got.RateLimitTier)
	require.Equal(t, 1, store.freeCalls)
}

func TestResolve_StoreError_FallsBackToFreeNeverHigher(t *testing.T) {
	store := &fakeStore{
		activeErr: errors.New("db down"),
		freePlan:  Plan{Code: "free", RateLimitTier: "free"},
	}
	r := New(store, time.Minute)

	got := r.Resolve(context.Background(), uuid.New())
	require.Equal(t, "free", got.RateLimitTier, "a store error must never resolve to a higher tier")
}

func TestResolve_FreeAlsoFails_HardcodedFree(t *testing.T) {
	store := &fakeStore{
		activeErr: errors.New("db down"),
		freeErr:   errors.New("catalog missing"),
	}
	r := New(store, time.Minute)

	got := r.Resolve(context.Background(), uuid.New())
	require.Equal(t, "free", got.Code)
	require.Equal(t, "free", got.RateLimitTier, "even a total DB fault resolves to Free, never Enterprise")
}

func TestInvalidate_ForcesRefetch(t *testing.T) {
	store := &fakeStore{activePlan: Plan{Code: "pro", RateLimitTier: "pro"}}
	r := New(store, time.Minute)
	withClock(r)
	id := uuid.New()

	_ = r.Resolve(context.Background(), id)
	require.Equal(t, 1, store.activeCalls)

	r.Invalidate(id)
	_ = r.Resolve(context.Background(), id)
	require.Equal(t, 2, store.activeCalls, "Invalidate must drop the cached entry")
}
