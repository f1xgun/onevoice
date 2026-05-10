package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestIncRBACCheck(t *testing.T) {
	cases := []struct{ name, result string }{
		{"allow", "allow"},
		{"deny", "deny"},
		{"missing", "missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := testutil.ToFloat64(rbacCheckTotal.WithLabelValues(tc.result))
			IncRBACCheck(tc.result)
			after := testutil.ToFloat64(rbacCheckTotal.WithLabelValues(tc.result))
			require.InDelta(t, before+1, after, 0.0001)
		})
	}
}

func TestIncRBACCheck_OnlyIncrementsSingleLabel(t *testing.T) {
	// After incrementing "allow", "deny" should remain unchanged.
	beforeDeny := testutil.ToFloat64(rbacCheckTotal.WithLabelValues("deny"))
	IncRBACCheck("allow")
	afterDeny := testutil.ToFloat64(rbacCheckTotal.WithLabelValues("deny"))
	require.InDelta(t, beforeDeny, afterDeny, 0.0001, "incrementing 'allow' should not affect 'deny'")

	// After incrementing "deny", "missing" should remain unchanged.
	beforeMissing := testutil.ToFloat64(rbacCheckTotal.WithLabelValues("missing"))
	IncRBACCheck("deny")
	afterMissing := testutil.ToFloat64(rbacCheckTotal.WithLabelValues("missing"))
	require.InDelta(t, beforeMissing, afterMissing, 0.0001, "incrementing 'deny' should not affect 'missing'")
}

// TestIncRBACCheck_LabelShape asserts the counter uses exactly one label
// named "result" by gathering from the default registry and inspecting labels.
func TestIncRBACCheck_LabelShape(t *testing.T) {
	// Ensure the counter has been initialized by calling it.
	IncRBACCheck("allow")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	mf := findMetric(families, "rbac_check_total")
	require.NotNil(t, mf, "rbac_check_total metric family not found")

	for _, m := range mf.GetMetric() {
		labels := m.GetLabel()
		require.Len(t, labels, 1, "rbac_check_total should have exactly 1 label")
		require.Equal(t, "result", labels[0].GetName(), "label name should be 'result'")
	}
}
