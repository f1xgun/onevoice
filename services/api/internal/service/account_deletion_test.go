package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

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
