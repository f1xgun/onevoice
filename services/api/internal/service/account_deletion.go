// Package service — account_deletion.go.
//
// AccountDeletionService implements the 30-day soft-delete grace period that
// closes 152-ФЗ Art. 21 ("right to erasure") and the user-recovery window in
// a single timer. See docs/services/account-deletion.md for business rules,
// lifecycle, threat-model bindings (T-DEL-01..08), and transaction
// boundaries.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/email/templates"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

const (
	// deletionGraceDays is the legal-ceiling-plus-user-recovery window
	// (152-ФЗ Art. 21 30-day operator deadline doubles as the
	// user-recoverable window).
	deletionGraceDays = 30
	// deletionT7OffsetDays is the offset from deletion_requested_at when
	// the T-7 warning email is enqueued via EnqueueDeferred (30 - 7).
	deletionT7OffsetDays = 23
	// defaultWarnScanWindow is the fallback width of the WarningSweeper scan
	// window when no tick interval is injected. It is set wide enough to never
	// be narrower than the production sweep cadence (see cmd/main.go
	// deletionWarningTick), so no user's T-7 moment can fall in a gap between
	// ticks. SetWarnScanWindow overrides it with the actual tick + jitter slack.
	defaultWarnScanWindow = 7 * time.Hour
	// systemOwnerRoleID — RBAC owner-role UUID seeded by migration 000007;
	// hardcoded because the FK target is fixed at migration time.
	systemOwnerRoleID = "00000000-0000-0000-0000-000000000001"
)

// SoleOwnerBusiness is one entry in the 409 response body when
// DELETE /users/me is rejected because the user is the sole OWNER.
type SoleOwnerBusiness struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ErrSoleOwnerBusinesses is the typed error returned by RequestDeletion when
// the user is the sole OWNER of at least one business; carries the list so
// the handler can write the 409 body without a second query.
type ErrSoleOwnerBusinesses struct {
	Businesses []SoleOwnerBusiness
}

// Error implements error.
func (e *ErrSoleOwnerBusinesses) Error() string {
	return fmt.Sprintf("account deletion: user is sole owner of %d business(es)", len(e.Businesses))
}

// AccountDeletionUserRepo is the slice of UserRepository surface
// AccountDeletionService needs. Declared as an interface so integration
// tests can substitute the real adapter; production wires
// *repository.UserResetExtAdapter.
type AccountDeletionUserRepo interface {
	GetByIDIncludingDeleted(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	GetByIDIncludingDeletedInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*domain.User, error)
	RequestDeletionInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
	CancelDeletion(ctx context.Context, userID uuid.UUID, graceDays int) (bool, error)
	EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error)
	EnumerateUpcomingDeletions(ctx context.Context, fromTime, toTime time.Time, limit int) ([]*domain.User, error)
	HardDeleteInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
}

// AccountDeletionService orchestrates request/cancel/sweep flows.
// See docs/services/account-deletion.md.
type AccountDeletionService struct {
	pool          *pgxpool.Pool
	users         AccountDeletionUserRepo
	conversations domain.ConversationRepository
	outbox        *repository.EmailOutboxRepository
	auditLog      audit.Logger
	graceDays     int
	t7OffsetDays  int
	// warnScanWindow is the width of the WarningSweeper scan window. It must be
	// at least as wide as the sweep tick interval; otherwise a user whose T-7
	// moment falls between two ticks is never enumerated and never warned.
	warnScanWindow time.Duration
}

// NewAccountDeletionService constructs the service; all deps are required.
// graceDays / t7OffsetDays come from the package constants.
func NewAccountDeletionService(
	pool *pgxpool.Pool,
	users AccountDeletionUserRepo,
	conversations domain.ConversationRepository,
	outbox *repository.EmailOutboxRepository,
	auditLogger audit.Logger,
) *AccountDeletionService {
	return &AccountDeletionService{
		pool:           pool,
		users:          users,
		conversations:  conversations,
		outbox:         outbox,
		auditLog:       auditLogger,
		graceDays:      deletionGraceDays,
		t7OffsetDays:   deletionT7OffsetDays,
		warnScanWindow: defaultWarnScanWindow,
	}
}

// SetWarnScanWindow ties the WarningSweeper scan window to the actual sweep
// tick interval so the two cannot diverge: the window is set to tick + slack
// for tick jitter, guaranteeing every T-7 moment is covered by some tick.
// Callers should pass the same interval the sweeper is scheduled on. A
// non-positive interval is ignored, leaving the safe default in place.
func (s *AccountDeletionService) SetWarnScanWindow(tick time.Duration) {
	if tick <= 0 {
		return
	}
	const jitterSlack = time.Hour
	s.warnScanWindow = tick + jitterSlack
}

// WithGraceDays returns a copy of the service with custom grace + T-7 offset
// durations — for integration tests that compress the 30-day timeline.
func (s *AccountDeletionService) WithGraceDays(graceDays, t7OffsetDays int) *AccountDeletionService {
	cp := *s
	cp.graceDays = graceDays
	cp.t7OffsetDays = t7OffsetDays
	return &cp
}

// RequestDeletion verifies the password (unless reason="consent_withdrawn"),
// enumerates sole-owner businesses, and on success soft-deletes the user in
// one PG TX alongside invitation revoke + confirmation/T-7 outbox enqueue.
// See docs/services/account-deletion.md for the reason-branch semantics,
// returned errors, and audit hooks.
func (s *AccountDeletionService) RequestDeletion(ctx context.Context, userID uuid.UUID, password, clientIP, userAgent, reason string) error {
	user, err := s.users.GetByIDIncludingDeleted(ctx, userID)
	if err != nil {
		return err
	}
	if user.DeletionRequestedAt != nil && user.DeletionCanceledAt == nil {
		return domain.ErrDeletionAlreadyPending
	}

	if reason != "consent_withdrawn" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			return domain.ErrInvalidCredentials
		}
	}

	soleOwners, err := s.EnumerateSoleOwnerBusinesses(ctx, userID)
	if err != nil {
		return fmt.Errorf("enumerate sole-owner businesses: %w", err)
	}
	if len(soleOwners) > 0 {
		businessIDs := make([]uuid.UUID, len(soleOwners))
		for i, b := range soleOwners {
			businessIDs[i] = b.ID
		}
		audit.LogSoleOwnerBlocked(ctx, s.auditLog, userID, clientIP, userAgent, businessIDs)
		return &ErrSoleOwnerBusinesses{Businesses: soleOwners}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin deletion tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.users.RequestDeletionInTx(ctx, tx, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE invitations
	                              SET revoked_at = NOW()
	                            WHERE created_by = $1
	                              AND revoked_at IS NULL
	                              AND accepted_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoke pending invitations: %w", err)
	}

	scheduledDeletionAt := time.Now().Add(time.Duration(s.graceDays) * 24 * time.Hour)

	confirmIn := repository.OutboxEnqueueInput{
		ToEmail:  user.Email,
		Subject:  templates.DeletionConfirmationSubject,
		BodyText: templates.DeletionConfirmationText(user.Email, scheduledDeletionAt),
		BodyHTML: templates.DeletionConfirmationHTML(user.Email, scheduledDeletionAt),
	}
	if _, err := s.outbox.Enqueue(ctx, tx, confirmIn); err != nil {
		return fmt.Errorf("enqueue confirmation email: %w", err)
	}

	warnIn := repository.OutboxEnqueueInput{
		ToEmail:  user.Email,
		Subject:  templates.DeletionT7WarningSubject,
		BodyText: templates.DeletionT7WarningText(user.Email, scheduledDeletionAt),
		BodyHTML: templates.DeletionT7WarningHTML(user.Email, scheduledDeletionAt),
	}
	t7At := time.Now().Add(time.Duration(s.t7OffsetDays) * 24 * time.Hour)
	if _, err := s.outbox.EnqueueDeferred(ctx, tx, warnIn, t7At); err != nil {
		return fmt.Errorf("enqueue T-7 warning email: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deletion tx: %w", err)
	}

	audit.LogDeletionRequested(ctx, s.auditLog, userID, clientIP, userAgent, nil)
	return nil
}

// CancelDeletion atomically clears deleted_at when inside the 30-day window,
// best-effort cancels the pending T-7 outbox row, and audits the cancel.
// See docs/services/account-deletion.md for returned errors.
func (s *AccountDeletionService) CancelDeletion(ctx context.Context, userID uuid.UUID, clientIP, userAgent string) error {
	canceled, err := s.users.CancelDeletion(ctx, userID, s.graceDays)
	if err != nil {
		return err
	}
	if !canceled {
		return domain.ErrAlreadyPurged
	}

	user, getErr := s.users.GetByIDIncludingDeleted(ctx, userID)
	if getErr == nil && user != nil {
		if cancelErr := s.outbox.CancelPendingBySubjectAndRecipient(ctx, user.Email, templates.DeletionT7WarningSubject); cancelErr != nil {
			slog.WarnContext(ctx, "cancel pending T-7 warning failed (non-fatal)", "userID", userID, "err", cancelErr)
		}
	}

	audit.LogDeletionCanceled(ctx, s.auditLog, userID, clientIP, userAgent)
	return nil
}

// EnumerateSoleOwnerBusinesses returns businesses where the user is the sole
// holder of the system owner role. Pool-based, read-only — no tx needed.
func (s *AccountDeletionService) EnumerateSoleOwnerBusinesses(ctx context.Context, userID uuid.UUID) ([]SoleOwnerBusiness, error) {
	const q = `SELECT b.id, b.name
	             FROM businesses b
	             JOIN business_members m ON m.business_id = b.id
	            WHERE m.user_id = $1
	              AND m.role_id = $2::uuid
	              AND (
	                  SELECT COUNT(*) FROM business_members m2
	                   WHERE m2.business_id = b.id
	                     AND m2.role_id = $2::uuid
	              ) = 1
	            ORDER BY b.name ASC`
	rows, err := s.pool.Query(ctx, q, userID, systemOwnerRoleID)
	if err != nil {
		return nil, fmt.Errorf("sole-owner query: %w", err)
	}
	defer rows.Close()
	var out []SoleOwnerBusiness
	for rows.Next() {
		var b SoleOwnerBusiness
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("sole-owner scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sole-owner rows: %w", err)
	}
	return out, nil
}

// GetScheduledDeletionAt returns deletion_requested_at + graceDays, or zero
// time + nil when no pending deletion exists. Used by the 423 body's
// deletionDate field for an already-pending deletion.
func (s *AccountDeletionService) GetScheduledDeletionAt(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	user, err := s.users.GetByIDIncludingDeleted(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}
	if user.DeletionRequestedAt == nil {
		return time.Time{}, nil
	}
	return user.DeletionRequestedAt.Add(time.Duration(s.graceDays) * 24 * time.Hour), nil
}

// HardDeleteSweeper runs the per-tick batch hard-delete cycle; returns the
// count of users actually deleted. See docs/services/account-deletion.md for
// the audit-before-delete and post-TX Mongo cleanup contract.
func (s *AccountDeletionService) HardDeleteSweeper(ctx context.Context) (int, error) {
	const batchSize = 100
	before := time.Now().Add(-time.Duration(s.graceDays) * 24 * time.Hour)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin sweeper tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userIDs, err := s.users.EnumeratePendingDeletionsInTx(ctx, tx, before, batchSize)
	if err != nil {
		return 0, fmt.Errorf("enumerate pending deletions: %w", err)
	}

	type purgedPair struct {
		userID uuid.UUID
		email  string
	}
	var purged []purgedPair
	for _, uid := range userIDs {
		user, err := s.users.GetByIDIncludingDeletedInTx(ctx, tx, uid)
		if err != nil {
			slog.WarnContext(ctx, "hard delete sweeper: get user failed", "userID", uid, "err", err)
			continue
		}
		if user.DeletionCanceledAt != nil {
			continue
		}
		originalEmail := user.Email

		if err := audit.LogUserSelfDeletedTx(ctx, tx, uid, originalEmail); err != nil {
			slog.ErrorContext(ctx, "hard delete sweeper: audit insert failed", "userID", uid, "err", err)
			continue
		}
		if err := s.users.HardDeleteInTx(ctx, tx, uid); err != nil {
			slog.ErrorContext(ctx, "hard delete sweeper: user delete failed", "userID", uid, "err", err)
			continue
		}
		purged = append(purged, purgedPair{userID: uid, email: originalEmail})
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit sweeper tx: %w", err)
	}

	for _, p := range purged {
		if _, err := s.conversations.MongoConversationsCleanup(ctx, p.userID.String(), p.email); err != nil {
			slog.WarnContext(ctx, "mongo conversations cleanup failed after hard delete (PG row already gone)",
				"userID", p.userID, "err", err)
		}
	}

	return len(purged), nil
}

// warningScanWindow returns the (fromTime, toTime] range the WarningSweeper
// enumerates for the given wall clock. toTime is the T-7 instant; fromTime is
// warnScanWindow earlier. The window width equals warnScanWindow, which is kept
// >= the sweep tick interval so no T-7 moment falls in a between-ticks gap.
func (s *AccountDeletionService) warningScanWindow(now time.Time) (fromTime, toTime time.Time) {
	toTime = now.Add(-time.Duration(s.t7OffsetDays) * 24 * time.Hour)
	fromTime = toTime.Add(-s.warnScanWindow)
	return fromTime, toTime
}

// WarningSweeper enqueues the T-7 warning for users whose
// deletion_requested_at falls inside the T-7 scan window; the window is at
// least as wide as the sweep tick so no due warning falls in a between-ticks
// gap. Dedupes via ExistsBySubjectAndRecipient. See
// docs/services/account-deletion.md.
func (s *AccountDeletionService) WarningSweeper(ctx context.Context) (int, error) {
	fromTime, toTime := s.warningScanWindow(time.Now())

	users, err := s.users.EnumerateUpcomingDeletions(ctx, fromTime, toTime, 1000)
	if err != nil {
		return 0, fmt.Errorf("enumerate upcoming deletions: %w", err)
	}

	enqueued := 0
	for _, u := range users {
		exists, err := s.outbox.ExistsBySubjectAndRecipient(ctx, u.Email, templates.DeletionT7WarningSubject)
		if err != nil {
			slog.WarnContext(ctx, "warning sweeper: exists check failed", "email", u.Email, "err", err)
			continue
		}
		if exists {
			continue
		}
		if u.DeletionRequestedAt == nil {
			continue
		}
		deletionAt := u.DeletionRequestedAt.Add(time.Duration(s.graceDays) * 24 * time.Hour)
		in := repository.OutboxEnqueueInput{
			ToEmail:  u.Email,
			Subject:  templates.DeletionT7WarningSubject,
			BodyText: templates.DeletionT7WarningText(u.Email, deletionAt),
			BodyHTML: templates.DeletionT7WarningHTML(u.Email, deletionAt),
		}
		if _, err := s.outbox.Enqueue(ctx, nil, in); err != nil {
			slog.WarnContext(ctx, "warning sweeper: enqueue failed", "email", u.Email, "err", err)
			continue
		}
		enqueued++
	}

	return enqueued, nil
}

// Defensive guard so the linter doesn't drop the errors import — used only by
// ErrSoleOwnerBusinesses, which is referenced via errors.As at the handler boundary.
var _ = errors.As
