package chatturn

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/ssecounter"
)

// TestStreamBudget_FitsSSECounterKeyTTL pins the cross-module invariant that the
// SSE concurrency slot key outlives a single chat stream. The wiring derives the
// counter's key TTL from StreamBudget plus slack, but ssecounter clamps to
// ssecounter.MinKeyTTL, so that floor must itself be >= streamBudget. If either
// constant drifts so the key could expire mid-stream, this fails: a key expiring
// mid-stream lets a concurrent acquire recreate the counter from 0 (cap stops
// bounding the heavy long streams) and the eventual release recreates it
// negative and never-expiring.
func TestStreamBudget_FitsSSECounterKeyTTL(t *testing.T) {
	assert.Equal(t, streamBudget, StreamBudget,
		"exported StreamBudget must mirror the internal streamBudget")
	assert.GreaterOrEqual(t, ssecounter.MinKeyTTL, streamBudget,
		"SSE slot-key TTL floor must outlive a single chat stream budget")
}
