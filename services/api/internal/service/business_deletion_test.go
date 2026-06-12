package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeBusinessDeletionRepo is an in-memory BusinessDeletionRepo double. It
// avoids the pool by exercising only requireOwner + the non-tx CancelDeletion
// path; the tx paths are covered by integration tests.
type fakeBusinessDeletionRepo struct {
	business        *domain.Business
	canceled        bool
	cancelErr       error
	getErr          error
	requestCalled   bool
	enumerateResult []uuid.UUID
}

func (f *fakeBusinessDeletionRepo) GetByIDIncludingDeleted(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.business, nil
}

func (f *fakeBusinessDeletionRepo) RequestDeletionInTx(_ context.Context, _ pgx.Tx, _ uuid.UUID) error {
	f.requestCalled = true
	return nil
}

func (f *fakeBusinessDeletionRepo) CancelDeletion(_ context.Context, _ uuid.UUID, _ int) (bool, error) {
	return f.canceled, f.cancelErr
}

func (f *fakeBusinessDeletionRepo) EnumeratePendingDeletionsInTx(_ context.Context, _ pgx.Tx, _ time.Time, _ int) ([]uuid.UUID, error) {
	return f.enumerateResult, nil
}

func (f *fakeBusinessDeletionRepo) HardDeleteInTx(_ context.Context, _ pgx.Tx, _ uuid.UUID) error {
	return nil
}

// fakeOwnerChecker returns a configurable membership for requireOwner tests.
type fakeOwnerChecker struct {
	member *domain.BusinessMember
	err    error
}

func (f *fakeOwnerChecker) GetByBusinessUser(_ context.Context, _, _ uuid.UUID) (*domain.BusinessMember, error) {
	return f.member, f.err
}

func ownerMember(roleID string) *domain.BusinessMember {
	return &domain.BusinessMember{RoleID: uuid.MustParse(roleID)}
}

// TestRequireOwner_OwnerRolePasses verifies the OWNER role UUID passes the gate.
func TestRequireOwner_OwnerRolePasses(t *testing.T) {
	s := &BusinessDeletionService{
		members:   &fakeOwnerChecker{member: ownerMember(systemOwnerRoleID)},
		graceDays: 30,
	}
	require.NoError(t, s.requireOwner(context.Background(), uuid.New(), uuid.New()))
}

// TestRequireOwner_NonOwnerRoleBlocked verifies a non-owner role is rejected.
func TestRequireOwner_NonOwnerRoleBlocked(t *testing.T) {
	s := &BusinessDeletionService{
		members:   &fakeOwnerChecker{member: ownerMember(uuid.NewString())},
		graceDays: 30,
	}
	err := s.requireOwner(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotBusinessOwner)
}

// TestRequireOwner_MissingMembershipBlocked maps ErrMembershipNotFound to the
// owner sentinel.
func TestRequireOwner_MissingMembershipBlocked(t *testing.T) {
	s := &BusinessDeletionService{
		members:   &fakeOwnerChecker{err: domain.ErrMembershipNotFound},
		graceDays: 30,
	}
	err := s.requireOwner(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotBusinessOwner)
}

// TestCancelDeletion_NonOwnerBlocked verifies CancelDeletion gates on owner.
func TestCancelDeletion_NonOwnerBlocked(t *testing.T) {
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{},
		members:    &fakeOwnerChecker{member: ownerMember(uuid.NewString())},
		auditLog:   audit.Nop(),
		graceDays:  30,
	}
	err := s.CancelDeletion(context.Background(), uuid.New(), uuid.New(), "1.2.3.4", "ua")
	require.ErrorIs(t, err, domain.ErrNotBusinessOwner)
}

// TestCancelDeletion_OwnerSuccess clears deletion for an owner inside the window.
func TestCancelDeletion_OwnerSuccess(t *testing.T) {
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{canceled: true},
		members:    &fakeOwnerChecker{member: ownerMember(systemOwnerRoleID)},
		auditLog:   audit.Nop(),
		graceDays:  30,
	}
	require.NoError(t, s.CancelDeletion(context.Background(), uuid.New(), uuid.New(), "1.2.3.4", "ua"))
}

// TestCancelDeletion_NotCanceledMapsToPurged returns ErrBusinessAlreadyPurged
// when the repo reports no row was canceled.
func TestCancelDeletion_NotCanceledMapsToPurged(t *testing.T) {
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{canceled: false},
		members:    &fakeOwnerChecker{member: ownerMember(systemOwnerRoleID)},
		auditLog:   audit.Nop(),
		graceDays:  30,
	}
	err := s.CancelDeletion(context.Background(), uuid.New(), uuid.New(), "1.2.3.4", "ua")
	require.ErrorIs(t, err, domain.ErrBusinessAlreadyPurged)
}

// TestGetScheduledDeletionAt computes requestedAt + graceDays.
func TestGetScheduledDeletionAt(t *testing.T) {
	requestedAt := time.Now().Add(-2 * 24 * time.Hour)
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{business: &domain.Business{DeletionRequestedAt: &requestedAt}},
		graceDays:  30,
	}
	got, err := s.GetScheduledDeletionAt(context.Background(), uuid.New())
	require.NoError(t, err)
	require.WithinDuration(t, requestedAt.Add(30*24*time.Hour), got, time.Second)
}

// TestGetScheduledDeletionAt_NoPending returns the zero time.
func TestGetScheduledDeletionAt_NoPending(t *testing.T) {
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{business: &domain.Business{}},
		graceDays:  30,
	}
	got, err := s.GetScheduledDeletionAt(context.Background(), uuid.New())
	require.NoError(t, err)
	require.True(t, got.IsZero())
}

// TestBusinessWithGraceDays_ReturnsCopy mirrors the account-deletion test:
// the override returns a NEW value, leaving the original at defaults.
func TestBusinessWithGraceDays_ReturnsCopy(t *testing.T) {
	s := &BusinessDeletionService{graceDays: 30, t7OffsetDays: 23}
	cp := s.WithGraceDays(1, 0)
	require.Equal(t, 30, s.graceDays, "original mutated")
	require.Equal(t, 23, s.t7OffsetDays, "original mutated")
	require.Equal(t, 1, cp.graceDays)
	require.Equal(t, 0, cp.t7OffsetDays)
}
