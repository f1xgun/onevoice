package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// The production adapter must satisfy AccountDeletionUserRepo, including the
// tx-aware read the sweeper now uses. A compile-time check so a signature
// drift between the interface and the adapter fails the build, not at runtime.
var _ AccountDeletionUserRepo = (*repository.UserResetExtAdapter)(nil)

// TestErrSoleOwnerBusinesses_Error verifies the error message includes
// the count — used by structured logging at the handler.
func TestErrSoleOwnerBusinesses_Error(t *testing.T) {
	err := &ErrSoleOwnerBusinesses{
		Businesses: []SoleOwnerBusiness{
			{ID: uuid.New(), Name: "Acme"},
			{ID: uuid.New(), Name: "Initech"},
		},
	}
	require.Contains(t, err.Error(), "2 business")
}

// TestErrSoleOwnerBusinesses_EmptyList still has a meaningful message —
// defensive (the service never constructs an empty-list version, but
// we want errors.As to behave deterministically).
func TestErrSoleOwnerBusinesses_EmptyList(t *testing.T) {
	err := &ErrSoleOwnerBusinesses{}
	require.Contains(t, err.Error(), "0 business")
}

// TestWithGraceDays_ReturnsCopy verifies the test-friendly override
// returns a NEW service value with the modified durations — original
// stays at production defaults.
func TestWithGraceDays_ReturnsCopy(t *testing.T) {
	s := &AccountDeletionService{graceDays: 30, t7OffsetDays: 23}
	cp := s.WithGraceDays(1, 0)
	require.Equal(t, 30, s.graceDays, "original mutated")
	require.Equal(t, 23, s.t7OffsetDays, "original mutated")
	require.Equal(t, 1, cp.graceDays)
	require.Equal(t, 0, cp.t7OffsetDays)
}

// productionDeletionWarningTick mirrors cmd/main.go's deletionWarningTick. It
// lives in package main and so can't be imported here; this copy keeps the
// window-vs-cadence invariant testable. If the production tick changes, this
// must change with it.
const productionDeletionWarningTick = 6 * time.Hour

// TestWarningScanWindow_CoversTick is the load-bearing regression guard for the
// between-ticks gap: a scan window narrower than the sweep cadence leaves users
// whose T-7 moment lands between two ticks un-enumerated and un-warned. The
// window span (toTime - fromTime) must be at least the tick interval so every
// T-7 moment is covered by some tick. Reverting to the old 1h-wide window makes
// the span smaller than the 6h tick and fails this test.
func TestWarningScanWindow_CoversTick(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

	t.Run("default window covers production tick", func(t *testing.T) {
		s := &AccountDeletionService{t7OffsetDays: deletionT7OffsetDays, warnScanWindow: defaultWarnScanWindow}
		from, to := s.warningScanWindow(now)
		require.GreaterOrEqual(t, to.Sub(from), productionDeletionWarningTick,
			"scan window narrower than sweep tick — users in the between-ticks gap get no warning")
	})

	t.Run("SetWarnScanWindow ties window to the injected tick", func(t *testing.T) {
		s := &AccountDeletionService{t7OffsetDays: deletionT7OffsetDays, warnScanWindow: defaultWarnScanWindow}
		s.SetWarnScanWindow(productionDeletionWarningTick)
		from, to := s.warningScanWindow(now)
		require.GreaterOrEqual(t, to.Sub(from), productionDeletionWarningTick,
			"window must be at least the injected tick interval")
	})

	t.Run("toTime is the T-7 instant", func(t *testing.T) {
		s := &AccountDeletionService{t7OffsetDays: deletionT7OffsetDays, warnScanWindow: defaultWarnScanWindow}
		_, to := s.warningScanWindow(now)
		require.Equal(t, now.Add(-time.Duration(deletionT7OffsetDays)*24*time.Hour), to)
	})

	t.Run("non-positive tick keeps the safe default", func(t *testing.T) {
		s := &AccountDeletionService{t7OffsetDays: deletionT7OffsetDays, warnScanWindow: defaultWarnScanWindow}
		s.SetWarnScanWindow(0)
		require.Equal(t, defaultWarnScanWindow, s.warnScanWindow)
	})
}

// TestSystemOwnerRoleID_PinnedToMigration covers the load-bearing
// invariant: the owner role UUID hardcoded in the sole-owner SQL
// MUST match the seed row in migration 000007_rbac_data_model.up.sql.
// If this constant drifts, every DELETE /users/me silently returns 204
// (no sole-owner case ever matches), which is a silent security
// regression. Pinning here forces a test-suite failure on accidental
// edit.
func TestSystemOwnerRoleID_PinnedToMigration(t *testing.T) {
	require.Equal(t, "00000000-0000-0000-0000-000000000001", systemOwnerRoleID,
		"systemOwnerRoleID drift — must match migration 000007 seed")
}
