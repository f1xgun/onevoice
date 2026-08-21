package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestSetNorthStar_RatioAndComponents(t *testing.T) {
	SetNorthStar(10, 4)
	assert.InDelta(t, 10, testutil.ToFloat64(presenceUpdates7d), 1e-9)
	assert.InDelta(t, 4, testutil.ToFloat64(activeBusinesses7d), 1e-9)
	assert.InDelta(t, 2.5, testutil.ToFloat64(nsmPresenceUpdatesPerActiveBiz), 1e-9)
}

func TestSetNorthStar_ZeroActiveIsNotNaN(t *testing.T) {
	SetNorthStar(5, 0)
	assert.InDelta(t, 5, testutil.ToFloat64(presenceUpdates7d), 1e-9)
	assert.InDelta(t, 0, testutil.ToFloat64(activeBusinesses7d), 1e-9)
	// ratio must be a clean 0, never NaN, when no business is active.
	assert.Equal(t, 0.0, testutil.ToFloat64(nsmPresenceUpdatesPerActiveBiz))
}
