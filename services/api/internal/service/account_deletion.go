// Package service — account_deletion.go
//
// AccountDeletionService implements ACCT-03 + ACCT-05: a 30-day soft-
// delete grace period that closes the 152-ФЗ Art. 21 "right to erasure"
// requirement AND the user-recovery window in a single timer.
//
// Five orchestration methods:
//
//   - RequestDeletion (D-31, T-DEL-02/03/07): verify password (constant-
//     time), enumerate sole-owner businesses, then inside ONE PG TX:
//     soft-delete user + revoke pending invitations + enqueue confirmation
//     email (immediate) + enqueue T-7 warning (deferred +23 days).
//     Audit row written async after commit.
//   - CancelDeletion (D-32): atomic UPDATE...RETURNING that clears
//     deleted_at iff the user is inside the 30-day window. Best-effort
//     cancellation of the pending T-7 outbox row.
//   - HardDeleteSweeper (D-31, T-DEL-01/04): hourly cron entry. Claims a
//     batch via FOR UPDATE SKIP LOCKED, hard-deletes each user inside its
//     own TX (audit row INSERT BEFORE the DELETE — survives via
//     ACCT-06 SET NULL + user_email_at_event), then best-effort Mongo
//     cleanup post-TX.
//   - WarningSweeper (D-35, T-DEL-08): 6h cron entry. Enumerates users
//     whose deletion_requested_at falls in the T-7 window
//     (22d23h..23d) and enqueues a single warning per user via
//     ExistsBySubjectAndRecipient dedupe.
//   - EnumerateSoleOwnerBusinesses: read-only enumeration so the handler
//     can return the friendly 409 with the businesses list.
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
	// (D-31). Bundled into one constant because 152-ФЗ Art. 21 fixes
	// the 30-day operator deadline; we use that same duration as the
	// user-recoverable window so the timer aligns with the legal floor.
	deletionGraceDays = 30
	// deletionT7OffsetDays is the offset from deletion_requested_at when
	// the T-7 warning email is enqueued via EnqueueDeferred (D-35).
	// 23 = 30 (grace) - 7 (T-7).
	deletionT7OffsetDays = 23
	// systemOwnerRoleID — Phase 1 RBAC seed (migration 000007). The
	// owner role's UUID is hardcoded because the FK target is fixed at
	// migration time and never changes.
	systemOwnerRoleID = "00000000-0000-0000-0000-000000000001"
)

// SoleOwnerBusiness is one entry in the 409 response body when DELETE
// /users/me is rejected because the user is the sole OWNER. The handler
// marshals this slice directly.
type SoleOwnerBusiness struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ErrSoleOwnerBusinesses is the typed error returned by RequestDeletion
// when the user is the sole OWNER of at least one business. Carries the
// list so the handler can write the 409 body without a second query.
type ErrSoleOwnerBusinesses struct {
	Businesses []SoleOwnerBusiness
}

// Error implements error.
func (e *ErrSoleOwnerBusinesses) Error() string {
	return fmt.Sprintf("account deletion: user is sole owner of %d business(es)", len(e.Businesses))
}

// AccountDeletionUserRepo is the slice of UserRepository surface
// AccountDeletionService needs. Declared as an interface so the
// integration tests can substitute the real adapter; production wires
// *repository.UserResetExtAdapter (the same value that satisfies
// service.UserRepoForReset and service.VerifyUserRepo).
type AccountDeletionUserRepo interface {
	GetByIDIncludingDeleted(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	RequestDeletionInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
	CancelDeletion(ctx context.Context, userID uuid.UUID, graceDays int) (bool, error)
	EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error)
	EnumerateUpcomingDeletions(ctx context.Context, fromTime, toTime time.Time, limit int) ([]*domain.User, error)
	HardDeleteInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
}

// AccountDeletionService — see file docstring.
type AccountDeletionService struct {
	pool          *pgxpool.Pool
	users         AccountDeletionUserRepo
	conversations domain.ConversationRepository
	outbox        *repository.EmailOutboxRepository
	auditLog      audit.Logger
	graceDays     int
	t7OffsetDays  int
}

// NewAccountDeletionService constructs the Phase 21-04 service. All
// deps are required. graceDays + t7OffsetDays are passed as parameters
// (rather than reading the constant) so integration tests can compress
// the timeline to seconds-scale.
func NewAccountDeletionService(
	pool *pgxpool.Pool,
	users AccountDeletionUserRepo,
	conversations domain.ConversationRepository,
	outbox *repository.EmailOutboxRepository,
	auditLogger audit.Logger,
) *AccountDeletionService {
	return &AccountDeletionService{
		pool:          pool,
		users:         users,
		conversations: conversations,
		outbox:        outbox,
		auditLog:      auditLogger,
		graceDays:     deletionGraceDays,
		t7OffsetDays:  deletionT7OffsetDays,
	}
}

// WithGraceDays returns a copy of the service with custom grace + T-7
// offset durations — for integration tests that need to compress the
// 30-day timeline.
func (s *AccountDeletionService) WithGraceDays(graceDays, t7OffsetDays int) *AccountDeletionService {
	cp := *s
	cp.graceDays = graceDays
	cp.t7OffsetDays = t7OffsetDays
	return &cp
}

// RequestDeletion verifies the password, enumerates sole-owner
// businesses, and on success soft-deletes the user inside a single PG
// TX alongside pending-invitation revocation + outbox enqueue (immediate
// confirmation + deferred T-7 warning).
//
// The `reason` parameter (Phase 22 / D-13) selects between the two
// supported entry paths:
//   - reason == "" — DELETE /users/me (Phase 21-04 default). Verifies
//     the bcrypt password (T-DEL-07).
//   - reason == "consent_withdrawn" — POST /users/me/consents/pdn/
//     withdraw (Phase 22). SKIPS the password check (152-ФЗ Art. 21
//     forbids friction barriers on the withdrawal path; D-14). The
//     caller (ConsentService.WithdrawPDN) is the only legitimate user
//     of this branch — see T-22-11 in the threat model.
//
// Returns:
//   - nil on success
//   - domain.ErrInvalidCredentials when password doesn't match (T-DEL-07; reason="" only)
//   - domain.ErrUserNotFound when the user doesn't exist
//   - domain.ErrDeletionAlreadyPending when deletion is already pending
//   - *ErrSoleOwnerBusinesses when the user is the sole OWNER of any business
func (s *AccountDeletionService) RequestDeletion(ctx context.Context, userID uuid.UUID, password, clientIP, userAgent, reason string) error {
	user, err := s.users.GetByIDIncludingDeleted(ctx, userID)
	if err != nil {
		return err
	}
	// Idempotency guard — D-31. If deletion already pending and not
	// canceled, surface ErrDeletionAlreadyPending so the handler returns
	// 423 instead of double-scheduling.
	if user.DeletionRequestedAt != nil && user.DeletionCanceledAt == nil {
		return domain.ErrDeletionAlreadyPending
	}

	// T-DEL-07 / D-13: constant-time bcrypt compare via the same
	// primitive ChangePassword uses. Phase 22 SKIPS this check when
	// reason=="consent_withdrawn" — the consent withdrawal flow already
	// authed via session + cannot present a friction barrier per
	// 152-ФЗ Art. 21.
	if reason != "consent_withdrawn" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			return domain.ErrInvalidCredentials
		}
	}

	// T-DEL-02: enumerate sole-owner businesses BEFORE issuing any
	// mutation. If non-empty, return the friendly 409 path WITHOUT
	// touching the users row — the migration 000007 DB trigger remains
	// as defense-in-depth, but the user never sees the raw trigger
	// error (23P01).
	soleOwners, err := s.EnumerateSoleOwnerBusinesses(ctx, userID)
	if err != nil {
		return fmt.Errorf("enumerate sole-owner businesses: %w", err)
	}
	if len(soleOwners) > 0 {
		// Audit the blocked attempt — telemetry-grade visibility for
		// the friendly 409 path (T-DEL-02 mitigation).
		businessIDs := make([]uuid.UUID, len(soleOwners))
		for i, b := range soleOwners {
			businessIDs[i] = b.ID
		}
		audit.LogSoleOwnerBlocked(ctx, s.auditLog, userID, clientIP, userAgent, businessIDs)
		return &ErrSoleOwnerBusinesses{Businesses: soleOwners}
	}

	// All checks passed — open TX and commit the lifecycle change.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin deletion tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// a) Soft-delete the user row.
	if err := s.users.RequestDeletionInTx(ctx, tx, userID); err != nil {
		return err
	}

	// b) Revoke pending invitations sent by this user (T-DEL-03).
	// Schema uses `created_by` (not `invited_by`) per migration 000007.
	// Pending = revoked_at IS NULL AND accepted_at IS NULL AND expires_at > NOW().
	if _, err := tx.Exec(ctx, `UPDATE invitations
	                              SET revoked_at = NOW()
	                            WHERE created_by = $1
	                              AND revoked_at IS NULL
	                              AND accepted_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoke pending invitations: %w", err)
	}

	scheduledDeletionAt := time.Now().Add(time.Duration(s.graceDays) * 24 * time.Hour)

	// c) Enqueue confirmation email (immediate — default next_attempt_at=NOW()).
	confirmIn := repository.OutboxEnqueueInput{
		ToEmail:  user.Email,
		Subject:  templates.DeletionConfirmationSubject,
		BodyText: templates.DeletionConfirmationText(user.Email, scheduledDeletionAt),
		BodyHTML: templates.DeletionConfirmationHTML(user.Email, scheduledDeletionAt),
	}
	if _, err := s.outbox.Enqueue(ctx, tx, confirmIn); err != nil {
		return fmt.Errorf("enqueue confirmation email: %w", err)
	}

	// d) Enqueue T-7 warning email (deferred to +23 days). The warning
	// sweeper is a safety net that runs separately and dedupes via
	// ExistsBySubjectAndRecipient — but enqueueing here at request
	// time gives us atomicity (same TX as the deletion request).
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

	// Audit row fires fire-and-forget post-commit (uses the existing
	// async Logger pathway). businesses_orphaned is always [] for v1.4
	// because we returned 409 above on any sole-owner case.
	audit.LogDeletionRequested(ctx, s.auditLog, userID, clientIP, userAgent, nil)
	return nil
}

// CancelDeletion runs the atomic UPDATE...RETURNING via the repository,
// best-effort cancels the pending T-7 outbox row, and fires the
// account.deletion_canceled audit event on success.
//
// Returns nil on success; domain.ErrAlreadyPurged when past 30d or row
// gone; domain.ErrNoDeletionPending when the user had no pending
// deletion.
func (s *AccountDeletionService) CancelDeletion(ctx context.Context, userID uuid.UUID, clientIP, userAgent string) error {
	canceled, err := s.users.CancelDeletion(ctx, userID, s.graceDays)
	if err != nil {
		return err
	}
	if !canceled {
		// Defensive — CancelDeletion should have surfaced one of the
		// sentinels already, but cover the case where the repo
		// returned (false, nil).
		return domain.ErrAlreadyPurged
	}

	// Best-effort cancel of the pending T-7 outbox row so the user
	// doesn't receive a warning email after restoring. Failure is
	// logged but not fatal (the worker will see status='canceled' on
	// next drain and skip the row).
	user, getErr := s.users.GetByIDIncludingDeleted(ctx, userID)
	if getErr == nil && user != nil {
		if cancelErr := s.outbox.CancelPendingBySubjectAndRecipient(ctx, user.Email, templates.DeletionT7WarningSubject); cancelErr != nil {
			slog.WarnContext(ctx, "cancel pending T-7 warning failed (non-fatal)", "userID", userID, "err", cancelErr)
		}
	}

	audit.LogDeletionCanceled(ctx, s.auditLog, userID, clientIP, userAgent)
	return nil
}

// EnumerateSoleOwnerBusinesses returns the list of businesses where the
// user holds the system owner role AND no other owner exists. Hardcoded
// owner role UUID (systemOwnerRoleID) — Phase 1 RBAC seed (migration
// 000007). Pool-based: read-only, no tx needed.
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

// GetScheduledDeletionAt returns the moment hard-delete will fire for
// the given user (= deletion_requested_at + graceDays). Used by the
// handler when it needs to render the 423 body's `deletionDate` field
// for an already-pending deletion. Returns zero time + nil if no
// pending deletion exists.
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

// HardDeleteSweeper runs the per-tick batch hard-delete cycle. Returns
// the count of users actually deleted (for logging).
//
// Algorithm:
//  1. Open outer TX, claim up to `batchSize` user IDs via FOR UPDATE
//     SKIP LOCKED so concurrent cancel calls can race-win the row.
//  2. For each ID: re-read inside the tx (defense-in-depth against
//     T-DEL-04 — cancel may have flipped deletion_canceled_at between
//     claim and processing), then INSERT audit row + DELETE user (in
//     that order; FK SET NULL needs the row before DELETE fires).
//  3. Commit. Then post-TX, best-effort Mongo cleanup for each
//     deleted user (T-DEL-05: PG is source of truth; Mongo failure
//     logged but not rolled back).
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
		// Re-read inside the tx (defense-in-depth T-DEL-04). If a
		// concurrent cancel flipped deletion_canceled_at after we
		// claimed the lock, skip.
		user, err := s.users.GetByIDIncludingDeleted(ctx, uid)
		if err != nil {
			slog.WarnContext(ctx, "hard delete sweeper: get user failed", "userID", uid, "err", err)
			continue
		}
		if user.DeletionCanceledAt != nil {
			continue
		}
		originalEmail := user.Email

		// Audit INSERT FIRST so the FK SET NULL has a row to point at
		// after the DELETE fires (ACCT-06).
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

	// Post-TX: best-effort Mongo cleanup. PG is source of truth; Mongo
	// failure is logged but does NOT cause the PG delete to roll back
	// (PG is already committed at this point — T-DEL-05 disposition).
	for _, p := range purged {
		if _, err := s.conversations.MongoConversationsCleanup(ctx, p.userID.String(), p.email); err != nil {
			slog.WarnContext(ctx, "mongo conversations cleanup failed after hard delete (PG row already gone)",
				"userID", p.userID, "err", err)
		}
	}

	return len(purged), nil
}

// WarningSweeper enqueues the T-7 warning email for users whose
// deletion_requested_at falls inside the window
// (requestedAt > now - 23d AND requestedAt <= now - 22d23h).
// Returns the count of emails newly enqueued.
//
// Dedupes via ExistsBySubjectAndRecipient — running the sweeper twice
// yields exactly ONE outbox row per user (T-DEL-08).
//
// Note: the request-deletion path ALREADY enqueues a deferred T-7
// warning via EnqueueDeferred, so this sweeper is a safety net for
// cases where the deferred row was lost (e.g. status='canceled' wave
// after a cancel-then-re-request flow).
func (s *AccountDeletionService) WarningSweeper(ctx context.Context) (int, error) {
	// Window: users requested between (T-23d, T-22d23h] ago — 1h wide.
	now := time.Now()
	fromTime := now.Add(-time.Duration(s.t7OffsetDays)*24*time.Hour - time.Hour)
	toTime := now.Add(-time.Duration(s.t7OffsetDays) * 24 * time.Hour)

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
		// nil tx is intentional — sweeper acts outside any user-
		// initiated transaction. The Enqueue extension in 21-04 adds
		// nil-tx fallback via pool.Exec.
		if _, err := s.outbox.Enqueue(ctx, nil, in); err != nil {
			slog.WarnContext(ctx, "warning sweeper: enqueue failed", "email", u.Email, "err", err)
			continue
		}
		enqueued++
	}

	return enqueued, nil
}

// Defensive guard so the linter doesn't drop the import — used only by
// ErrSoleOwnerBusinesses, which is referenced via errors.As at the
// handler boundary.
var _ = errors.As
