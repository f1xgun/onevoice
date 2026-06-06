package agentbase_test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/agentbase"
)

func TestNewDedupeClient_EmptyURL_ReturnsNil(t *testing.T) {
	got := agentbase.NewDedupeClient("")
	assert.Nil(t, got)
}

func TestNewDedupeClient_ParseError_ReturnsNil(t *testing.T) {
	got := agentbase.NewDedupeClient("not a url")
	assert.Nil(t, got)
}

func TestNewDedupeClient_PingFails_ReturnsNil(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	got := agentbase.NewDedupeClient("redis://" + addr)
	assert.Nil(t, got, "ping against a closed miniredis must return nil, not panic")
}

func TestNewDedupeClient_HappyPath_ReturnsClient(t *testing.T) {
	mr := miniredis.RunT(t)

	got := agentbase.NewDedupeClient("redis://" + mr.Addr())
	assert.NotNil(t, got, "happy-path ping should produce a non-nil DedupeClient")
}

func TestGetEnv_FallsBackToDefault(t *testing.T) {
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
	got := agentbase.GetEnv("AGENTBASE_DEFINITELY_UNSET_XYZ_42", "fallback")
	assert.Equal(t, "fallback", got)
}
