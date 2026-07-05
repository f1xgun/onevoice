package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/tools"
	apiplatform "github.com/f1xgun/onevoice/services/api/internal/platform"
)

// Drafter tunable defaults / safety limits.
const (
	// defaultDraftMaxExamples / defaultDraftPerPassLimit are the fallback
	// values applied when the constructor receives zero/negative inputs from
	// config.
	defaultDraftMaxExamples  = 5
	defaultDraftPerPassLimit = 10

	// yandexReplyMaxRunes caps a stored draft targeting Yandex.Business. The RPA
	// reply form accepts at most this many runes, so a draft is clamped before it
	// is persisted rather than failing at publish time. The single-draft path
	// (and the on-demand batch path, which reuses generateOne) share this bound
	// because the orchestrator's max-token budget is a soft byte-ish limit, not a
	// hard rune count.
	yandexReplyMaxRunes = 2500
)

// DraftReplyClient is the narrow surface of *orchestratorclient.Client that
// ReviewDrafter consumes. *orchestratorclient.Client satisfies it; tests pass
// a stub that returns canned responses without binding sockets.
type DraftReplyClient interface {
	DraftReply(ctx context.Context, in orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error)
}

// AutoPublisher is the narrow slice of the review service the autopilot calls to
// publish a drafted reply. *reviewService satisfies it via Reply, which reuses
// the persisted HITL dedupe key so a retry re-sends the SAME key (agent returns
// its cached result, no double public post) and enforces the tenant + soft-delete
// gates. Kept an interface so tests pass a fake without a NATS connection.
type AutoPublisher interface {
	Reply(ctx context.Context, businessID uuid.UUID, id, replyText string) error
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

	// autoPublisher is nil when the review-reply autopilot is not wired: every
	// drafted review then stays pending for HITL, exactly as before. When set,
	// an opted-in positive non-Yandex draft is auto-published best-effort after
	// it is persisted ready. auditLog records the 152-ФЗ auto-publish event;
	// nil falls back to a no-op logger.
	autoPublisher AutoPublisher
	auditLog      audit.Logger
}

// SetAutoPublisher wires the review-reply autopilot after construction (the
// drafter is built before the review service it publishes through, so the
// dependency is injected post-hoc, matching the nil-drafter/nil-nc conventions).
// A nil publisher leaves the autopilot disabled. A nil audit logger is replaced
// with a no-op so an auto-publish still fires but is not recorded.
func (d *ReviewDrafter) SetAutoPublisher(p AutoPublisher, auditLog audit.Logger) {
	d.autoPublisher = p
	if auditLog == nil {
		auditLog = audit.Nop()
	}
	d.auditLog = auditLog
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

// DraftReviewByID drafts a reply for a single review the caller has already
// resolved and authorized (the batch-draft endpoint passes a review it loaded
// under RequireBusinessAccess + soft-delete gate). It funnels through the exact
// single-draft path generateOne uses — the same orchestrator call, so it
// inherits transborder-PDn redaction and per-draft metering — plus the Yandex
// clamp and needs_review flag applied on the shared store path. business and
// examples are fetched once by the caller and passed in so a batch does not
// re-load them per review.
//
// It returns generateOne's outcome directly: the claim compare-and-swap means a
// review another pass is already drafting returns nil without a second LLM
// call, so a batch never double-meters a review the sync ticker is mid-drafting.
func (d *ReviewDrafter) DraftReviewByID(ctx context.Context, business *domain.Business, review *domain.Review, examples []domain.Review) error {
	return d.generateOne(ctx, business, review, examples)
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
		VoiceProfile:        apiplatform.VoiceProfileFromSettings(business.Settings),
		Platform:            review.Platform,
		ReviewText:          review.Text,
		Rating:              review.Rating,
		AuthorName:          review.AuthorName,
		Examples:            buildExamples(examples, review.ID),
	}

	draft, err := d.callOrchestrator(ctx, reqBody)
	if err != nil {
		if updErr := d.reviewRepo.UpdateDraft(ctx, review.ID, "", domain.ReviewDraftStatusFailed, err.Error(), false); updErr != nil {
			slog.Warn("review draft: failed to persist failure status",
				"review_id", review.ID,
				"original_error", err,
				"persist_error", updErr,
			)
		}
		return err
	}

	draft = clampDraftForPlatform(review.Platform, draft)
	needsReview := review.Rating <= domain.ReviewNeedsReviewMaxRating
	if err := d.reviewRepo.UpdateDraft(ctx, review.ID, draft, domain.ReviewDraftStatusReady, "", needsReview); err != nil {
		return fmt.Errorf("persist draft: %w", err)
	}

	d.maybeAutoPublish(ctx, business, review, draft, needsReview)
	return nil
}

// maybeAutoPublish publishes the freshly-drafted reply when the business opted
// into the review-reply autopilot AND the review is a positive, non-needs_review
// draft AND its platform is not excluded (Yandex.Business is beta -> HITL only)
// AND its rating clears the business-configured minRating floor. Everything else
// stays pending for HITL — the default. The publish is best-effort: an error is
// logged and never fails the draft pass, so the draft remains ready and the
// operator can still approve it by hand. Publication reuses the review service's
// idempotent Reply path (persisted dispatch key, tenant + soft-delete gates), so
// a retry over the same review stays exec-1.
func (d *ReviewDrafter) maybeAutoPublish(ctx context.Context, business *domain.Business, review *domain.Review, draft string, needsReview bool) {
	if d.autoPublisher == nil {
		return
	}
	cfg := apiplatform.ReviewAutopilotFromSettings(business.Settings)
	if !autopilotShouldPublish(cfg, review, draft, needsReview) {
		return
	}

	if err := d.autoPublisher.Reply(ctx, business.ID, review.ID, draft); err != nil {
		slog.Warn("review autopilot: auto-publish failed, leaving draft for HITL",
			"review_id", review.ID,
			"business_id", business.ID,
			"platform", review.Platform,
			"error", err,
		)
		return
	}

	audit.LogReviewAutoReplied(ctx, d.auditLog, business.ID, review.Platform, replyToolForPlatform(review.Platform), review.ExternalID)
}

// autopilotShouldPublish is the full auto-publish gate. It is intentionally the
// conjunction of four independent guards so reverting any single one still blocks
// a forbidden auto-publish:
//   - opt-in: the business enabled the autopilot;
//   - positive + ready: a ready non-empty non-needs_review draft above the
//     negative rating threshold (isPositiveDraftReady, shared with bulk-approve);
//   - rating floor: the rating clears the business-configured minRating (already
//     clamped to at least ReviewAutopilotMinRatingFloor at read time);
//   - platform: Yandex.Business is excluded (beta -> HITL only).
//
// draft and needsReview are the values just persisted, so the gate reads the
// exact stored draft rather than re-loading the row.
func autopilotShouldPublish(cfg apiplatform.ReviewAutopilotConfig, review *domain.Review, draft string, needsReview bool) bool {
	if !cfg.Enabled {
		return false
	}
	gate := *review
	gate.DraftStatus = domain.ReviewDraftStatusReady
	gate.DraftReply = draft
	gate.NeedsReview = needsReview
	if !isPositiveDraftReady(&gate) {
		return false
	}
	if review.Rating < cfg.MinRating {
		return false
	}
	if review.Platform == a2a.AgentYandexBusiness {
		return false
	}
	return true
}

// replyToolForPlatform maps a platform to the canonical reply tool name recorded
// in the auto-publish audit event. It is derived from buildPlatformReply's tool
// selection so the audit Tool field matches the tool the dispatch actually used.
// An unknown platform yields "" (the autopilot never reaches an unknown platform
// because the gate excludes Yandex and only Telegram/VK are eligible today).
func replyToolForPlatform(platform string) string {
	switch platform {
	case a2a.AgentVK:
		return tools.VKReplyComment
	case a2a.AgentTelegram:
		return tools.TelegramReplyToComment
	case a2a.AgentGoogleBusiness:
		return tools.GoogleBusinessReplyReview
	}
	return ""
}

// clampDraftForPlatform trims a generated draft to the target platform's reply
// length ceiling before it is persisted. Only Yandex.Business enforces a hard
// rune cap today; other platforms pass through unchanged. Sharing this on the
// single-draft store path means every draft (sync-triggered or batch-triggered,
// which reuses generateOne) is stored within the platform's limit.
func clampDraftForPlatform(platform, draft string) string {
	if platform == a2a.AgentYandexBusiness && utf8.RuneCountInString(draft) > yandexReplyMaxRunes {
		return string([]rune(draft)[:yandexReplyMaxRunes])
	}
	return draft
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
