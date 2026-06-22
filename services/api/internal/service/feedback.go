// Package service — feedback.go.
//
// FeedbackService persists in-app user feedback and, when a notification
// address is configured, enqueues an owner-notification email atomically with
// the feedback row (transactional outbox).
package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// FeedbackInput is the validated service-layer feedback submission. The handler
// validates category/message/rating bounds before constructing it.
type FeedbackInput struct {
	Category      string
	Message       string
	Page          string
	Rating        *int16
	UserAgent     string
	CorrelationID string
}

// feedbackRepo is the narrow persistence surface FeedbackService depends on.
type feedbackRepo interface {
	Insert(ctx context.Context, tx pgx.Tx, row repository.ProductFeedbackRow) (uuid.UUID, error)
}

// feedbackOutbox is the narrow outbox surface FeedbackService depends on.
type feedbackOutbox interface {
	Enqueue(ctx context.Context, tx pgx.Tx, in repository.OutboxEnqueueInput) (uuid.UUID, error)
}

// feedbackTxPool is the narrow tx-opening surface (satisfied by *pgxpool.Pool
// and pgxmock) so the tx path is unit-testable.
type feedbackTxPool interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// feedbackUserLookup resolves a submitter's email for the founder
// notification. Satisfied by domain.UserRepository.
type feedbackUserLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

// FeedbackService persists feedback and notifies the founder.
type FeedbackService struct {
	pool        feedbackTxPool
	repo        feedbackRepo
	outbox      feedbackOutbox
	users       feedbackUserLookup
	notifyEmail string
}

// NewFeedbackService constructs a FeedbackService. notifyEmail empty disables
// the owner-notification email (the feedback row is still persisted).
func NewFeedbackService(pool feedbackTxPool, repo feedbackRepo, outbox feedbackOutbox, users feedbackUserLookup, notifyEmail string) *FeedbackService {
	return &FeedbackService{pool: pool, repo: repo, outbox: outbox, users: users, notifyEmail: notifyEmail}
}

// Submit records the feedback row and, when FEEDBACK_NOTIFY_EMAIL is set,
// enqueues a plain-text founder notification inside the same tx so the row and
// the email persist atomically. A uuid.Nil userID is stored as NULL.
func (s *FeedbackService) Submit(ctx context.Context, userID uuid.UUID, in FeedbackInput) error {
	var userPtr *uuid.UUID
	if userID != uuid.Nil {
		userPtr = &userID
	}

	// Resolve the submitter email BEFORE opening the tx so the tx holds a
	// single pooled connection for the minimal insert+enqueue window.
	submitter := ""
	if s.notifyEmail != "" {
		submitter = s.resolveSubmitter(ctx, userID)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("feedback begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := repository.ProductFeedbackRow{
		UserID:    userPtr,
		Category:  in.Category,
		Message:   in.Message,
		Page:      in.Page,
		Rating:    in.Rating,
		UserAgent: in.UserAgent,
	}
	if in.CorrelationID != "" {
		cid := in.CorrelationID
		row.CorrelationID = &cid
	}

	feedbackID, err := s.repo.Insert(ctx, tx, row)
	if err != nil {
		return fmt.Errorf("feedback insert: %w", err)
	}

	if s.notifyEmail != "" {
		if _, err := s.outbox.Enqueue(ctx, tx, s.notification(feedbackID, submitter, in)); err != nil {
			return fmt.Errorf("feedback notify enqueue: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("feedback commit: %w", err)
	}
	return nil
}

// resolveSubmitter best-effort resolves the submitter's email (falling back to
// the user id string) so the founder can reply. Runs outside the tx.
func (s *FeedbackService) resolveSubmitter(ctx context.Context, userID uuid.UUID) string {
	submitter := userID.String()
	if s.users != nil && userID != uuid.Nil {
		if u, uerr := s.users.GetByID(ctx, userID); uerr == nil && u != nil && u.Email != "" {
			submitter = u.Email
		}
	}
	return submitter
}

// notification builds the founder-notification email from a pre-resolved
// submitter identity.
func (s *FeedbackService) notification(feedbackID uuid.UUID, submitter string, in FeedbackInput) repository.OutboxEnqueueInput {
	rating := "—"
	if in.Rating != nil {
		rating = strconv.Itoa(int(*in.Rating))
	}
	page := in.Page
	if page == "" {
		page = "—"
	}
	body := fmt.Sprintf(
		"Новая обратная связь в OneVoice.\n\nКатегория: %s\nОценка: %s\nСтраница: %s\nОт: %s\nID: %s\n\nСообщение:\n%s\n",
		in.Category, rating, page, submitter, feedbackID, in.Message,
	)
	return repository.OutboxEnqueueInput{
		ToEmail:  s.notifyEmail,
		Subject:  fmt.Sprintf("OneVoice feedback [%s]", in.Category),
		BodyText: body,
	}
}
