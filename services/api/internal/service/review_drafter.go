package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// Drafter tunable defaults / safety limits.
const (
	// defaultDraftMaxExamples / defaultDraftPerPassLimit are the fallback
	// values applied when the constructor receives zero/negative inputs from
	// config.
	defaultDraftMaxExamples  = 5
	defaultDraftPerPassLimit = 10
)

// DraftReplyClient is the narrow surface of *orchestratorclient.Client that
// ReviewDrafter consumes. *orchestratorclient.Client satisfies it; tests pass
// a stub that returns canned responses without binding sockets.
type DraftReplyClient interface {
	DraftReply(ctx context.Context, in orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error)
}

// ReviewDrafter generates AI reply drafts for pending reviews. It is wired
// into ReviewSyncer as a post-upsert hook (see review_sync.go) and runs
// once per (business, platform) per sync pass.
//
// LLM access is via the orchestrator's POST /internal/draft-reply endpoint —
// the api service intentionally does not import pkg/llm, keeping the
// orchestrator as the single source of truth for provider routing, rate
// limiting, and billing.
//
// HTTP plumbing lives in pkg/orchestratorclient so every api→orchestrator
// call shares a single Client + Transport pool.
type ReviewDrafter struct {
	reviewRepo   domain.ReviewRepository
	businessRepo domain.BusinessRepository
	orch         DraftReplyClient
	maxExamples  int
	perPassLimit int
}

// NewReviewDrafter constructs a drafter. The supplied client must already be
// bound to the orchestrator base URL — typically svcs.OrchClient (the
// shared client). maxExamples and perPassLimit are clamped to sensible
// defaults when ≤0. orch must be non-nil; passing nil panics so a wiring
// regression surfaces at boot, not on the first sync tick.
func NewReviewDrafter(
	reviewRepo domain.ReviewRepository,
	businessRepo domain.BusinessRepository,
	orch DraftReplyClient,
	maxExamples, perPassLimit int,
) *ReviewDrafter {
	if orch == nil {
		panic("NewReviewDrafter: orch DraftReplyClient cannot be nil")
	}
	if maxExamples <= 0 {
		maxExamples = defaultDraftMaxExamples
	}
	if perPassLimit <= 0 {
		perPassLimit = defaultDraftPerPassLimit
	}
	return &ReviewDrafter{
		reviewRepo:   reviewRepo,
		businessRepo: businessRepo,
		orch:         orch,
		maxExamples:  maxExamples,
		perPassLimit: perPassLimit,
	}
}

// GenerateForBusiness drafts replies for every pending review of the given
// (businessID, platform) that doesn't yet have a draft. Errors on individual
// reviews are logged and persisted as draft_status="failed" so the next pass
// can retry; the function returns nil unless the whole pass setup fails.
func (d *ReviewDrafter) GenerateForBusiness(ctx context.Context, businessID uuid.UUID, platform string) error {
	pending, err := d.reviewRepo.ListPendingWithoutDraft(ctx, businessID.String(), platform, d.perPassLimit)
	if err != nil {
		return fmt.Errorf("list pending without draft: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	business, err := d.businessRepo.GetByID(ctx, businessID)
	if err != nil {
		return fmt.Errorf("get business: %w", err)
	}

	examples, err := d.reviewRepo.ListRepliedExamples(ctx, businessID.String(), platform, d.maxExamples)
	if err != nil {
		slog.Warn("review draft: replied examples lookup failed",
			"business_id", businessID,
			"platform", platform,
			"error", err,
		)
		examples = nil
	}

	for i := range pending {
		review := &pending[i]
		if err := d.generateOne(ctx, business, review, examples); err != nil {
			slog.Warn("review draft: per-review generation failed",
				"review_id", review.ID,
				"business_id", businessID,
				"platform", platform,
				"error", err,
			)
		}
	}
	return nil
}

// generateOne claims a single review row, calls the orchestrator, and
// persists the outcome. Returns the orchestrator/HTTP error so callers can
// log it; the draft_status update is best-effort and not part of the error.
//
// The claim is a compare-and-swap: two overlapping sync passes (the periodic
// ReviewSyncer ticker and a manual SyncForBusiness call) can each read the same
// pending row before either writes "generating". ClaimDraftForGenerating only
// wins for one of them; the loser observes claimed=false and returns without
// calling the orchestrator, so the LLM never drafts the same review twice.
func (d *ReviewDrafter) generateOne(ctx context.Context, business *domain.Business, review *domain.Review, examples []domain.Review) error {
	claimed, err := d.reviewRepo.ClaimDraftForGenerating(ctx, review.ID)
	if err != nil {
		return fmt.Errorf("claim row: %w", err)
	}
	if !claimed {
		return nil
	}

	reqBody := orchestratorclient.DraftReplyRequest{
		BusinessID:          business.ID.String(),
		BusinessName:        business.Name,
		BusinessCategory:    business.Category,
		BusinessDescription: business.Description,
		Platform:            review.Platform,
		ReviewText:          review.Text,
		Rating:              review.Rating,
		AuthorName:          review.AuthorName,
		Examples:            buildExamples(examples, review.ID),
	}

	draft, err := d.callOrchestrator(ctx, reqBody)
	if err != nil {
		if updErr := d.reviewRepo.UpdateDraft(ctx, review.ID, "", domain.ReviewDraftStatusFailed, err.Error()); updErr != nil {
			slog.Warn("review draft: failed to persist failure status",
				"review_id", review.ID,
				"original_error", err,
				"persist_error", updErr,
			)
		}
		return err
	}

	if err := d.reviewRepo.UpdateDraft(ctx, review.ID, draft, domain.ReviewDraftStatusReady, ""); err != nil {
		return fmt.Errorf("persist draft: %w", err)
	}
	return nil
}

// callOrchestrator POSTs to /internal/draft-reply via the shared
// orchestratorclient.Client and returns the draft text.
func (d *ReviewDrafter) callOrchestrator(ctx context.Context, body orchestratorclient.DraftReplyRequest) (string, error) {
	out, err := d.orch.DraftReply(ctx, body)
	if err != nil {
		return "", fmt.Errorf("post draft-reply: %w", err)
	}
	draft := strings.TrimSpace(out.DraftReply)
	if draft == "" {
		return "", fmt.Errorf("orchestrator returned empty draft")
	}
	return draft, nil
}

// buildExamples projects domain.Review records into the wire shape and drops
// the row matching skipID (so a row that just transitioned reply_status the
// other way doesn't feed itself back as an example).
func buildExamples(src []domain.Review, skipID string) []orchestratorclient.DraftReplyExample {
	out := make([]orchestratorclient.DraftReplyExample, 0, len(src))
	for i := range src {
		ex := src[i]
		if ex.ID == skipID {
			continue
		}
		if strings.TrimSpace(ex.Text) == "" || strings.TrimSpace(ex.ReplyText) == "" {
			continue
		}
		out = append(out, orchestratorclient.DraftReplyExample{
			ReviewText: ex.Text,
			ReplyText:  ex.ReplyText,
			Rating:     ex.Rating,
		})
	}
	return out
}
