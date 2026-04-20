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
	// Canonical key format per 16-CONTEXT.md D-25. ANY drift here silently
	// breaks cross-agent dedupe, so this is a guardrail grep-friendly test.
	assert.Equal(t, "hitl:approval:biz-1:appr-a", hitldedupe.KeyFor("biz-1", "appr-a"))
	assert.Equal(t, "hitl:approval:b:a", hitldedupe.KeyFor("b", "a"))
}

func TestClaim_EmptyApprovalID_ReturnsSkip(t *testing.T) {
	// Anti-footgun #2 (per 16-OVERVIEW.md): empty approvalID must SKIP SetNX
	// entirely — NOT SetNX with an empty-string key. len(mr.Keys()) MUST
	// stay 0 across the Claim call.
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

	// Without Store, the key still holds the "executing" sentinel.
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

	// miniredis FastForward past the 24h TTL — the key must be gone.
	mr.FastForward(25 * time.Hour)
	assert.False(t, mr.Exists(key), "key must auto-expire after TTL")
}

func TestStore_OverwritesExecuting_WithFreshTTL(t *testing.T) {
	client, mr := newTestClient(t)
	ctx := context.Background()

	_, _, err := client.Claim(ctx, "biz-1", "appr-store")
	require.NoError(t, err)
	key := hitldedupe.KeyFor("biz-1", "appr-store")

	// Advance 1 hour — TTL should be ~23h now.
	mr.FastForward(1 * time.Hour)
	ttlBefore := mr.TTL(key)
	require.Greater(t, ttlBefore, time.Duration(0))
	require.Less(t, ttlBefore, 24*time.Hour,
		"TTL must have decreased from 24h after FastForward(1h); got %v", ttlBefore)

	// Store refreshes the TTL back to 24h.
	require.NoError(t, client.Store(ctx, "biz-1", "appr-store",
		map[string]interface{}{"task_id": "t", "success": true}))

	ttlAfter := mr.TTL(key)
	// miniredis TTL is the full remaining duration; with a fresh 24h it should
	// be close to 24h (within a few ms of scheduling slack).
	assert.Greater(t, ttlAfter, 23*time.Hour,
		"Store must refresh TTL to ~24h; got %v", ttlAfter)

	// Value must now be a JSON blob, not "executing".
	val, err := mr.Get(key)
	require.NoError(t, err)
	assert.NotEqual(t, "executing", val, "Store must overwrite the executing sentinel")
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(val), &decoded))
	assert.Equal(t, "t", decoded["task_id"])
}
