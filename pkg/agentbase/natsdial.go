package agentbase

import (
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"
)

// natsReconnectWait is the backoff between NATS reconnect attempts. It mirrors
// the orchestrator's dial posture so every NATS client in the cluster shares
// the same reconnect cadence.
const natsReconnectWait = 2 * time.Second

// resilientNATSOptions returns the NATS dial options that make a connection
// survive an outage of any duration. MaxReconnects(-1) is infinite, so a NATS
// restart/upgrade or partition longer than the nats.go default budget
// (MaxReconnect=60 × ReconnectWait=2s ≈ 2 min) no longer exhausts the budget,
// closes the conn, and silently strands the agent's plain subscriptions (its
// tasks.<agentID> sub and the integrations.revoked.<platform>.* revoke fan-out).
// RetryOnFailedConnect makes a NATS that is down at boot non-fatal: Connect
// returns a live conn already reconnecting. This is the same option set the
// orchestrator already dials with — kept here as a single helper so it is unit
// tested and any drift back to a bare Connect is caught.
func resilientNATSOptions() []natslib.Option {
	return []natslib.Option{
		natslib.RetryOnFailedConnect(true),
		natslib.MaxReconnects(-1),
		natslib.ReconnectWait(natsReconnectWait),
		natslib.DisconnectErrHandler(func(_ *natslib.Conn, e error) {
			slog.Warn("NATS disconnected", "error", e)
		}),
		natslib.ReconnectHandler(func(c *natslib.Conn) {
			slog.Info("NATS reconnected", "url", redactConnURL(c.ConnectedUrl()))
		}),
	}
}
