package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestIncSweeperRun(t *testing.T) {
	for _, result := range []string{SweeperResultOK, SweeperResultError} {
		t.Run(result, func(t *testing.T) {
			before := testutil.ToFloat64(sweeperRunsTotal.WithLabelValues(SweeperAccountHardDelete, result))
			IncSweeperRun(SweeperAccountHardDelete, result)
			after := testutil.ToFloat64(sweeperRunsTotal.WithLabelValues(SweeperAccountHardDelete, result))
			require.InDelta(t, before+1, after, 0.0001)
		})
	}
}

func TestIncSweeperRun_OnlyIncrementsSingleSeries(t *testing.T) {
	beforeErr := testutil.ToFloat64(sweeperRunsTotal.WithLabelValues(SweeperBusinessHardDelete, SweeperResultError))
	beforeOtherSweeper := testutil.ToFloat64(sweeperRunsTotal.WithLabelValues(SweeperDeletionWarning, SweeperResultOK))
	IncSweeperRun(SweeperBusinessHardDelete, SweeperResultOK)
	afterErr := testutil.ToFloat64(sweeperRunsTotal.WithLabelValues(SweeperBusinessHardDelete, SweeperResultError))
	afterOtherSweeper := testutil.ToFloat64(sweeperRunsTotal.WithLabelValues(SweeperDeletionWarning, SweeperResultOK))
	require.InDelta(t, beforeErr, afterErr, 0.0001, "ok must not touch the error series")
	require.InDelta(t, beforeOtherSweeper, afterOtherSweeper, 0.0001, "one sweeper must not touch another")
}

func TestAddSweeperItems(t *testing.T) {
	before := testutil.ToFloat64(sweeperItemsProcessedTotal.WithLabelValues(SweeperDeletionWarning))
	AddSweeperItems(SweeperDeletionWarning, 3)
	after := testutil.ToFloat64(sweeperItemsProcessedTotal.WithLabelValues(SweeperDeletionWarning))
	require.InDelta(t, before+3, after, 0.0001)
}

func TestAddSweeperItems_NonPositiveIsNoOp(t *testing.T) {
	before := testutil.ToFloat64(sweeperItemsProcessedTotal.WithLabelValues(SweeperAccountHardDelete))
	AddSweeperItems(SweeperAccountHardDelete, 0)
	AddSweeperItems(SweeperAccountHardDelete, -5)
	after := testutil.ToFloat64(sweeperItemsProcessedTotal.WithLabelValues(SweeperAccountHardDelete))
	require.InDelta(t, before, after, 0.0001, "n<=0 must not move the counter")
}

func TestMarkSweeperSuccess(t *testing.T) {
	start := time.Now().Unix()
	MarkSweeperSuccess(SweeperBusinessHardDelete)
	got := testutil.ToFloat64(sweeperLastSuccessTimestamp.WithLabelValues(SweeperBusinessHardDelete))
	require.GreaterOrEqual(t, got, float64(start), "gauge must be stamped to a current unix timestamp")
}
