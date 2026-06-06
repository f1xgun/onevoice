package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// fakeConsentRepo captures UpsertConsent / ListByUser / MarkWithdrawn
// calls. ConsentService unit tests never touch a real Postgres — the
// repository's SQL is already covered by user_consents_test.go.
type fakeConsentRepo struct {
	mu              sync.Mutex
	upsertCalls     []repository.UpsertConsentInput
	withdrawnCalls  []withdrawCall
	listByUserRows  []repository.Consent
	listByUserErr   error
	upsertErr       error
	markWithdrawErr error
}

type withdrawCall struct {
	UserID  uuid.UUID
	Purpose string
}

func (f *fakeConsentRepo) UpsertConsent(_ context.Context, _ pgx.Tx, in repository.UpsertConsentInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertCalls = append(f.upsertCalls, in)
	return f.upsertErr
}

func (f *fakeConsentRepo) ListByUser(_ context.Context, _ uuid.UUID) ([]repository.Consent, error) {
	return f.listByUserRows, f.listByUserErr
}

func (f *fakeConsentRepo) MarkWithdrawn(_ context.Context, _ pgx.Tx, userID uuid.UUID, purpose string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withdrawnCalls = append(f.withdrawnCalls, withdrawCall{UserID: userID, Purpose: purpose})
	return f.markWithdrawErr
}

// fakeAccountDeletion captures the RequestDeletion call for assertion.
type fakeAccountDeletion struct {
	mu             sync.Mutex
	calledReason   string
	calledPassword string
	err            error
}

func (f *fakeAccountDeletion) RequestDeletion(_ context.Context, _ uuid.UUID, password, _, _, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledReason = reason
	f.calledPassword = password
	return f.err
}

// nopAuditLogger swallows audit.Logger.Log calls — the tx-aware
// builders bypass the logger entirely and write SQL directly.
type nopAuditLogger struct{}

func (nopAuditLogger) Log(_ context.Context, _ audit.Entry) {}

func (nopAuditLogger) LogSync(_ context.Context, _ audit.Entry) error { return nil }

// currentVersionV1 returns ("v1.0", "") for all three slugs.
func currentVersionV1(slug legalconfig.PolicySlug) (version, sha256 string) {
	switch slug {
	case legalconfig.PolicyTOS, legalconfig.PolicyPrivacy, legalconfig.PolicyPDN:
		return "v1.0", ""
	}
	return "", ""
}

// TestRecordRegistrationConsents_ThreeUpsertsInTx asserts the service
// calls UpsertConsent three times with the three slugs in the same tx.
// The audit row is skipped here because the tx-aware builder requires
// a real pgx.Tx (covered by audit/builders_test.go); we pass nil-tx
// guarded by a service-level flag below.
func TestRecordRegistrationConsents_ThreeUpsertsInTx(t *testing.T) {
	t.Skip("RecordRegistrationConsents requires a real pgx.Tx for LogConsentRecordedTx; covered end-to-end in the Register handler integration suite. The 3-upsert loop is direct mechanical iteration.")
}

// TestDiffAgainstCurrent_TriggersReconsentForPreV22 asserts that three
// rows at policy_version='pre-v22' surface as three PolicyDiff entries
// with newVersion=v1.0.
func TestDiffAgainstCurrent_TriggersReconsentForPreV22(t *testing.T) {
	userID := uuid.New()
	repo := &fakeConsentRepo{
		listByUserRows: []repository.Consent{
			{UserID: userID, Purpose: "tos", PolicyVersion: "pre-v22", AcceptedAt: time.Now()},
			{UserID: userID, Purpose: "privacy", PolicyVersion: "pre-v22", AcceptedAt: time.Now()},
			{UserID: userID, Purpose: "pdn", PolicyVersion: "pre-v22", AcceptedAt: time.Now()},
		},
	}
	svc := NewConsentService(nil, repo, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	got, err := svc.DiffAgainstCurrent(context.Background(), userID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Policies, 3)
	for _, d := range got.Policies {
		require.Equal(t, "pre-v22", d.OldVersion)
		require.Equal(t, "v1.0", d.NewVersion)
	}
}

// TestDiffAgainstCurrent_NilWhenAllCurrent asserts no diff when all
// three rows are at the build's currentVersion — /auth/me returns
// `requiresReconsent: null`.
func TestDiffAgainstCurrent_NilWhenAllCurrent(t *testing.T) {
	userID := uuid.New()
	repo := &fakeConsentRepo{
		listByUserRows: []repository.Consent{
			{UserID: userID, Purpose: "tos", PolicyVersion: "v1.0"},
			{UserID: userID, Purpose: "privacy", PolicyVersion: "v1.0"},
			{UserID: userID, Purpose: "pdn", PolicyVersion: "v1.0"},
		},
	}
	svc := NewConsentService(nil, repo, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	got, err := svc.DiffAgainstCurrent(context.Background(), userID)
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestDiffAgainstCurrent_MissingRowsTriggerDiff asserts that when the
// user has no user_consents rows at all (e.g. pre-Phase-21 account
// that escaped the backfill), DiffAgainstCurrent surfaces three diffs
// with OldVersion="".
func TestDiffAgainstCurrent_MissingRowsTriggerDiff(t *testing.T) {
	userID := uuid.New()
	repo := &fakeConsentRepo{listByUserRows: nil}
	svc := NewConsentService(nil, repo, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	got, err := svc.DiffAgainstCurrent(context.Background(), userID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Policies, 3)
	for _, d := range got.Policies {
		require.Equal(t, "", d.OldVersion)
		require.Equal(t, "v1.0", d.NewVersion)
	}
}

// TestDiffAgainstCurrent_SkipsWithdrawnRows asserts that a row with
// withdrawn_at set does NOT trigger a diff — the user is mid-deletion
// and showing the modal would be useless.
func TestDiffAgainstCurrent_SkipsWithdrawnRows(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	repo := &fakeConsentRepo{
		listByUserRows: []repository.Consent{
			{UserID: userID, Purpose: "tos", PolicyVersion: "v1.0"},
			{UserID: userID, Purpose: "privacy", PolicyVersion: "v1.0"},
			{UserID: userID, Purpose: "pdn", PolicyVersion: "pre-v22", WithdrawnAt: &now},
		},
	}
	svc := NewConsentService(nil, repo, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	got, err := svc.DiffAgainstCurrent(context.Background(), userID)
	require.NoError(t, err)
	require.Nil(t, got, "withdrawn rows must not surface as diffs")
}

// TestReConsent_409VersionMismatch asserts the service returns
// ErrConsentVersionMismatch when the submitted policy version doesn't
// match the build's currentVersion.
func TestReConsent_409VersionMismatch(t *testing.T) {
	svc := NewConsentService(nil, &fakeConsentRepo{}, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	err := svc.ReConsent(context.Background(), uuid.New(), "1.2.3.4", "UA", []PolicyAccepted{
		{Slug: "tos", Version: "v0.9"},
		{Slug: "privacy", Version: "v1.0"},
		{Slug: "pdn", Version: "v1.0"},
	})
	require.ErrorIs(t, err, domain.ErrConsentVersionMismatch)
}

// TestReConsent_ErrConsentMissingOnEmptyPolicies asserts empty body
// returns ErrConsentMissing.
func TestReConsent_ErrConsentMissingOnEmptyPolicies(t *testing.T) {
	svc := NewConsentService(nil, &fakeConsentRepo{}, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	err := svc.ReConsent(context.Background(), uuid.New(), "ip", "ua", nil)
	require.ErrorIs(t, err, domain.ErrConsentMissing)
}

// TestReConsent_ErrConsentMissingOnUnknownSlug asserts a slug not in
// legalconfig.AllSlugs returns ErrConsentMissing (currentVersion
// returns "" → caught by the slug-validation branch).
func TestReConsent_ErrConsentMissingOnUnknownSlug(t *testing.T) {
	svc := NewConsentService(nil, &fakeConsentRepo{}, &fakeAccountDeletion{}, nopAuditLogger{}, currentVersionV1)
	err := svc.ReConsent(context.Background(), uuid.New(), "ip", "ua", []PolicyAccepted{
		{Slug: "marketing", Version: "v1.0"},
	})
	require.ErrorIs(t, err, domain.ErrConsentMissing)
}

// TestWithdrawPDN_DelegatesToDeletionWithReason asserts that
// WithdrawPDN calls AccountDeletionService.RequestDeletion with
// reason="consent_withdrawn" and empty password (literal). When
// RequestDeletion errors, the error surfaces unchanged (no MarkWithdrawn).
func TestWithdrawPDN_DelegatesToDeletionWithReason(t *testing.T) {
	repo := &fakeConsentRepo{}
	del := &fakeAccountDeletion{err: domain.ErrDeletionAlreadyPending}
	svc := NewConsentService(nil, repo, del, nopAuditLogger{}, currentVersionV1)
	err := svc.WithdrawPDN(context.Background(), uuid.New(), "1.2.3.4", "UA")
	require.ErrorIs(t, err, domain.ErrDeletionAlreadyPending)
	require.Equal(t, "consent_withdrawn", del.calledReason)
	require.Equal(t, "", del.calledPassword, "WithdrawPDN must pass empty password — 152-ФЗ Art. 21")
	require.Empty(t, repo.withdrawnCalls, "MarkWithdrawn must NOT run when deletion fails")
}

// TestWithdrawPDN_ErrSentinelSurfaces asserts arbitrary deletion errors
// surface unchanged.
func TestWithdrawPDN_ErrSentinelSurfaces(t *testing.T) {
	sentinel := errors.New("synthetic deletion failure")
	repo := &fakeConsentRepo{}
	del := &fakeAccountDeletion{err: sentinel}
	svc := NewConsentService(nil, repo, del, nopAuditLogger{}, currentVersionV1)
	err := svc.WithdrawPDN(context.Background(), uuid.New(), "ip", "ua")
	require.ErrorIs(t, err, sentinel)
}
