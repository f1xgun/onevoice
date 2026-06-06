package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestIncIntegrationsRevokedReceived(t *testing.T) {
	platforms := []string{"telegram", "vk", "yandex_business", "google_business"}
	for _, p := range platforms {
		before := testutil.ToFloat64(integrationsRevokedReceivedTotal.WithLabelValues(p))
		IncIntegrationsRevokedReceived(p)
		after := testutil.ToFloat64(integrationsRevokedReceivedTotal.WithLabelValues(p))
		require.InDelta(t, before+1, after, 0.0001, "platform %s", p)
	}
}

func TestIncIntegrationsRevokePublishFailed(t *testing.T) {
	before := testutil.ToFloat64(integrationsRevokePublishFailedTotal)
	IncIntegrationsRevokePublishFailed()
	after := testutil.ToFloat64(integrationsRevokePublishFailedTotal)
	require.InDelta(t, before+1, after, 0.0001)
}

func TestIncIntegrationsRevokeDropped(t *testing.T) {
	platforms := []string{"telegram", "vk", "yandex_business", "google_business"}
	for _, p := range platforms {
		before := testutil.ToFloat64(integrationsRevokeDroppedTotal.WithLabelValues(p))
		IncIntegrationsRevokeDropped(p)
		after := testutil.ToFloat64(integrationsRevokeDroppedTotal.WithLabelValues(p))
		require.InDelta(t, before+1, after, 0.0001, "platform %s", p)
	}
}

func TestIncNATSPublishRejected(t *testing.T) {
	before := testutil.ToFloat64(natsPublishRejectedTotal.WithLabelValues("denylist_key"))
	IncNATSPublishRejected("denylist_key")
	after := testutil.ToFloat64(natsPublishRejectedTotal.WithLabelValues("denylist_key"))
	require.InDelta(t, before+1, after, 0.0001)
}

func TestIncIntegrationTokenDecrypted(t *testing.T) {
	cases := []struct{ platform, caller string }{
		{"telegram", "agent-telegram"},
		{"vk", "agent-vk"},
		{"yandex_business", "orchestrator"},
	}
	for _, tc := range cases {
		before := testutil.ToFloat64(integrationTokenDecryptedTotal.WithLabelValues(tc.platform, tc.caller))
		IncIntegrationTokenDecrypted(tc.platform, tc.caller)
		after := testutil.ToFloat64(integrationTokenDecryptedTotal.WithLabelValues(tc.platform, tc.caller))
		require.InDelta(t, before+1, after, 0.0001, "%s/%s", tc.platform, tc.caller)
	}
}

func TestIncIntegrationsPurgeRun(t *testing.T) {
	for _, result := range []string{"ok", "locked", "error"} {
		before := testutil.ToFloat64(integrationsPurgeRunsTotal.WithLabelValues(result))
		IncIntegrationsPurgeRun(result)
		after := testutil.ToFloat64(integrationsPurgeRunsTotal.WithLabelValues(result))
		require.InDelta(t, before+1, after, 0.0001, "result %s", result)
	}
}

func TestAddIntegrationsPurged(t *testing.T) {
	before := testutil.ToFloat64(integrationsPurgedTotal)
	AddIntegrationsPurged(5)
	after := testutil.ToFloat64(integrationsPurgedTotal)
	require.InDelta(t, before+5, after, 0.0001)
}

func TestAddIntegrationsPurged_NonPositiveIsNoOp(t *testing.T) {
	before := testutil.ToFloat64(integrationsPurgedTotal)
	AddIntegrationsPurged(0)
	AddIntegrationsPurged(-3)
	after := testutil.ToFloat64(integrationsPurgedTotal)
	require.InDelta(t, before, after, 0.0001)
}

func TestIntegrationCounters_NonNil(t *testing.T) {
	require.NotNil(t, integrationsRevokedReceivedTotal)
	require.NotNil(t, integrationsRevokePublishFailedTotal)
	require.NotNil(t, integrationsRevokeDroppedTotal)
	require.NotNil(t, natsPublishRejectedTotal)
	require.NotNil(t, integrationTokenDecryptedTotal)
	require.NotNil(t, integrationsPurgeRunsTotal)
	require.NotNil(t, integrationsPurgedTotal)
}
