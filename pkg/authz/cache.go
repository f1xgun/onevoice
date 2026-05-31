package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
)

const (
	membershipCacheSize = 1024
	roleCacheSize       = 256
	cacheTTL            = 30 * time.Second
	// authzCacheBucketCount is the number of internal LRU buckets used by
	// NewCacheForTest (golang-lru shards the cache for concurrent access).
	authzCacheBucketCount = 16
)

type cacheKey struct {
	BusinessID uuid.UUID
	UserID     uuid.UUID
}

// Cache is the two-level RBAC cache.
// Membership LRU: (businessID, userID) -> CachedMember (RoleID + Status + JoinedAt). 1024 entries, 30s TTL.
// Role LRU: roleID -> CachedRole (Permissions). 256 entries, 30s TTL.
// InvalidateMember and InvalidateRole are O(1) deletes (no fanout).
//
// expirable.LRU uses Go's time.Now internally and exposes no clock seam.
// For deterministic tests, use NewCacheForTest with small TTLs (e.g. 1s)
// and a >TTL sleep — see TestRBACCoverage_TTLCeiling.
type Cache struct {
	loader  MembershipLoader
	members *expirable.LRU[cacheKey, CachedMember]
	roles   *expirable.LRU[uuid.UUID, CachedRole]
}

// NewCache builds a Cache with production sizes/TTL (1024 / 256, 30s).
// Panics on nil loader.
func NewCache(loader MembershipLoader) *Cache {
	if loader == nil {
		panic("authz.NewCache: loader cannot be nil")
	}
	return &Cache{
		loader:  loader,
		members: expirable.NewLRU[cacheKey, CachedMember](membershipCacheSize, nil, cacheTTL),
		roles:   expirable.NewLRU[uuid.UUID, CachedRole](roleCacheSize, nil, cacheTTL),
	}
}

// NewCacheForTest constructs a Cache with INJECTABLE TTLs for both caches.
// ONLY for use by tests (TestRBACCoverage_TTLCeiling). Production
// callers must use NewCache(loader). Sizes are kept small (16/16) — tests
// do not exercise eviction. Panics on nil loader.
//
// Honors the SPEC AUTHZ-10 <1s determinism bar by allowing TTL=1s with a
// 1.1s sleep instead of multi-second sleeps; see.
func NewCacheForTest(loader MembershipLoader, membershipTTL, roleTTL time.Duration) *Cache {
	if loader == nil {
		panic("authz.NewCacheForTest: loader cannot be nil")
	}
	return &Cache{
		loader:  loader,
		members: expirable.NewLRU[cacheKey, CachedMember](authzCacheBucketCount, nil, membershipTTL),
		roles:   expirable.NewLRU[uuid.UUID, CachedRole](authzCacheBucketCount, nil, roleTTL),
	}
}

// GetMembership returns the cached membership; on miss, calls loader.LoadMembership and populates.
// Returns the loader error verbatim (so callers can errors.Is(..., domain.ErrMembershipNotFound)).
func (c *Cache) GetMembership(ctx context.Context, businessID, userID uuid.UUID) (CachedMember, error) {
	key := cacheKey{BusinessID: businessID, UserID: userID}
	if v, ok := c.members.Get(key); ok {
		return v, nil
	}
	loaded, err := c.loader.LoadMembership(ctx, businessID, userID)
	if err != nil {
		return CachedMember{}, fmt.Errorf("load membership: %w", err)
	}
	if loaded == nil {
		return CachedMember{}, errors.New("authz: loader returned nil membership without error")
	}
	c.members.Add(key, *loaded)
	return *loaded, nil
}

// GetRole returns the cached role; on miss, calls loader.LoadRole and populates.
func (c *Cache) GetRole(ctx context.Context, roleID uuid.UUID) (CachedRole, error) {
	if v, ok := c.roles.Get(roleID); ok {
		return v, nil
	}
	loaded, err := c.loader.LoadRole(ctx, roleID)
	if err != nil {
		return CachedRole{}, fmt.Errorf("load role: %w", err)
	}
	if loaded == nil {
		return CachedRole{}, errors.New("authz: loader returned nil role without error")
	}
	c.roles.Add(roleID, *loaded)
	return *loaded, nil
}

// InvalidateMember deletes the cached membership for (businessID, userID).
// O(1). Call AFTER tx.Commit per AUTHZ-04 / MEMBER-05.
func (c *Cache) InvalidateMember(businessID, userID uuid.UUID) {
	c.members.Remove(cacheKey{BusinessID: businessID, UserID: userID})
}

// InvalidateRole deletes the cached role permissions for roleID.
// O(1). Membership entries holding RoleID=roleID stay valid; their next
// GetRole lookup misses and refreshes permissions.
// The businessID parameter is kept in the public API to match
// AUTHZ-04's documented two-arg shape, but the cache is keyed by roleID
// alone (roles are unique across businesses by UUID).
//
// production callers will land in — every role-mutation
// handler (PUT /roles/{id}, DELETE /roles/{id}) MUST call this AFTER
// tx.Commit, the same post-commit ordering MEMBER-05 documents for
// InvalidateMember. Without the call, every member holding the edited
// role evaluates Can against the stale permission slice for up to
// the cache TTL (~30 s).
//
// TODO: wire InvalidateRole into role-mutation endpoints — the role-update
// handler for custom roles must call this exactly once per successful mutation.
func (c *Cache) InvalidateRole(businessID, roleID uuid.UUID) {
	_ = businessID
	c.roles.Remove(roleID)
}
