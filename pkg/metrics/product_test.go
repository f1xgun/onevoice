package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestIncChatTurn(t *testing.T) {
	for _, outcome := range []string{"done", "error", "pause_hitl", "rejoined_resume"} {
		t.Run(outcome, func(t *testing.T) {
			before := testutil.ToFloat64(chatTurnsTotal.WithLabelValues(outcome))
			IncChatTurn(outcome)
			after := testutil.ToFloat64(chatTurnsTotal.WithLabelValues(outcome))
			require.InDelta(t, before+1, after, 0.0001)
		})
	}
}

func TestIncChatTurn_OnlyIncrementsSingleLabel(t *testing.T) {
	beforeErr := testutil.ToFloat64(chatTurnsTotal.WithLabelValues("error"))
	IncChatTurn("done")
	afterErr := testutil.ToFloat64(chatTurnsTotal.WithLabelValues("error"))
	require.InDelta(t, beforeErr, afterErr, 0.0001, "incrementing 'done' must not affect 'error'")
}

func TestIncHITLDecision(t *testing.T) {
	for _, decision := range []string{"approve", "edit", "reject"} {
		t.Run(decision, func(t *testing.T) {
			before := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(decision))
			IncHITLDecision(decision)
			after := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(decision))
			require.InDelta(t, before+1, after, 0.0001)
		})
	}
}

func TestIncHITLDecision_OnlyIncrementsSingleLabel(t *testing.T) {
	beforeReject := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues("reject"))
	IncHITLDecision("approve")
	afterReject := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues("reject"))
	require.InDelta(t, beforeReject, afterReject, 0.0001, "incrementing 'approve' must not affect 'reject'")
}

// TestIncHITLDecision_CollapsesUnknown guards the cardinality bound: an
// unvalidated upstream action string must land in the bounded "other"
// bucket and must NOT create a new series for the raw value.
func TestIncHITLDecision_CollapsesUnknown(t *testing.T) {
	beforeOther := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(labelOther))
	IncHITLDecision("frobnicate")
	IncHITLDecision("")
	afterOther := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(labelOther))
	require.InDelta(t, beforeOther+2, afterOther, 0.0001, "unknown decisions must increment 'other'")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	mf := findMetric(families, "hitl_decisions_total")
	require.NotNil(t, mf)
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			require.NotContains(t, []string{"frobnicate", ""}, l.GetValue(),
				"raw unknown decision values must never become a series")
		}
	}
}

func TestProductMetricsLabelShape(t *testing.T) {
	IncChatTurn("done")
	IncHITLDecision("approve")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, tc := range []struct{ metric, label string }{
		{"chat_turns_total", "outcome"},
		{"hitl_decisions_total", "decision"},
	} {
		mf := findMetric(families, tc.metric)
		require.NotNil(t, mf, "%s metric family not found", tc.metric)
		for _, m := range mf.GetMetric() {
			labels := m.GetLabel()
			require.Len(t, labels, 1, "%s should have exactly 1 label", tc.metric)
			require.Equal(t, tc.label, labels[0].GetName())
		}
	}
}
