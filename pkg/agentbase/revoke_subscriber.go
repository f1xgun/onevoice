package agentbase

import (
	"fmt"
	"log/slog"
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

// RevokeHook runs for the revoked businessID after the token cache is
// invalidated. Agents that cache more than the token (e.g. the Yandex RPA pool's
// per-business BrowserContext) register one so a revoke also tears down their
// own state instead of serving it until the next idle sweep.
type RevokeHook func(businessID string)

type revokeOptions struct {
	hooks []RevokeHook
}

// WithRevokeHook registers an extra per-businessID hook invoked alongside the
// token-cache invalidation on every revoke. Multiple hooks may be registered.
func WithRevokeHook(hook RevokeHook) func(*revokeOptions) {
	return func(o *revokeOptions) {
		if hook != nil {
			o.hooks = append(o.hooks, hook)
		}
	}
}

// NewRevokeSubscriber subscribes the agent to its own platform's revoke
// fan-out subject (integrations.revoked.<platform>.*). On each message it parses
// the businessID from the subject and calls tc.Invalidate(businessID, platform,
// "") so the next GetToken re-fetches from the API, then runs any hooks
// registered via WithRevokeHook for the same businessID. The wire payload is
// empty — all routing lives in the subject — so a wildcard cache invalidation is
// used. The caller owns nc and tc; Close releases only the subscription.
func NewRevokeSubscriber(nc *natslib.Conn, tc *tokenclient.Client, platform string, opts ...func(*revokeOptions)) (*RevokeSubscriber, error) {
	var o revokeOptions
	for _, opt := range opts {
		opt(&o)
	}
	invalidate := func(businessID, platform, externalID string) {
		tc.Invalidate(businessID, platform, externalID)
		for _, hook := range o.hooks {
			hook(businessID)
		}
	}
	subject := revokeSubject(platform)
	sub, err := nc.Subscribe(subject, func(msg *natslib.Msg) {
		handleRevokeMessage(msg, platform, invalidate, metrics.IncIntegrationsRevokedReceived, metrics.IncIntegrationsRevokeDropped)
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
	recordDropped func(platform string),
) {
	parts := strings.Split(msg.Subject, ".")
	if len(parts) != 4 || parts[3] == "" {
		slog.Warn("revoke subscriber: dropping malformed subject", "subject", msg.Subject, "platform", platform)
		recordDropped(platform)
		return
	}
	businessID := parts[3]
	invalidate(businessID, platform, "")
	recordReceived(platform)
}
