package service

import (
	"context"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

func (s *reviewService) recordReviewDecision(ctx context.Context, review *domain.Review, replyText string) {
	outcome := "approve"
	if isReadyReviewDraft(review) && review.DraftReply != replyText {
		outcome = "edit"
	}
	s.recordReviewApproval(ctx, review, "decision_recorded", outcome)
}

func (s *reviewService) recordReviewEditSaved(ctx context.Context, review *domain.Review) {
	s.recordReviewApproval(ctx, review, "edit_saved", "")
}

func reviewEditSavedEligible(review *domain.Review, replyText string, recordsDecision bool) bool {
	return recordsDecision && isReadyReviewDraft(review) && review.DraftReply != replyText
}

func isReadyReviewDraft(review *domain.Review) bool {
	return review.DraftStatus == domain.ReviewDraftStatusReady && strings.TrimSpace(review.DraftReply) != ""
}

func (s *reviewService) recordReviewResult(ctx context.Context, review *domain.Review, toolName string, dispatchErr error) {
	outcome := "success"
	if dispatchErr != nil {
		outcome = taskStatusError
	} else if toolName == "" {
		outcome = "not_dispatched"
	}
	s.recordReviewApproval(ctx, review, "send_result", outcome)
}

func (s *reviewService) recordReviewApproval(ctx context.Context, review *domain.Review, action, outcome string) {
	if s.approvalTelemetry == nil || isAutoPublishSignal(ctx) {
		return
	}
	s.approvalTelemetry.RecordApproval(ctx, approvaltelemetry.Event{
		DraftID: "review-reply-" + review.ID, ApprovalID: manualReplyApprovalID(review),
		Kind: "review_reply", Source: "reviews", Action: action, Outcome: outcome,
	})
}
