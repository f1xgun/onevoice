package creditmeter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/creditmeter"
)

func TestCompute_FullyCovered(t *testing.T) {
	got := creditmeter.Compute(5, 1)
	require.Equal(t, -1, got.Delta)
	require.Equal(t, 4, got.BalanceAfter)
	require.Equal(t, 0, got.OverageCredits)
	require.Equal(t, domain.CreditReasonConsume, got.Reason)
}

func TestCompute_ExactlyDrainsToZero(t *testing.T) {
	got := creditmeter.Compute(1, 1)
	require.Equal(t, -1, got.Delta)
	require.Equal(t, 0, got.BalanceAfter, "balance drains exactly to 0")
	require.Equal(t, 0, got.OverageCredits)
	require.Equal(t, domain.CreditReasonConsume, got.Reason)
}

func TestCompute_ZeroBalanceIsAllOverage(t *testing.T) {
	got := creditmeter.Compute(0, 1)
	require.Equal(t, 0, got.Delta, "nothing drawn from an empty balance")
	require.Equal(t, 0, got.BalanceAfter, "balance never goes negative")
	require.Equal(t, 1, got.OverageCredits)
	require.Equal(t, domain.CreditReasonOverage, got.Reason)
}

func TestCompute_PartialCoverSplitsIntoOverage(t *testing.T) {
	// consume 5 against a balance of 2: draw 2 down to 0, 3 overage.
	got := creditmeter.Compute(2, 5)
	require.Equal(t, -2, got.Delta)
	require.Equal(t, 0, got.BalanceAfter)
	require.Equal(t, 3, got.OverageCredits)
	require.Equal(t, domain.CreditReasonOverage, got.Reason)
}

func TestCompute_NonPositiveConsumeIsNoOp(t *testing.T) {
	got := creditmeter.Compute(7, 0)
	require.Equal(t, 0, got.Delta)
	require.Equal(t, 7, got.BalanceAfter)
	require.Equal(t, 0, got.OverageCredits)
	require.Equal(t, domain.CreditReasonConsume, got.Reason)
}
