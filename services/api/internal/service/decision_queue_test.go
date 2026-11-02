package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type queueReviews struct {
	domain.ReviewRepository
	business string
	err      error
	failed   []domain.Review
	receipt  *domain.Review
}

func (r *queueReviews) ListByBusinessID(_ context.Context, business string, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	r.business = business
	if filter.ReplyStatus == domain.ReviewReplyStatusError {
		return r.failed, len(r.failed), r.err
	}
	if filter.ReplyStatus != domain.ReviewReplyStatusPending {
		panic("must exclude skipped")
	}
	return []domain.Review{{ID: "r1", BusinessID: business, ReplyStatus: domain.ReviewReplyStatusPending}}, 1, r.err
}

type queuePending struct{ business, user string }

func (r *queuePending) ListPendingByBusiness(_ context.Context, business, user string) ([]*domain.PendingToolCallBatch, error) {
	r.business = business
	r.user = user
	return []*domain.PendingToolCallBatch{{ID: "b1", BusinessID: business, ConversationID: "c1", UserID: user, Status: domain.ReviewReplyStatusPending, Calls: []domain.PendingCall{{CallID: "a"}, {CallID: "b"}}, ExpiresAt: time.Now().Add(time.Hour)}, {ID: "other-user", BusinessID: business, UserID: uuid.NewString(), Status: domain.ReviewReplyStatusPending, Calls: []domain.PendingCall{{CallID: "foreign"}}, ExpiresAt: time.Now().Add(time.Hour)}, {ID: "expired", Status: domain.ReviewReplyStatusPending, ExpiresAt: time.Now().Add(-time.Hour)}}, nil
}

type queueIntegrations struct{ domain.IntegrationRepository }

func (*queueIntegrations) ListByBusinessID(_ context.Context, business uuid.UUID) ([]domain.Integration, error) {
	return []domain.Integration{{ID: uuid.New(), BusinessID: business, Status: "token_expired"}, {ID: uuid.New(), BusinessID: business, Status: "active"}}, nil
}

type queueSync struct{ domain.SyncStateRepository }

func (*queueSync) ListByBusinessID(_ context.Context, business uuid.UUID) ([]domain.SyncState, error) {
	now := time.Now()
	return []domain.SyncState{{ID: uuid.New(), BusinessID: business, DriftDetected: true, LastCheckedAt: &now}}, nil
}

type queueAudit struct{ domain.AuditLogRepository }

func (*queueAudit) ListByBusiness(_ context.Context, business uuid.UUID, filter domain.AuditLogFilter) ([]domain.AuditLog, error) {
	if filter.Action != "review.auto_replied" {
		panic("automatic provenance required")
	}
	return []domain.AuditLog{{Action: "review.auto_replied", BusinessID: &business}, {Action: "platform.review_replied"}}, nil
}
func TestDecisionQueueAggregatesWithoutSplittingBatches(t *testing.T) {
	reviews := &queueReviews{}
	pending := &queuePending{}
	business := uuid.New()
	user := uuid.New()
	svc := NewDecisionQueueService(reviews, pending, &queueIntegrations{}, &queueSync{}, &queueAudit{})
	queue, err := svc.List(context.Background(), business, user, DecisionAccess{Content: true, Integrations: true, Audit: true})
	require.NoError(t, err)
	require.Equal(t, 4, queue.Total)
	require.Len(t, queue.Items, 4)
	require.Len(t, queue.Automatic, 1)
	require.Equal(t, business.String(), reviews.business)
	require.Equal(t, business.String(), pending.business)
	require.Equal(t, user.String(), pending.user)
	for _, item := range queue.Items {
		if item.Kind == decisionKindApproval {
			require.Equal(t, 2, item.CallCount)
			require.Equal(t, "c1", item.ConversationID)
		}
	}
}
func TestDecisionQueuePermissionsDoNotReadUnauthorizedSources(t *testing.T) {
	queue, err := NewDecisionQueueService(nil, nil, nil, nil, nil).List(context.Background(), uuid.New(), uuid.New(), DecisionAccess{})
	require.NoError(t, err)
	require.Empty(t, queue.Items)
	require.Empty(t, queue.Automatic)
}
func TestDecisionQueueSourceFailureDoesNotBecomeEmptySuccess(t *testing.T) {
	_, err := NewDecisionQueueService(&queueReviews{err: errors.New("offline")}, nil, nil, nil, nil).List(context.Background(), uuid.New(), uuid.New(), DecisionAccess{Content: true})
	require.Error(t, err)
}
func TestReviewSkipPersistsAndAllowsLaterManualReply(t *testing.T) {
	business := uuid.New()
	repo := &stubReviewRepo{review: &domain.Review{ID: "r1", BusinessID: business.String(), ReplyStatus: domain.ReviewReplyStatusPending}}
	svc := NewReviewService(repo, nil, nil, nil, nil, nil)
	require.NoError(t, svc.Skip(context.Background(), business, "r1"))
	require.Equal(t, "skipped", repo.review.ReplyStatus)
	require.ErrorIs(t, svc.Reply(withAutoPublishSignal(context.Background()), business, "r1", "auto"), domain.ErrReviewAlreadyAnswered)
	require.NoError(t, svc.Reply(context.Background(), business, "r1", "manual"))
	require.Equal(t, "replied", repo.review.ReplyStatus)
	require.ErrorIs(t, svc.Skip(context.Background(), uuid.New(), "r1"), domain.ErrReviewNotFound)
	stats := computeSLA([]domain.Review{{ReplyStatus: "skipped"}}, time.Now(), 24)
	require.Zero(t, stats.Unanswered)
}

func (s *stubReviewRepo) SkipPending(ctx context.Context, businessID, id string) error {
	if s.review.BusinessID != businessID || s.review.ReplyStatus != domain.ReviewReplyStatusPending {
		return domain.ErrReviewAlreadyAnswered
	}
	return s.UpdateReply(ctx, id, "", domain.ReviewReplyStatusSkipped, nil)
}

func TestDecisionQueueIncludesFailedRepliesAndCountsThem(t *testing.T) {
	business, user := uuid.New(), uuid.New()
	svc := NewDecisionQueueService(&queueReviews{failed: []domain.Review{{ID: "failed", BusinessID: business.String(), ReplyStatus: domain.ReviewReplyStatusError, ReplyText: "Stored failed reply"}}}, &queuePending{}, nil, nil, nil)
	queue, err := svc.List(context.Background(), business, user, DecisionAccess{Content: true})
	require.NoError(t, err)
	require.Equal(t, 3, queue.Total)
	var failed *domain.Review
	for _, item := range queue.Items {
		if item.Review != nil && item.Review.ID == "failed" {
			failed = item.Review
		}
	}
	require.NotNil(t, failed)
	require.Equal(t, "Stored failed reply", failed.ReplyText)
}

func (r *queueReviews) GetByExternalID(_ context.Context, business, _, _ string) (*domain.Review, error) {
	r.business = business
	if r.receipt == nil {
		return nil, domain.ErrReviewNotFound
	}
	return r.receipt, nil
}
