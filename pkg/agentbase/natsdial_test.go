package agentbase

import (
	"testing"
	"time"

	natslib "github.com/nats-io/nats.go"
)

// TestResilientNATSOptions pins the dial posture that keeps an agent's NATS
// subscriptions (its tasks.<agentID> sub and the revoke fan-out) alive across
// an outage of any duration. Applying the options to a default Options must flip
// MaxReconnect to -1 (infinite), enable RetryOnFailedConnect, and set the 2s
// reconnect backoff. Reverting Run to a bare natslib.Connect (no options) leaves
// these at their nats.go defaults (MaxReconnect=60, RetryOnFailedConnect=false,
// ReconnectWait=2s), which fails the MaxReconnect and RetryOnFailedConnect
// assertions below.
func TestResilientNATSOptions(t *testing.T) {
	t.Parallel()

	opts := natslib.GetDefaultOptions()
	for _, o := range resilientNATSOptions() {
		if err := o(&opts); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}

	if opts.MaxReconnect != -1 {
		t.Errorf("MaxReconnect = %d, want -1 (infinite)", opts.MaxReconnect)
	}
	if !opts.RetryOnFailedConnect {
		t.Errorf("RetryOnFailedConnect = false, want true")
	}
	if opts.ReconnectWait != 2*time.Second {
		t.Errorf("ReconnectWait = %v, want %v", opts.ReconnectWait, 2*time.Second)
	}
}
