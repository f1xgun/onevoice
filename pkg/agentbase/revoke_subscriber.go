package agentbase

import (
	"fmt"
	"strings"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
)

// RevokeSubscriber holds the NATS subscription that invalidates an agent's
// token cache when an integration is revoked elsewhere in the cluster.
type RevokeSubscriber struct {
	sub *natslib.Subscription
}

// NewRevokeSubscriber subscribes the agent to its own platform's revoke
// fan-out subject (integrations.revoked.<platform>.*). On each message it parses
// the businessID from the subject and calls tc.Invalidate(businessID, platform,
// "") so the next GetToken re-fetches from the API. The wire payload is empty —
// all routing lives in the subject — so a wildcard cache invalidation is used.
// The caller owns nc and tc; Close releases only the subscription.
func NewRevokeSubscriber(nc *natslib.Conn, tc *tokenclient.Client, platform string) (*RevokeSubscriber, error) {
	subject := revokeSubject(platform)
	sub, err := nc.Subscribe(subject, func(msg *natslib.Msg) {
		handleRevokeMessage(msg, platform, tc.Invalidate, metrics.IncIntegrationsRevokedReceived)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", subject, err)
	}
	return &RevokeSubscriber{sub: sub}, nil
}

// Close unsubscribes. It is safe to call on a nil receiver or a zero value.
func (r *RevokeSubscriber) Close() error {
	if r == nil || r.sub == nil {
		return nil
	}
	return r.sub.Unsubscribe()
}

func revokeSubject(platform string) string {
	return fmt.Sprintf("integrations.revoked.%s.*", platform)
}

func handleRevokeMessage(
	msg *natslib.Msg,
	platform string,
	invalidate func(businessID, platform, externalID string),
	recordReceived func(platform string),
) {
	parts := strings.Split(msg.Subject, ".")
	if len(parts) != 4 {
		return
	}
	businessID := parts[3]
	if businessID == "" {
		return
	}
	invalidate(businessID, platform, "")
	recordReceived(platform)
}
