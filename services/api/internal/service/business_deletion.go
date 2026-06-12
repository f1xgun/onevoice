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

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/email/templates"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// BusinessDeletionRepo is the tx-aware slice of the business repository that
// BusinessDeletionService consumes. Declared as an interface so tests can
// substitute a double; production wires *repository.BusinessDeletionExtAdapter.
type BusinessDeletionRepo interface {
	GetByIDIncludingDeleted(ctx context.Context, businessID uuid.UUID) (*domain.Business, error)
	RequestDeletionInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error
	CancelDeletion(ctx context.Context, businessID uuid.UUID, graceDays int) (bool, error)
	EnumeratePendingDeletionsInTx(ctx context.Context, tx pgx.Tx, before time.Time, limit int) ([]uuid.UUID, error)
	HardDeleteInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) error
}

// BusinessOwnerChecker reports whether userID holds the system OWNER role in
// businessID. Satisfied by domain.BusinessMembershipRepository.
type BusinessOwnerChecker interface {
	GetByBusinessUser(ctx context.Context, businessID, userID uuid.UUID) (*domain.BusinessMember, error)
}

// BusinessOwnerEmailResolver resolves the requesting owner's email so the
// confirmation + T-7 emails can be addressed. Satisfied by
// domain.UserRepository. Member fan-out is intentionally NOT done — only the
// requesting owner is notified.
type BusinessOwnerEmailResolver interface {
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
}

// BusinessDeletionService orchestrates the organization request/cancel/sweep
// flows. Mirrors AccountDeletionService — same 30-day grace, same audit + email
// structure — but gated to members holding the system OWNER role.
type BusinessDeletionService struct {
	pool          *pgxpool.Pool
	businesses    BusinessDeletionRepo
	members       BusinessOwnerChecker
	users         BusinessOwnerEmailResolver
	conversations domain.ConversationRepository
	outbox        *repository.EmailOutboxRepository
	auditLog      audit.Logger
	graceDays     int
	t7OffsetDays  int
}

// NewBusinessDeletionService constructs the service; all deps are required.
// graceDays / t7OffsetDays come from the shared deletion constants.
func NewBusinessDeletionService(
	pool *pgxpool.Pool,
	businesses BusinessDeletionRepo,
	members BusinessOwnerChecker,
	users BusinessOwnerEmailResolver,
	conversations domain.ConversationRepository,
	outbox *repository.EmailOutboxRepository,
	auditLogger audit.Logger,
) *BusinessDeletionService {
	return &BusinessDeletionService{
		pool:          pool,
		businesses:    businesses,
		members:       members,
		users:         users,
		conversations: conversations,
		outbox:        outbox,
		auditLog:      auditLogger,
		graceDays:     deletionGraceDays,
		t7OffsetDays:  deletionT7OffsetDays,
	}
}

// WithGraceDays returns a copy of the service with custom grace + T-7 offset
// durations — for integration tests that compress the 30-day timeline.
func (s *BusinessDeletionService) WithGraceDays(graceDays, t7OffsetDays int) *BusinessDeletionService {
	cp := *s
	cp.graceDays = graceDays
	cp.t7OffsetDays = t7OffsetDays
	return &cp
}

// requireOwner returns ErrNotBusinessOwner unless actorUserID holds the system
// OWNER role in businessID. ErrMembershipNotFound also maps to ErrNotBusinessOwner
// (the access middleware already verified membership; a missing row here means a
// non-owner role).
func (s *BusinessDeletionService) requireOwner(ctx context.Context, businessID, actorUserID uuid.UUID) error {
	member, err := s.members.GetByBusinessUser(ctx, businessID, actorUserID)
	if err != nil {
		if errors.Is(err, domain.ErrMembershipNotFound) {
			return domain.ErrNotBusinessOwner
		}
		return fmt.Errorf("load membership: %w", err)
	}
	if member.RoleID.String() != systemOwnerRoleID {
		return domain.ErrNotBusinessOwner
	}
	return nil
}

// RequestDeletion owner-gates then soft-deletes the organization in one PG TX,
// enqueueing the confirmation + deferred T-7 emails to the requesting owner.
// Returns ErrNotBusinessOwner (403), ErrBusinessDeletionAlreadyPending (423),
// or ErrBusinessNotFound (404).
func (s *BusinessDeletionService) RequestDeletion(ctx context.Context, actorUserID, businessID uuid.UUID, clientIP, userAgent string) error {
	if err := s.requireOwner(ctx, businessID, actorUserID); err != nil {
		if errors.Is(err, domain.ErrNotBusinessOwner) {
			audit.LogBusinessNotOwnerBlocked(ctx, s.auditLog, businessID, actorUserID, clientIP, userAgent)
		}
		return err
	}

	business, err := s.businesses.GetByIDIncludingDeleted(ctx, businessID)
	if err != nil {
		return err
	}
	if business.DeletionRequestedAt != nil && business.DeletionCanceledAt == nil {
		return domain.ErrBusinessDeletionAlreadyPending
	}

	ownerEmail := ""
	ownerLocale := ""
	if owner, ownerErr := s.users.GetByID(ctx, actorUserID); ownerErr == nil && owner != nil {
		ownerEmail = owner.Email
		ownerLocale = owner.PreferredLocale
	} else if ownerErr != nil {
		slog.WarnContext(ctx, "resolve owner email failed (sending no email)", "userID", actorUserID, "err", ownerErr)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin business deletion tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.businesses.RequestDeletionInTx(ctx, tx, businessID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE invitations
	                              SET revoked_at = NOW()
	                            WHERE business_id = $1
	                              AND revoked_at IS NULL
	                              AND accepted_at IS NULL`, businessID); err != nil {
		return fmt.Errorf("revoke pending invitations: %w", err)
	}

	scheduledDeletionAt := time.Now().Add(time.Duration(s.graceDays) * 24 * time.Hour)

	if ownerEmail != "" {
		confirmIn := repository.OutboxEnqueueInput{
			ToEmail:  ownerEmail,
			Subject:  templates.BusinessDeletionConfirmationSubject(ownerLocale),
			BodyText: templates.BusinessDeletionConfirmationText(ownerLocale, business.Name, scheduledDeletionAt),
			BodyHTML: templates.BusinessDeletionConfirmationHTML(ownerLocale, business.Name, scheduledDeletionAt),
		}
		if _, err := s.outbox.Enqueue(ctx, tx, confirmIn); err != nil {
			return fmt.Errorf("enqueue confirmation email: %w", err)
		}

		warnIn := repository.OutboxEnqueueInput{
			ToEmail:  ownerEmail,
			Subject:  templates.BusinessDeletionT7WarningSubject(ownerLocale),
			BodyText: templates.BusinessDeletionT7WarningText(ownerLocale, business.Name, scheduledDeletionAt),
			BodyHTML: templates.BusinessDeletionT7WarningHTML(ownerLocale, business.Name, scheduledDeletionAt),
		}
		t7At := time.Now().Add(time.Duration(s.t7OffsetDays) * 24 * time.Hour)
		if _, err := s.outbox.EnqueueDeferred(ctx, tx, warnIn, t7At); err != nil {
			return fmt.Errorf("enqueue T-7 warning email: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit business deletion tx: %w", err)
	}

	audit.LogBusinessDeletionRequested(ctx, s.auditLog, businessID, actorUserID, clientIP, userAgent)
	return nil
}

// CancelDeletion owner-gates then atomically clears deleted_at when inside the
// 30-day window. Returns ErrNotBusinessOwner (403),
// ErrNoBusinessDeletionPending (404), or ErrBusinessAlreadyPurged (410).
func (s *BusinessDeletionService) CancelDeletion(ctx context.Context, actorUserID, businessID uuid.UUID, clientIP, userAgent string) error {
	if err := s.requireOwner(ctx, businessID, actorUserID); err != nil {
		if errors.Is(err, domain.ErrNotBusinessOwner) {
			audit.LogBusinessNotOwnerBlocked(ctx, s.auditLog, businessID, actorUserID, clientIP, userAgent)
		}
		return err
	}

	canceled, err := s.businesses.CancelDeletion(ctx, businessID, s.graceDays)
	if err != nil {
		return err
	}
	if !canceled {
		return domain.ErrBusinessAlreadyPurged
	}

	audit.LogBusinessDeletionCanceled(ctx, s.auditLog, businessID, actorUserID, clientIP, userAgent)
	return nil
}

// GetScheduledDeletionAt returns deletion_requested_at + graceDays, or zero time
// + nil when no pending deletion exists. Used by the 423 body's deletionDate.
func (s *BusinessDeletionService) GetScheduledDeletionAt(ctx context.Context, businessID uuid.UUID) (time.Time, error) {
	business, err := s.businesses.GetByIDIncludingDeleted(ctx, businessID)
	if err != nil {
		return time.Time{}, err
	}
	if business.DeletionRequestedAt == nil {
		return time.Time{}, nil
	}
	return business.DeletionRequestedAt.Add(time.Duration(s.graceDays) * 24 * time.Hour), nil
}

// HardDeleteSweeper runs the per-tick batch hard-delete cycle; returns the count
// of organizations actually deleted. Audit-before-delete in the same TX, Mongo
// cleanup post-commit.
func (s *BusinessDeletionService) HardDeleteSweeper(ctx context.Context) (int, error) {
	const batchSize = 100
	before := time.Now().Add(-time.Duration(s.graceDays) * 24 * time.Hour)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin sweeper tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	businessIDs, err := s.businesses.EnumeratePendingDeletionsInTx(ctx, tx, before, batchSize)
	if err != nil {
		return 0, fmt.Errorf("enumerate pending business deletions: %w", err)
	}

	type purgedPair struct {
		businessID uuid.UUID
		name       string
	}
	var purged []purgedPair
	for _, bid := range businessIDs {
		business, err := s.businesses.GetByIDIncludingDeleted(ctx, bid)
		if err != nil {
			slog.WarnContext(ctx, "business hard delete sweeper: get business failed", "businessID", bid, "err", err)
			continue
		}
		if business.DeletionCanceledAt != nil {
			continue
		}
		originalName := business.Name

		if err := audit.LogBusinessSelfDeletedTx(ctx, tx, bid, originalName); err != nil {
			slog.ErrorContext(ctx, "business hard delete sweeper: audit insert failed", "businessID", bid, "err", err)
			continue
		}
		if err := s.businesses.HardDeleteInTx(ctx, tx, bid); err != nil {
			slog.ErrorContext(ctx, "business hard delete sweeper: business delete failed", "businessID", bid, "err", err)
			continue
		}
		purged = append(purged, purgedPair{businessID: bid, name: originalName})
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit sweeper tx: %w", err)
	}

	for _, p := range purged {
		if _, err := s.conversations.MongoBusinessCleanup(ctx, p.businessID.String(), p.name); err != nil {
			slog.WarnContext(ctx, "mongo business cleanup failed after hard delete (PG row already gone)",
				"businessID", p.businessID, "err", err)
		}
	}

	return len(purged), nil
}
