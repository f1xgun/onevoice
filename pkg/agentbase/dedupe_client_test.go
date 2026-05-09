package agentbase_test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/agentbase"
)

func TestNewDedupeClient_EmptyURL_ReturnsNil(t *testing.T) {
	// Empty REDIS_URL must not panic and must not refuse the boot.
	// The agent's contract is "fall back to no-dedupe" rather than fatal.
	got := agentbase.NewDedupeClient("")
	assert.Nil(t, got)
}

func TestNewDedupeClient_ParseError_ReturnsNil(t *testing.T) {
	// Garbage URL → ParseURL fails → we log + return nil, never touch network.
	got := agentbase.NewDedupeClient("not a url")
	assert.Nil(t, got)
}

func TestNewDedupeClient_PingFails_ReturnsNil(t *testing.T) {
	// Spin up miniredis to claim a real port, capture its address, then close
	// it so subsequent dials get a fast "connection refused". This proves the
	// ping path works AND keeps the test sub-second (no 2s timeout wait).
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	got := agentbase.NewDedupeClient("redis://" + addr)
	assert.Nil(t, got, "ping against a closed miniredis must return nil, not panic")
}

func TestNewDedupeClient_HappyPath_ReturnsClient(t *testing.T) {
	// Live miniredis — ping succeeds, we get a real *hitldedupe.DedupeClient.
	// Mirrors the production wiring so any future regressions in the dial
	// path are caught here.
	mr := miniredis.RunT(t)

	got := agentbase.NewDedupeClient("redis://" + mr.Addr())
	assert.NotNil(t, got, "happy-path ping should produce a non-nil DedupeClient")
}

func TestGetEnv_FallsBackToDefault(t *testing.T) {
	// t.Setenv auto-restores on test cleanup; we set the var to empty to
	// prove that "" triggers the fallback (matches os.Getenv semantics).
	t.Setenv("AGENTBASE_TEST_KEY", "")

	got := agentbase.GetEnv("AGENTBASE_TEST_KEY", "fallback")
	assert.Equal(t, "fallback", got)
}

func TestGetEnv_ReturnsValueWhenSet(t *testing.T) {
	t.Setenv("AGENTBASE_TEST_KEY", "actual")

	got := agentbase.GetEnv("AGENTBASE_TEST_KEY", "fallback")
	assert.Equal(t, "actual", got)
}

func TestGetEnv_UnsetVariableReturnsDefault(t *testing.T) {
	// Use a key that nothing else sets to verify the unset (vs empty) branch.
	got := agentbase.GetEnv("AGENTBASE_DEFINITELY_UNSET_XYZ_42", "fallback")
	assert.Equal(t, "fallback", got)
}
