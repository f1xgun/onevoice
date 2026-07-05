package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// maxBatchReviewIDs bounds one batch-draft / bulk-approve request. A batch-draft
// costs one metered LLM call per review, so an unbounded request could drain
// credits; the cap forces a client to page through large backlogs and keeps the
// per-request cost predictable. It applies whether the caller supplies explicit
// review ids or asks for the auto-selected pending set.
const maxBatchReviewIDs = 50

// draftExamplesLimit is the number of past replied reviews fed to the drafter
// as few-shot examples per batch. It mirrors the sync path's default so a
// batch-drafted reply matches the owner's established tone.
const draftExamplesLimit = 5

// Batch item statuses returned per review so a partial failure never fails the
// whole batch or reports a false aggregate.
const (
	BatchItemStatusDrafted   = "drafted"
	BatchItemStatusPublished = "published"
	BatchItemStatusSkipped   = "skipped"
	BatchItemStatusFailed    = "failed"
)

// BatchItemResult is the per-review outcome of a batch operation. Error is a
// short machine-readable reason for skipped/failed items and empty on success.
type BatchItemResult struct {
	ReviewID string
	Status   string
	Error    string
}

// SingleDrafter is the narrow slice of *ReviewDrafter the batch path needs: it
// drafts one already-resolved review through the shared single-draft path
// (inheriting transborder redaction, per-draft metering, the Yandex clamp, and
// the needs_review flag). Kept as an interface so tests pass a fake without
// standing up an orchestrator client.
type SingleDrafter interface {
	DraftReviewByID(ctx context.Context, business *domain.Business, review *domain.Review, examples []domain.Review) error
}

// BatchDraft generates AI reply drafts for a bounded set of currently-unanswered
// reviews of the caller's business. When reviewIDs is non-empty it drafts only
// those (capped at maxBatchReviewIDs); when empty it auto-selects the pending
// backlog without a ready draft, also capped. Each review is drafted through the
// shared single-draft path, so drafts inherit redaction + metering + the Yandex
// clamp and negative reviews are flagged needs_review. Returns a per-item result
// for every requested review; one failure never fails the batch.
func (s *reviewService) BatchDraft(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]BatchItemResult, error) {
	if s.drafter == nil {
		return nil, fmt.Errorf("review drafting is not configured")
	}
	if err := s.gateBusiness(ctx, businessID); err != nil {
		return nil, err
	}

	business, err := s.businessService.GetByID(ctx, businessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("load business: %w", err)
	}

	targets, results, err := s.resolveDraftTargets(ctx, businessID, reviewIDs)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return results, nil
	}

	examples, err := s.repo.ListRepliedExamples(ctx, businessID.String(), "", draftExamplesLimit)
	if err != nil {
		examples = nil
	}

	for i := range targets {
		review := &targets[i]
		item := BatchItemResult{ReviewID: review.ID, Status: BatchItemStatusDrafted}
		if err := s.drafter.DraftReviewByID(ctx, business, review, examples); err != nil {
			item.Status = BatchItemStatusFailed
			item.Error = err.Error()
		}
		results = append(results, item)
	}
	return results, nil
}

// resolveDraftTargets turns the request into the concrete list of reviews to
// draft plus the pre-filled results for ids that were skipped up front (missing,
// cross-business, or already answered). When reviewIDs is empty it pulls the
// pending-without-draft backlog capped at maxBatchReviewIDs.
func (s *reviewService) resolveDraftTargets(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]domain.Review, []BatchItemResult, error) {
	if len(reviewIDs) == 0 {
		pending, err := s.repo.ListPendingWithoutDraft(ctx, businessID.String(), "", maxBatchReviewIDs)
		if err != nil {
			return nil, nil, fmt.Errorf("list pending without draft: %w", err)
		}
		return pending, make([]BatchItemResult, 0, len(pending)), nil
	}

	capped := reviewIDs
	if len(capped) > maxBatchReviewIDs {
		capped = capped[:maxBatchReviewIDs]
	}

	targets := make([]domain.Review, 0, len(capped))
	results := make([]BatchItemResult, 0, len(capped))
	for _, id := range capped {
		review, err := s.repo.GetByID(ctx, id)
		if err != nil {
			results = append(results, BatchItemResult{
				ReviewID: id, Status: BatchItemStatusSkipped, Error: "not_found",
			})
			continue
		}
		if review.BusinessID != businessID.String() {
			results = append(results, BatchItemResult{
				ReviewID: id, Status: BatchItemStatusSkipped, Error: "not_found",
			})
			continue
		}
		if review.ReplyStatus == domain.ReviewReplyStatusReplied || review.ReplyText != "" {
			results = append(results, BatchItemResult{
				ReviewID: id, Status: BatchItemStatusSkipped, Error: "already_answered",
			})
			continue
		}
		targets = append(targets, *review)
	}
	return targets, results, nil
}

// BulkApprove publishes the stored draft for every POSITIVE review in the
// request (rating > ReviewNeedsReviewMaxRating AND not flagged needs_review AND a
// ready draft exists), each through the same platform-dispatch path a manual
// reply uses — inheriting the HITL dedupe key so a retry never double-posts.
// Negative / needs_review / draftless reviews are skipped and never published.
// reviewIDs must be explicit: bulk-approve is the operator's one click of
// approval over a set they saw, not an unattended sweep. Returns a per-item
// result; one dispatch failure never fails the batch.
func (s *reviewService) BulkApprove(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]BatchItemResult, error) {
	if len(reviewIDs) == 0 {
		return []BatchItemResult{}, nil
	}
	if err := s.gateBusiness(ctx, businessID); err != nil {
		return nil, err
	}

	capped := reviewIDs
	if len(capped) > maxBatchReviewIDs {
		capped = capped[:maxBatchReviewIDs]
	}

	results := make([]BatchItemResult, 0, len(capped))
	for _, id := range capped {
		results = append(results, s.bulkApproveOne(ctx, businessID, id))
	}
	return results, nil
}

// bulkApproveOne evaluates and (when eligible) publishes one stored draft. It is
// the single gate that decides a review is bulk-publishable: the negative /
// needs_review exclusion lives here so a critical reply can never be one-tap
// sent. Reply() is reused for the actual dispatch + persistence so the manual
// and bulk paths share the idempotency and status-write behavior.
func (s *reviewService) bulkApproveOne(ctx context.Context, businessID uuid.UUID, id string) BatchItemResult {
	review, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return BatchItemResult{ReviewID: id, Status: BatchItemStatusSkipped, Error: "not_found"}
	}
	if review.BusinessID != businessID.String() {
		return BatchItemResult{ReviewID: id, Status: BatchItemStatusSkipped, Error: "not_found"}
	}
	if review.ReplyStatus == domain.ReviewReplyStatusReplied || review.ReplyText != "" {
		return BatchItemResult{ReviewID: id, Status: BatchItemStatusSkipped, Error: "already_answered"}
	}
	if !isBulkApprovable(review) {
		return BatchItemResult{ReviewID: id, Status: BatchItemStatusSkipped, Error: "not_positive_or_no_draft"}
	}

	if err := s.Reply(ctx, businessID, id, review.DraftReply); err != nil {
		return BatchItemResult{ReviewID: id, Status: BatchItemStatusFailed, Error: err.Error()}
	}
	return BatchItemResult{ReviewID: id, Status: BatchItemStatusPublished}
}

// isBulkApprovable is the positive-only predicate for one-tap publishing: a
// ready draft must exist, the review must be positive (rating strictly above the
// negative threshold), and it must not be flagged needs_review. A negative
// review carries needs_review AND fails the rating gate, so it is excluded on
// two independent conditions — reverting either one alone still holds the line.
func isBulkApprovable(review *domain.Review) bool {
	if review.DraftStatus != domain.ReviewDraftStatusReady || review.DraftReply == "" {
		return false
	}
	if review.NeedsReview {
		return false
	}
	return review.Rating > domain.ReviewNeedsReviewMaxRating
}
