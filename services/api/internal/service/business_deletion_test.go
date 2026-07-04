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
	"github.com/f1xgun/onevoice/pkg/email/templates"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// fakeOwnerResolver returns a configurable owner user for the T-7 cancel path.
type fakeOwnerResolver struct {
	user *domain.User
	err  error
}

func (f *fakeOwnerResolver) GetByID(_ context.Context, _ uuid.UUID) (*domain.User, error) {
	return f.user, f.err
}

// fakeDeletionOutbox records the (toEmail, subject) pairs passed to the cancel
// call so a test can assert the pending T-7 row was targeted, and tallies the
// per-business cancel calls so a test can assert sibling organizations are not
// over-canceled. Enqueue paths are no-ops — they're only exercised by
// integration tests.
type fakeDeletionOutbox struct {
	canceledSubjectsByEmail map[string][]string
	canceledByBusiness      map[uuid.UUID]int
}

func (f *fakeDeletionOutbox) Enqueue(_ context.Context, _ pgx.Tx, _ repository.OutboxEnqueueInput) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (f *fakeDeletionOutbox) EnqueueDeferred(_ context.Context, _ pgx.Tx, _ repository.OutboxEnqueueInput, _ time.Time) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (f *fakeDeletionOutbox) CancelPendingBusinessT7ByRecipient(_ context.Context, toEmail, subject string, businessID uuid.UUID) error {
	if f.canceledSubjectsByEmail == nil {
		f.canceledSubjectsByEmail = map[string][]string{}
	}
	if f.canceledByBusiness == nil {
		f.canceledByBusiness = map[uuid.UUID]int{}
	}
	f.canceledSubjectsByEmail[toEmail] = append(f.canceledSubjectsByEmail[toEmail], subject)
	f.canceledByBusiness[businessID]++
	return nil
}

// The production adapter must satisfy BusinessDeletionRepo, including the
// tx-aware read the sweeper now uses. A compile-time check so a signature
// drift between the interface and the adapter fails the build, not at runtime.
var _ BusinessDeletionRepo = (*repository.BusinessDeletionExtAdapter)(nil)

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

func (f *fakeBusinessDeletionRepo) GetByIDIncludingDeletedInTx(_ context.Context, _ pgx.Tx, _ uuid.UUID) (*domain.Business, error) {
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
		users:      &fakeOwnerResolver{user: &domain.User{Email: "owner@example.com", PreferredLocale: "ru"}},
		outbox:     &fakeDeletionOutbox{},
		auditLog:   audit.Nop(),
		graceDays:  30,
	}
	require.NoError(t, s.CancelDeletion(context.Background(), uuid.New(), uuid.New(), "1.2.3.4", "ua"))
}

// TestCancelDeletion_CancelsPendingT7Warning is the fail-on-revert guard: a
// successful cancel must cancel the pending deferred T-7 warning row for the
// owner's resolved locale, so the worker doesn't email a live organization 7
// days "before" a deletion that was already called off. Reverting the
// cancelPendingT7Warning call leaves the row uncanceled and fails this test.
func TestCancelDeletion_CancelsPendingT7Warning(t *testing.T) {
	owner := &domain.User{Email: "owner@example.com", PreferredLocale: "ru"}
	outbox := &fakeDeletionOutbox{}
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{canceled: true},
		members:    &fakeOwnerChecker{member: ownerMember(systemOwnerRoleID)},
		users:      &fakeOwnerResolver{user: owner},
		outbox:     outbox,
		auditLog:   audit.Nop(),
		graceDays:  30,
	}

	require.NoError(t, s.CancelDeletion(context.Background(), uuid.New(), uuid.New(), "1.2.3.4", "ua"))

	require.Contains(t, outbox.canceledSubjectsByEmail, owner.Email,
		"T-7 warning was not canceled for the owner on cancel")
	require.Contains(t, outbox.canceledSubjectsByEmail[owner.Email],
		templates.BusinessDeletionT7WarningSubject("ru"),
		"the ru-locale T-7 subject was not canceled")
}

// TestCancelDeletion_UnknownLocaleCancelsBothVariants verifies the safety net:
// when the owner's locale can't be resolved, BOTH the ru and en T-7 subject
// variants are canceled so the locale-dependent pending row is caught.
func TestCancelDeletion_UnknownLocaleCancelsBothVariants(t *testing.T) {
	owner := &domain.User{Email: "owner@example.com"}
	outbox := &fakeDeletionOutbox{}
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{canceled: true},
		members:    &fakeOwnerChecker{member: ownerMember(systemOwnerRoleID)},
		users:      &fakeOwnerResolver{user: owner},
		outbox:     outbox,
		auditLog:   audit.Nop(),
		graceDays:  30,
	}

	require.NoError(t, s.CancelDeletion(context.Background(), uuid.New(), uuid.New(), "1.2.3.4", "ua"))

	subjects := outbox.canceledSubjectsByEmail[owner.Email]
	require.Contains(t, subjects, templates.BusinessDeletionT7WarningSubject("ru"))
	require.Contains(t, subjects, templates.BusinessDeletionT7WarningSubject("en"))
}

// TestCancelDeletion_CancelScopedToOwnBusiness is the fail-on-revert guard for
// the sibling-overcancel bug: an owner with two pending organization deletions
// (orgA + orgB, same owner email + identical locale-keyed T-7 subject) restores
// orgA; the cancel must target ONLY orgA's business_id so orgB's still-scheduled
// T-7 warning survives. Reverting cancelPendingT7Warning to the unscoped
// (to_email, subject)-only cancel makes a single CancelDeletion call address
// both rows; the per-business tally would then be wrong and this test fails.
func TestCancelDeletion_CancelScopedToOwnBusiness(t *testing.T) {
	owner := &domain.User{Email: "owner@example.com", PreferredLocale: "ru"}
	outbox := &fakeDeletionOutbox{}
	s := &BusinessDeletionService{
		businesses: &fakeBusinessDeletionRepo{canceled: true},
		members:    &fakeOwnerChecker{member: ownerMember(systemOwnerRoleID)},
		users:      &fakeOwnerResolver{user: owner},
		outbox:     outbox,
		auditLog:   audit.Nop(),
		graceDays:  30,
	}

	orgA := uuid.New()
	orgB := uuid.New()

	require.NoError(t, s.CancelDeletion(context.Background(), uuid.New(), orgA, "1.2.3.4", "ua"))

	require.Equal(t, 1, outbox.canceledByBusiness[orgA],
		"restoring orgA must cancel exactly orgA's T-7 warning row")
	require.Equal(t, 0, outbox.canceledByBusiness[orgB],
		"orgB's still-scheduled T-7 warning must survive orgA's restore")
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

// fakeObjectPurger records the prefixes passed to DeletePrefix so a test can
// assert hard-delete reclaims the businesses/{id}/ object-storage prefix.
type fakeObjectPurger struct {
	prefixes []string
	err      error
}

func (f *fakeObjectPurger) DeletePrefix(_ context.Context, prefix string) error {
	f.prefixes = append(f.prefixes, prefix)
	return f.err
}

// TestPurgeObjects_DeletesBusinessPrefix is the fail-on-revert guard for the
// 152-ФЗ erasure-completeness gap: hard-deleting an organization must remove its
// publicly-readable storage objects under the businesses/{id}/ prefix. Reverting
// the purgeObjects call in HardDeleteSweeper leaves DeletePrefix uncalled and
// fails this test.
func TestPurgeObjects_DeletesBusinessPrefix(t *testing.T) {
	purger := &fakeObjectPurger{}
	s := &BusinessDeletionService{objectStore: purger}

	businessID := uuid.New()
	s.purgeObjects(context.Background(), businessID)

	require.Equal(t, []string{"businesses/" + businessID.String() + "/"}, purger.prefixes)
}

// TestPurgeObjects_NoStoreIsNoOp verifies the cleanup is skipped (no panic) when
// no object store is wired.
func TestPurgeObjects_NoStoreIsNoOp(t *testing.T) {
	s := &BusinessDeletionService{}
	s.purgeObjects(context.Background(), uuid.New())
}

// spyPendingPurger records every businessID passed to DeleteByBusinessID so a
// test can assert the sweeper's post-commit cleanup purged the org's HITL
// pending tool-call batches.
type spyPendingPurger struct {
	deletedBusinessIDs []string
}

func (s *spyPendingPurger) DeleteByBusinessID(_ context.Context, businessID string) (int64, error) {
	s.deletedBusinessIDs = append(s.deletedBusinessIDs, businessID)
	return 1, nil
}

// noopConvCleanup satisfies the ConversationRepository slice purgeDatastores
// consumes (MongoBusinessCleanup) so the pending-purge wiring can be exercised
// without a Mongo-backed conversation repository. All methods are no-ops; only
// MongoBusinessCleanup is reached by purgeDatastores.
type noopConvCleanup struct{}

func (noopConvCleanup) Create(_ context.Context, _ *domain.Conversation) error { return nil }
func (noopConvCleanup) GetByID(_ context.Context, _ string) (*domain.Conversation, error) {
	return nil, nil
}
func (noopConvCleanup) ListByUserID(_ context.Context, _, _ string, _, _ int) ([]domain.Conversation, error) {
	return nil, nil
}
func (noopConvCleanup) Update(_ context.Context, _ *domain.Conversation) error { return nil }
func (noopConvCleanup) Delete(_ context.Context, _ string) error               { return nil }
func (noopConvCleanup) UpdateProjectAssignment(_ context.Context, _ string, _ *string) error {
	return nil
}
func (noopConvCleanup) UpdateTitleIfPending(_ context.Context, _, _ string) error { return nil }
func (noopConvCleanup) TransitionToAutoPending(_ context.Context, _ string) error { return nil }
func (noopConvCleanup) Pin(_ context.Context, _, _, _ string) error               { return nil }
func (noopConvCleanup) Unpin(_ context.Context, _, _, _ string) error             { return nil }
func (noopConvCleanup) BumpLastMessageAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (noopConvCleanup) SearchTitles(_ context.Context, _, _, _ string, _ *string, _ int) ([]domain.ConversationTitleHit, []string, error) {
	return nil, nil, nil
}
func (noopConvCleanup) ScopedConversationIDs(_ context.Context, _, _ string, _ *string) ([]string, error) {
	return nil, nil
}
func (noopConvCleanup) MongoConversationsCleanup(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (noopConvCleanup) MongoBusinessCleanup(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// TestPurgePendingToolCalls_DeletesBusinessBatches is the fail-on-revert guard
// for the 152-ФЗ business-granularity erasure gap: hard-deleting an organization
// must purge its HITL pending tool-call batches, which carry business_id +
// user_id + a ModelMessages conversation-history PII snapshot and have no live
// read path once the business is gone. Reverting the purgePendingToolCalls call
// in the sweeper's post-commit cleanup leaves DeleteByBusinessID uncalled and
// fails this test.
func TestPurgePendingToolCalls_DeletesBusinessBatches(t *testing.T) {
	pending := &spyPendingPurger{}
	s := &BusinessDeletionService{
		conversations: noopConvCleanup{},
		pending:       pending,
	}

	businessID := uuid.New()
	s.purgeDatastores(context.Background(), []purgedBusiness{{businessID: businessID, name: "Acme"}})

	require.Equal(t, []string{businessID.String()}, pending.deletedBusinessIDs,
		"business hard-delete must purge the org's pending tool-call batches")
}

// TestPurgePendingToolCalls_NoRepoIsNoOp verifies the cleanup is skipped (no
// panic) when no pending repo is wired.
func TestPurgePendingToolCalls_NoRepoIsNoOp(t *testing.T) {
	s := &BusinessDeletionService{}
	s.purgePendingToolCalls(context.Background(), uuid.New())
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
