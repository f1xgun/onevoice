package hitldedupe_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// newTestClient spins up a miniredis server + go-redis client and returns
// both the DedupeClient and the underlying *miniredis.Miniredis (so tests can
// inspect state, fast-forward time, and assert DbSize).
func newTestClient(t *testing.T) (*hitldedupe.DedupeClient, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return hitldedupe.New(rdb), mr
}

func TestKeyFor_ExactFormat(t *testing.T) {
	assert.Equal(t, "hitl:approval:biz-1:appr-a", hitldedupe.KeyFor("biz-1", "appr-a"))
	assert.Equal(t, "hitl:approval:b:a", hitldedupe.KeyFor("b", "a"))
}

func TestClaim_EmptyApprovalID_ReturnsSkip(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	require.Equal(t, 0, len(mr.Keys()), "pre-condition: empty redis")

	outcome, cached, err := client.Claim(ctx, "biz-1", "")
	require.NoError(t, err)
	assert.Equal(t, hitldedupe.ClaimOutcomeSkip, outcome)
	assert.Equal(t, "", cached)
	assert.Equal(t, 0, len(mr.Keys()),
		"Claim with empty approvalID must NOT touch Redis (anti-footgun #2)")
}

func TestClaim_FirstCall_ReturnsClaimed(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	outcome, cached, err := client.Claim(ctx, "biz-1", "appr-1")
	require.NoError(t, err)
	assert.Equal(t, hitldedupe.ClaimOutcomeClaimed, outcome)
	assert.Equal(t, "", cached)

	key := hitldedupe.KeyFor("biz-1", "appr-1")
	assert.True(t, mr.Exists(key), "SetNX must have created the key")
	got, err := mr.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "executing", got, "first-claim sentinel is 'executing'")
}

func TestClaim_SecondCall_WhileExecuting_ReturnsInFlight(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	out1, _, err := client.Claim(ctx, "biz-1", "appr-1")
	require.NoError(t, err)
	require.Equal(t, hitldedupe.ClaimOutcomeClaimed, out1)

	out2, cached, err := client.Claim(ctx, "biz-1", "appr-1")
	require.NoError(t, err)
	assert.Equal(t, hitldedupe.ClaimOutcomeInFlight, out2)
	assert.Equal(t, "", cached)
}

func TestClaim_AfterStore_ReturnsDuplicate(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	out1, _, err := client.Claim(ctx, "biz-1", "appr-1")
	require.NoError(t, err)
	require.Equal(t, hitldedupe.ClaimOutcomeClaimed, out1)

	result := map[string]interface{}{"task_id": "t1", "success": true, "result": map[string]interface{}{"ok": true}}
	require.NoError(t, client.Store(ctx, "biz-1", "appr-1", result))

	out2, cached, err := client.Claim(ctx, "biz-1", "appr-1")
	require.NoError(t, err)
	assert.Equal(t, hitldedupe.ClaimOutcomeDuplicate, out2)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(cached), &decoded))
	assert.Equal(t, "t1", decoded["task_id"])
	assert.Equal(t, true, decoded["success"])
}

func TestStore_EmptyApprovalID_IsNoOp(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	require.Equal(t, 0, len(mr.Keys()))
	err := client.Store(ctx, "biz-1", "", map[string]interface{}{"ok": true})
	require.NoError(t, err)
	assert.Equal(t, 0, len(mr.Keys()),
		"Store with empty approvalID must NOT create a Redis key")
}

func TestClaim_TTLIsSet(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	_, _, err := client.Claim(ctx, "biz-1", "appr-ttl")
	require.NoError(t, err)

	key := hitldedupe.KeyFor("biz-1", "appr-ttl")
	require.True(t, mr.Exists(key), "pre-condition: key exists")

	mr.FastForward(25 * time.Hour)
	assert.False(t, mr.Exists(key), "key must auto-expire after TTL")
}

func TestClaim_ExecutingSentinel_UsesShortTTL(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	_, _, err := client.Claim(ctx, "biz-1", "appr-short")
	require.NoError(t, err)

	key := hitldedupe.KeyFor("biz-1", "appr-short")
	require.True(t, mr.Exists(key), "pre-condition: key exists")

	ttl := mr.TTL(key)
	assert.Equal(t, hitldedupe.ExecutingTTL, ttl,
		"executing sentinel must use the short ExecutingTTL, not the 24h DedupeTTL")
	assert.Less(t, ttl, hitldedupe.DedupeTTL,
		"executing sentinel TTL must be far below the completed-result DedupeTTL")
}

func TestStore_CompletedResult_UsesDedupeTTL(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	_, _, err := client.Claim(ctx, "biz-1", "appr-done")
	require.NoError(t, err)

	require.NoError(t, client.Store(ctx, "biz-1", "appr-done",
		map[string]interface{}{"task_id": "t", "success": true}))

	key := hitldedupe.KeyFor("biz-1", "appr-done")
	ttl := mr.TTL(key)
	assert.Equal(t, hitldedupe.DedupeTTL, ttl,
		"a completed Store must keep the full 24h DedupeTTL, not the short ExecutingTTL")
	assert.Greater(t, ttl, hitldedupe.ExecutingTTL,
		"completed result TTL must outlast the executing sentinel window")
}

func TestRelease_DeletesExecutingSentinel(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	out, _, err := client.Claim(ctx, "biz-1", "appr-rel")
	require.NoError(t, err)
	require.Equal(t, hitldedupe.ClaimOutcomeClaimed, out)
	key := hitldedupe.KeyFor("biz-1", "appr-rel")
	require.True(t, mr.Exists(key), "pre-condition: claim wrote the sentinel")

	require.NoError(t, client.Release(ctx, "biz-1", "appr-rel"))
	assert.False(t, mr.Exists(key), "Release must delete the executing sentinel")

	out2, _, err := client.Claim(ctx, "biz-1", "appr-rel")
	require.NoError(t, err)
	assert.Equal(t, hitldedupe.ClaimOutcomeClaimed, out2,
		"after Release a retry must be able to re-Claim")
}

func TestRelease_EmptyApprovalID_IsNoOp(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	require.NoError(t, client.Release(ctx, "biz-1", ""))
	assert.Equal(t, 0, len(mr.Keys()),
		"Release with empty approvalID must NOT touch Redis")
}

func TestStore_OverwritesExecuting_WithFreshTTL(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	_, _, err := client.Claim(ctx, "biz-1", "appr-store")
	require.NoError(t, err)
	key := hitldedupe.KeyFor("biz-1", "appr-store")

	mr.FastForward(30 * time.Second)
	ttlBefore := mr.TTL(key)
	require.Greater(t, ttlBefore, time.Duration(0))
	require.Less(t, ttlBefore, hitldedupe.ExecutingTTL,
		"sentinel TTL must have decreased from ExecutingTTL after FastForward; got %v", ttlBefore)

	require.NoError(t, client.Store(ctx, "biz-1", "appr-store",
		map[string]interface{}{"task_id": "t", "success": true}))

	ttlAfter := mr.TTL(key)
	assert.Greater(t, ttlAfter, 23*time.Hour,
		"Store must refresh TTL to ~24h; got %v", ttlAfter)

	val, err := mr.Get(key)
	require.NoError(t, err)
	assert.NotEqual(t, "executing", val, "Store must overwrite the executing sentinel")
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(val), &decoded))
	assert.Equal(t, "t", decoded["task_id"])
}
