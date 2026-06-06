// Package service — consent flows. See docs/services/consent.md.
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

// PolicyAccepted carries one (slug, version, sha256) tuple from the client.
type PolicyAccepted struct {
	Slug    string // "tos" | "privacy" | "pdn"
	Version string
	SHA256  string // may be empty when frontend has not yet computed it
}

// PolicyDiff describes one policy that needs re-consent (OldVersion="" means no row exists).
type PolicyDiff struct {
	Slug       string `json:"slug"`
	OldVersion string `json:"oldVersion"`
	NewVersion string `json:"newVersion"`
	SHA256     string `json:"sha256"`
}

// RequiresReconsentInfo is the /auth/me payload returned when at least one policy is stale.
type RequiresReconsentInfo struct {
	Policies []PolicyDiff `json:"policies"`
}

// ConsentRepo is the *repository.UserConsentsRepository slice this service needs (seam for mocks).
type ConsentRepo interface {
	UpsertConsent(ctx context.Context, tx pgx.Tx, in repository.UpsertConsentInput) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]repository.Consent, error)
	MarkWithdrawn(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose string) error
}

// AccountDeletionForConsent is the *AccountDeletionService slice WithdrawPDN needs.
type AccountDeletionForConsent interface {
	RequestDeletion(ctx context.Context, userID uuid.UUID, password, clientIP, userAgent, reason string) error
}

// CurrentVersionFunc returns the build's (version, sha256) for a policy slug.
type CurrentVersionFunc func(slug legalconfig.PolicySlug) (version, sha256 string)

// ConsentService orchestrates the three consent flows. See docs/services/consent.md.
type ConsentService struct {
	pool            *pgxpool.Pool
	consents        ConsentRepo
	accountDeletion AccountDeletionForConsent
	auditLog        audit.Logger
	currentVersion  CurrentVersionFunc
}

// NewConsentService constructs the ConsentService; all deps must be non-nil.
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

// upsertConsents UPSERTs each policy in the supplied tx, returning the
// accumulated purposes plus the first (version, sha256) seen for the audit row.
func (s *ConsentService) upsertConsents(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ip, userAgent string, policies []PolicyAccepted) (purposes []string, firstVersion, firstSHA256 string, err error) {
	purposes = make([]string, 0, len(policies))
	for _, p := range policies {
		if err := s.consents.UpsertConsent(ctx, tx, repository.UpsertConsentInput{
			UserID:        userID,
			Purpose:       p.Slug,
			PolicyVersion: p.Version,
			PolicySHA256:  p.SHA256,
			IP:            ip,
			UserAgent:     userAgent,
		}); err != nil {
			return nil, "", "", err
		}
		purposes = append(purposes, p.Slug)
		if firstVersion == "" {
			firstVersion = p.Version
			firstSHA256 = p.SHA256
		}
	}
	return purposes, firstVersion, firstSHA256, nil
}

// RecordRegistrationConsents UPSERTs consents inside the caller's Register tx (atomic with user row).
// See docs/services/consent.md.
func (s *ConsentService) RecordRegistrationConsents(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ip, userAgent string, policies []PolicyAccepted) error {
	purposes, policyVersion, policySHA256, err := s.upsertConsents(ctx, tx, userID, ip, userAgent, policies)
	if err != nil {
		return fmt.Errorf("record registration consents: %w", err)
	}
	if err := audit.LogConsentRecordedTx(ctx, tx, userID, purposes, policyVersion, policySHA256, ip, userAgent); err != nil {
		return fmt.Errorf("record registration consents audit: %w", err)
	}
	return nil
}

// ReConsent handles POST /auth/consents — own tx, version-validate, UPSERT, audit, commit.
// See docs/services/consent.md.
func (s *ConsentService) ReConsent(ctx context.Context, userID uuid.UUID, ip, userAgent string, policies []PolicyAccepted) error {
	if len(policies) == 0 {
		return domain.ErrConsentMissing
	}
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

	purposes, toVersion, _, err := s.upsertConsents(ctx, tx, userID, ip, userAgent, policies)
	if err != nil {
		return fmt.Errorf("reconsent upsert: %w", err)
	}
	if err := audit.LogConsentReconsentedTx(ctx, tx, userID, purposes, "", toVersion, ip, userAgent); err != nil {
		return fmt.Errorf("reconsent audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconsent commit: %w", err)
	}
	return nil
}

// WithdrawPDN handles POST /users/me/consents/pdn/withdraw — deletion + mark + audit (two tx).
// See docs/services/consent.md.
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

// DiffAgainstCurrent returns PolicyDiff entries for stale slugs (missing = diff, withdrawn = skip).
// See docs/services/consent.md.
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
			diffs = append(diffs, PolicyDiff{
				Slug:       string(slug),
				OldVersion: "",
				NewVersion: want,
				SHA256:     wantSHA,
			})
			continue
		}
		if got.WithdrawnAt != nil {
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
