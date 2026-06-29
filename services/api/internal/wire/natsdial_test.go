package wire

import (
	"log/slog"
	"testing"
	"time"

	natslib "github.com/nats-io/nats.go"
)

// TestResilientNATSOptions pins the API's NATS dial posture so the revoke
// publisher, review syncer, and agent-task publisher survive an outage of any
// duration instead of silently failing every publish after the default
// reconnect budget is exhausted. Applying the options to a default Options must
// flip MaxReconnect to -1 (infinite), enable RetryOnFailedConnect, and set the
// 2s reconnect backoff. Reverting to a bare natslib.Connect (no options) leaves
// these at their nats.go defaults (MaxReconnect=60, RetryOnFailedConnect=false),
// which fails the assertions below.
func TestResilientNATSOptions(t *testing.T) {
	t.Parallel()

	opts := natslib.GetDefaultOptions()
	for _, o := range resilientNATSOptions(slog.Default()) {
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
