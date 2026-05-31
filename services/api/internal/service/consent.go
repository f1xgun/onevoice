// Package service — consent.go
//
// ConsentService orchestrates the
// three consent flows that Register, ReConsentModal, and Withdraw use.
//
// - RecordRegistrationConsents: called inside userService.Register's
// tx. Performs three UpsertConsent calls (tos, privacy, pdn) at the
// build's currentVersion + writes a single tx-scoped audit row via
// LogConsentRecordedTx.
// - ReConsent: opens its own tx. Validates every submitted (slug,
// version) matches legalconfig.CurrentVersion(slug); mismatch returns
// domain.ErrConsentVersionMismatch (handler maps to 409). UPSERTs the
// three rows + writes LogConsentReconsentedTx in the same tx.
// - WithdrawPDN: calls AccountDeletionService.RequestDeletion
// with reason="consent_withdrawn" (skips password check), then opens a
// tx and sets user_consents.withdrawn_at=NOW + writes
// LogConsentWithdrawnTx. ErrDeletionAlreadyPending surfaces unchanged.
// - DiffAgainstCurrent: /auth/me reads the user's three rows and
// returns a slice of PolicyDiff entries when any policy_version is
// stale (or rows missing — pre-v22 backfill case).
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// PolicyAccepted carries one (slug, version, sha256) tuple submitted by
// the client.
type PolicyAccepted struct {
	Slug    string // "tos" | "privacy" | "pdn".
	Version string
	SHA256  string // may be empty (frontend computes from .md file in 22-02).
}

// PolicyDiff describes one policy that needs re-consent: the old version
// the user has on file (or "" when no row exists — pre-v22 backfill case)
// vs the build's current version.
type PolicyDiff struct {
	Slug       string `json:"slug"` // "tos" | "privacy" | "pdn".
	OldVersion string `json:"oldVersion"`
	NewVersion string `json:"newVersion"`
	SHA256     string `json:"sha256"`
}

// RequiresReconsentInfo is the /auth/me payload returned when at least
// one policy is stale. omitempty on the parent field → /auth/me returns
// `"requiresReconsent": null` when no diffs.
type RequiresReconsentInfo struct {
	Policies []PolicyDiff `json:"policies"`
}

// ConsentRepo is the slice of *repository.UserConsentsRepository the
// service needs. Declared as an interface so the unit tests can
// substitute a mock.
type ConsentRepo interface {
	UpsertConsent(ctx context.Context, tx pgx.Tx, in repository.UpsertConsentInput) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]repository.Consent, error)
	MarkWithdrawn(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose string) error
}

// AccountDeletionForConsent is the slice of *AccountDeletionService
// WithdrawPDN needs. Mirrors the handler-side seam.
type AccountDeletionForConsent interface {
	RequestDeletion(ctx context.Context, userID uuid.UUID, password, clientIP, userAgent, reason string) error
}

// CurrentVersionFunc is the build's current-version + sha256 lookup —
// returns (version, sha256) for a slug. The sha256 is empty when the
// policy text loader isn't yet wired (fills it in).
type CurrentVersionFunc func(slug legalconfig.PolicySlug) (version, sha256 string)

// ConsentService — see file docstring.
type ConsentService struct {
	pool            *pgxpool.Pool
	consents        ConsentRepo
	accountDeletion AccountDeletionForConsent
	auditLog        audit.Logger
	currentVersion  CurrentVersionFunc
}

// NewConsentService constructs the ConsentService. All
// dependencies must be non-nil. currentVersion supplies the build's
// active policy versions — typically a closure over legalconfig.
func NewConsentService(
	pool *pgxpool.Pool,
	consents ConsentRepo,
	accountDeletion AccountDeletionForConsent,
	auditLogger audit.Logger,
	currentVersion CurrentVersionFunc,
) *ConsentService {
	return &ConsentService{
		pool:            pool,
		consents:        consents,
		accountDeletion: accountDeletion,
		auditLog:        auditLogger,
		currentVersion:  currentVersion,
	}
}

// RecordRegistrationConsents is the Register-flow entry point. Runs
// inside the caller's tx — does NOT open its own. The Register flow's
// tx-defer-rollback guarantees the consent rows + user row commit
// together (atomic-Register invariant).
//
// policies must contain exactly the three slugs (tos, privacy, pdn) at
// the build's currentVersion — the handler validates this before
// invoking. Loops over the policies and calls UpsertConsent for each;
// writes ONE consolidated LogConsentRecordedTx audit row.
func (s *ConsentService) RecordRegistrationConsents(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ip, userAgent string, policies []PolicyAccepted) error {
	purposes := make([]string, 0, len(policies))
	var policyVersion, policySHA256 string
	for _, p := range policies {
		if err := s.consents.UpsertConsent(ctx, tx, repository.UpsertConsentInput{
			UserID:        userID,
			Purpose:       p.Slug,
			PolicyVersion: p.Version,
			PolicySHA256:  p.SHA256,
			IP:            ip,
			UserAgent:     userAgent,
		}); err != nil {
			return fmt.Errorf("record registration consents: %w", err)
		}
		purposes = append(purposes, p.Slug)
		// Use the first policy's version + sha256 as the canonical pair
		// for the audit row. All three are bumped in lockstep by Phase
		// 22, so the values match across the slice.
		if policyVersion == "" {
			policyVersion = p.Version
			policySHA256 = p.SHA256
		}
	}
	if err := audit.LogConsentRecordedTx(ctx, tx, userID, purposes, policyVersion, policySHA256, ip, userAgent); err != nil {
		return fmt.Errorf("record registration consents audit: %w", err)
	}
	return nil
}

// ReConsent handles POST /auth/consents. Opens its own pgx.Tx, validates
// each submitted (slug, version) matches the build's currentVersion
// (operator may have bumped mid-review — returns ErrConsentVersionMismatch
// → 409 in that race), UPSERTs the three rows, writes the audit row,
// commits.
//
// fromVersion in the audit row is best-effort — for v1.4 we leave it
// empty (no historical pre-v22 lookup) and rely on the user_consents
// row's policy_version delta for forensic reconstruction.
func (s *ConsentService) ReConsent(ctx context.Context, userID uuid.UUID, ip, userAgent string, policies []PolicyAccepted) error {
	if len(policies) == 0 {
		return domain.ErrConsentMissing
	}
	// Validate every submitted version matches the build's currentVersion.
	for _, p := range policies {
		wantVersion, _ := s.currentVersion(legalconfig.PolicySlug(p.Slug))
		if wantVersion == "" {
			return domain.ErrConsentMissing
		}
		if p.Version != wantVersion {
			return domain.ErrConsentVersionMismatch
		}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("reconsent begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	purposes := make([]string, 0, len(policies))
	var toVersion string
	for _, p := range policies {
		if err := s.consents.UpsertConsent(ctx, tx, repository.UpsertConsentInput{
			UserID:        userID,
			Purpose:       p.Slug,
			PolicyVersion: p.Version,
			PolicySHA256:  p.SHA256,
			IP:            ip,
			UserAgent:     userAgent,
		}); err != nil {
			return fmt.Errorf("reconsent upsert: %w", err)
		}
		purposes = append(purposes, p.Slug)
		if toVersion == "" {
			toVersion = p.Version
		}
	}
	if err := audit.LogConsentReconsentedTx(ctx, tx, userID, purposes, "" /* fromVersion best-effort */, toVersion, ip, userAgent); err != nil {
		return fmt.Errorf("reconsent audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconsent commit: %w", err)
	}
	return nil
}

// WithdrawPDN handles POST /users/me/consents/pdn/withdraw. Triggers
// the account-deletion flow with reason="consent_withdrawn"
// (skips password check). On success, sets user_consents.withdrawn_at
// for the pdn row and writes LogConsentWithdrawnTx in the same tx.
//
// ErrDeletionAlreadyPending surfaces unchanged so the handler returns 423.
func (s *ConsentService) WithdrawPDN(ctx context.Context, userID uuid.UUID, ip, userAgent string) error {
	if err := s.accountDeletion.RequestDeletion(ctx, userID, "", ip, userAgent, "consent_withdrawn"); err != nil {
		return err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("withdraw_pdn begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.consents.MarkWithdrawn(ctx, tx, userID, string(legalconfig.PolicyPDN)); err != nil {
		return fmt.Errorf("withdraw_pdn mark: %w", err)
	}
	if err := audit.LogConsentWithdrawnTx(ctx, tx, userID, string(legalconfig.PolicyPDN), ip, userAgent); err != nil {
		return fmt.Errorf("withdraw_pdn audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("withdraw_pdn commit: %w", err)
	}
	return nil
}

// DiffAgainstCurrent returns a slice of PolicyDiff entries for every
// slug whose recorded policy_version differs from the build's
// currentVersion. Returns nil when no diffs (omitempty triggers
// `requiresReconsent: null` in /auth/me).
//
// Missing rows count as a diff (OldVersion == ""). Withdrawn rows are
// skipped — the user is mid-deletion; the modal would be useless.
func (s *ConsentService) DiffAgainstCurrent(ctx context.Context, userID uuid.UUID) (*RequiresReconsentInfo, error) {
	rows, err := s.consents.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("diff against current list: %w", err)
	}

	bySlug := make(map[string]repository.Consent, len(rows))
	for _, c := range rows {
		bySlug[c.Purpose] = c
	}

	var diffs []PolicyDiff
	for _, slug := range legalconfig.AllSlugs() {
		want, wantSHA := s.currentVersion(slug)
		got, ok := bySlug[string(slug)]
		if !ok {
			// No row at all — pre-v22 backfill missing this purpose, or
			// the user predates the table. Trigger re-consent.
			diffs = append(diffs, PolicyDiff{
				Slug:       string(slug),
				OldVersion: "",
				NewVersion: want,
				SHA256:     wantSHA,
			})
			continue
		}
		if got.WithdrawnAt != nil {
			// User withdrew this consent — they're in the deletion
			// flow; no modal.
			continue
		}
		if got.PolicyVersion != want {
			diffs = append(diffs, PolicyDiff{
				Slug:       string(slug),
				OldVersion: got.PolicyVersion,
				NewVersion: want,
				SHA256:     wantSHA,
			})
		}
	}

	if len(diffs) == 0 {
		return nil, nil
	}
	return &RequiresReconsentInfo{Policies: diffs}, nil
}
