package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// IntegrationTokenDecryptedCounter exposes the {platform,caller_service} child
// of the token-decrypted counter so other packages can read its current value
// (e.g. assert the increment in a fail-closed audit test).
func IntegrationTokenDecryptedCounter(platform, callerService string) prometheus.Counter {
	return integrationTokenDecryptedTotal.WithLabelValues(platform, callerService)
}

// integrationsRevokedReceivedTotal counts revoke fan-out messages received by
// agent subscribers, labeled by platform. Cardinality bounded by the four
// known agent platforms.
var integrationsRevokedReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "integrations_revoked_received_total",
	Help: "Revoke fan-out messages received by agent subscribers, labeled by platform.",
}, []string{"platform"})

// integrationsRevokePublishFailedTotal counts failures publishing
// integrations.revoked.* on the API side. Fail-open path — the cache TTL
// backstop handles the gap.
var integrationsRevokePublishFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "integrations_revoke_publish_failed_total",
	Help: "Failures publishing integrations.revoked.* on the API side. Fail-open path — TTL backstop handles the gap.",
})

// natsPublishRejectedTotal counts tool dispatches rejected before NATS
// publish, labeled by reason. Cardinality bounded by the closed reason set.
var natsPublishRejectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "nats_publish_rejected_total",
	Help: "Tool dispatches rejected before NATS publish, labeled by reason. Bounded cardinality.",
}, []string{"reason"})

// integrationTokenDecryptedTotal counts successful GetDecryptedToken
// invocations, labeled by platform and caller_service. Caller cardinality is
// bounded by the known service identities.
var integrationTokenDecryptedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "integration_token_decrypted_total",
	Help: "Successful GetDecryptedToken invocations, labeled by platform and caller_service. Caller cardinality is bounded by known service identities.",
}, []string{"platform", "caller_service"})

// integrationsPurgeRunsTotal counts integrations soft-delete purge sweep
// invocations, labeled by result {ok|locked|error}.
var integrationsPurgeRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "integrations_purge_runs_total",
	Help: "Integrations soft-delete purge sweep invocations, labeled by result {ok|locked|error}.",
}, []string{"result"})

// integrationsPurgedTotal counts integrations rows hard-deleted by the 90-day
// purge sweep across all runs.
var integrationsPurgedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "integrations_purged_total",
	Help: "Integrations rows hard-deleted by the 90-day purge sweep across all runs.",
})

// IncIntegrationsRevokedReceived increments the {platform} bucket of the
// revoke-received counter.
func IncIntegrationsRevokedReceived(platform string) {
	integrationsRevokedReceivedTotal.WithLabelValues(platform).Inc()
}

// IncIntegrationsRevokePublishFailed increments the revoke-publish-failed
// counter.
func IncIntegrationsRevokePublishFailed() {
	integrationsRevokePublishFailedTotal.Inc()
}

// IncNATSPublishRejected increments the {reason} bucket of the
// NATS-publish-rejected counter.
func IncNATSPublishRejected(reason string) {
	natsPublishRejectedTotal.WithLabelValues(reason).Inc()
}

// IncIntegrationTokenDecrypted increments the {platform,caller_service} bucket
// of the token-decrypted counter.
func IncIntegrationTokenDecrypted(platform, callerService string) {
	integrationTokenDecryptedTotal.WithLabelValues(platform, callerService).Inc()
}

// IncIntegrationsPurgeRun increments the {result} bucket of the purge-runs
// counter. result must be one of "ok", "locked", "error".
func IncIntegrationsPurgeRun(result string) {
	integrationsPurgeRunsTotal.WithLabelValues(result).Inc()
}

// AddIntegrationsPurged increments the purged-rows counter by n. Negative or
// zero counts are no-ops; the counter is monotonic.
func AddIntegrationsPurged(n int64) {
	if n <= 0 {
		return
	}
	integrationsPurgedTotal.Add(float64(n))
}
