package authz_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
)

// countingLoader is a fake MembershipLoader that counts how many times
// LoadMembership and LoadRole are called.
type countingLoader struct {
	mu          sync.Mutex
	memberCalls int
	roleCalls   int
	member      *authz.CachedMember
	role        *authz.CachedRole
	memberErr   error
	roleErr     error
}

func (f *countingLoader) LoadMembership(_ context.Context, _, _ uuid.UUID) (*authz.CachedMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memberCalls++
	return f.member, f.memberErr
}

func (f *countingLoader) LoadRole(_ context.Context, _ uuid.UUID) (*authz.CachedRole, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roleCalls++
	return f.role, f.roleErr
}

func (f *countingLoader) MemberCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.memberCalls
}

func (f *countingLoader) RoleCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.roleCalls
}

// newTestLoader creates a countingLoader with a pre-configured member and role.
func newTestLoader() *countingLoader {
	roleID := uuid.New()
	return &countingLoader{
		member: &authz.CachedMember{
			RoleID:   roleID,
			Status:   "active",
			JoinedAt: time.Now(),
		},
		role: &authz.CachedRole{
			Permissions: []authz.Permission{authz.PermContentRead},
		},
	}
}

// Test 1: NewCache(nil) panics
func TestNewCache_NilLoaderPanics(t *testing.T) {
	require.Panics(t, func() {
		authz.NewCache(nil)
	})
}

// Test 2: GetMembership populates the cache — second call within TTL does not hit loader
func TestCache_GetMembership_CachesOnFirstCall(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)

	bizID, userID := uuid.New(), uuid.New()

	m1, err := cache.GetMembership(context.Background(), bizID, userID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.MemberCallCount(), "first call should hit loader")

	m2, err := cache.GetMembership(context.Background(), bizID, userID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.MemberCallCount(), "second call within TTL should NOT hit loader")
	require.Equal(t, m1.RoleID, m2.RoleID)
}

// Test 3: GetRole populates the role cache — second call within TTL does not hit loader
func TestCache_GetRole_CachesOnFirstCall(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)

	roleID := uuid.New()
	loader.role = &authz.CachedRole{Permissions: []authz.Permission{authz.PermContentCreate}}

	r1, err := cache.GetRole(context.Background(), roleID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.RoleCallCount(), "first call should hit loader")

	r2, err := cache.GetRole(context.Background(), roleID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.RoleCallCount(), "second call within TTL should NOT hit loader")
	require.Equal(t, r1.Permissions, r2.Permissions)
}

// Test 4: InvalidateMember deletes exactly one membership key; next GetMembership re-invokes loader.
// Other members in the same business remain cached.
func TestCache_InvalidateMember_DeletesOneKey(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)

	bizID := uuid.New()
	userID1, userID2 := uuid.New(), uuid.New()

	_, err := cache.GetMembership(context.Background(), bizID, userID1)
	require.NoError(t, err)
	_, err = cache.GetMembership(context.Background(), bizID, userID2)
	require.NoError(t, err)
	require.Equal(t, 2, loader.MemberCallCount())

	cache.InvalidateMember(bizID, userID1)

	_, err = cache.GetMembership(context.Background(), bizID, userID1)
	require.NoError(t, err)
	require.Equal(t, 3, loader.MemberCallCount(), "invalidated user must re-fetch from loader")

	_, err = cache.GetMembership(context.Background(), bizID, userID2)
	require.NoError(t, err)
	require.Equal(t, 3, loader.MemberCallCount(), "non-invalidated user should remain cached")
}

// Test 5: InvalidateRole deletes exactly the role-cache key for r.
// Membership entries holding RoleID=r remain in the membership cache.
func TestCache_InvalidateRole_DeletesOneRoleKey(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)

	bizID := uuid.New()
	roleID := uuid.New()

	_, err := cache.GetRole(context.Background(), roleID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.RoleCallCount())

	cache.InvalidateRole(bizID, roleID)

	_, err = cache.GetRole(context.Background(), roleID)
	require.NoError(t, err)
	require.Equal(t, 2, loader.RoleCallCount(), "invalidated role must re-fetch from loader")
}

// Test 6: Cache evicts oldest entry after 1025th Add (1024-entry contract).
func TestCache_MembershipEviction_At1025(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)
	bizID := uuid.New()

	userIDs := make([]uuid.UUID, 1025)
	for i := 0; i < 1025; i++ {
		userIDs[i] = uuid.New()
		_, err := cache.GetMembership(context.Background(), bizID, userIDs[i])
		require.NoError(t, err)
	}
	countAfterFill := loader.MemberCallCount()
	require.Equal(t, 1025, countAfterFill, "each unique pair should hit loader once")

	_, err := cache.GetMembership(context.Background(), bizID, userIDs[0])
	require.NoError(t, err)
	require.Equal(t, 1026, loader.MemberCallCount(), "oldest entry should be evicted after 1025 inserts")
}

// Test 7: NewCacheForTest(loader, 1s, 1s) honors injected TTL.
func TestNewCacheForTest_TTLInjection(t *testing.T) {
	loader := &countingLoader{
		member: &authz.CachedMember{RoleID: uuid.New(), Status: "active", JoinedAt: time.Now()},
	}
	cache := authz.NewCacheForTest(loader, 1*time.Second, 1*time.Second)

	bizID, userID := uuid.New(), uuid.New()
	_, err := cache.GetMembership(context.Background(), bizID, userID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.MemberCallCount(), "first call hits loader")

	time.Sleep(500 * time.Millisecond)
	_, err = cache.GetMembership(context.Background(), bizID, userID)
	require.NoError(t, err)
	require.Equal(t, 1, loader.MemberCallCount(), "within TTL, no re-load")

	time.Sleep(700 * time.Millisecond)
	_, err = cache.GetMembership(context.Background(), bizID, userID)
	require.NoError(t, err)
	require.Equal(t, 2, loader.MemberCallCount(), "after TTL elapsed, loader re-invoked")
}

// Test 8: NewCache constructs membership LRU capacity 1024, role LRU capacity 256.
// Covered behaviorally: Test 6 for membership (1025 eviction).
// Role LRU 257 eviction:
func TestCache_RoleEviction_At257(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)

	roleIDs := make([]uuid.UUID, 257)
	for i := 0; i < 257; i++ {
		roleIDs[i] = uuid.New()
		_, err := cache.GetRole(context.Background(), roleIDs[i])
		require.NoError(t, err)
	}
	require.Equal(t, 257, loader.RoleCallCount())

	_, err := cache.GetRole(context.Background(), roleIDs[0])
	require.NoError(t, err)
	require.Equal(t, 258, loader.RoleCallCount(), "oldest role entry should be evicted after 257 inserts")
}

// Test 9: Concurrent Get/Invalidate calls do not race.
func TestCache_ConcurrentAccess_NoRace(t *testing.T) {
	loader := newTestLoader()
	cache := authz.NewCache(loader)
	bizID := uuid.New()
	userID := uuid.New()

	_, err := cache.GetMembership(context.Background(), bizID, userID)
	require.NoError(t, err)

	var wg sync.WaitGroup
	const goroutines = 100
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = cache.GetMembership(context.Background(), bizID, userID)
		}()
		go func() {
			defer wg.Done()
			cache.InvalidateMember(bizID, userID)
		}()
	}
	wg.Wait()
}

// Test 10: NewCacheForTest panics when loader == nil.
func TestNewCacheForTest_NilLoaderPanics(t *testing.T) {
	require.Panics(t, func() {
		authz.NewCacheForTest(nil, 1*time.Second, 1*time.Second)
	})
}
