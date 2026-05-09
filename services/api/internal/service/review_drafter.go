package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Drafter tunable defaults / safety limits.
const (
	// reviewDrafterHTTPTimeout caps the per-call budget toward
	// orchestrator /internal/draft-reply. 60s allows a slow LLM round-trip
	// (cold-start + tokenization on cheap models can spike) without leaving
	// the sync pass hanging if the provider is hard-down.
	reviewDrafterHTTPTimeout = 60 * time.Second

	// errorSnippetMaxBytes caps the bytes we read off a non-2xx response body
	// for inclusion in the wrapped error. Keeps logs tidy and prevents an
	// unbounded body from a misbehaving orchestrator from filling memory.
	errorSnippetMaxBytes = 512

	// defaultDraftMaxExamples / defaultDraftPerPassLimit are the fallback
	// values applied when the constructor receives zero/negative inputs from
	// config.
	defaultDraftMaxExamples  = 5
	defaultDraftPerPassLimit = 10
)

// DraftHTTPClient is the narrow http.Client surface ReviewDrafter needs.
// http.Client satisfies it; tests pass a stub that returns canned responses
// without binding sockets.
type DraftHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ReviewDrafter generates AI reply drafts for pending reviews. It is wired
// into ReviewSyncer as a post-upsert hook (see review_sync.go) and runs
// once per (business, platform) per sync pass.
//
// LLM access is via the orchestrator's POST /internal/draft-reply endpoint —
// the api service intentionally does not import pkg/llm, keeping the
// orchestrator as the single source of truth for provider routing, rate
// limiting, and billing.
type ReviewDrafter struct {
	reviewRepo      domain.ReviewRepository
	businessRepo    domain.BusinessRepository
	httpClient      DraftHTTPClient
	orchestratorURL string
	maxExamples     int
	perPassLimit    int
}

// NewReviewDrafter constructs a drafter. orchestratorURL must NOT include
// the path; the drafter appends "/internal/draft-reply". maxExamples and
// perPassLimit are clamped to sensible defaults when ≤0.
func NewReviewDrafter(
	reviewRepo domain.ReviewRepository,
	businessRepo domain.BusinessRepository,
	httpClient DraftHTTPClient,
	orchestratorURL string,
	maxExamples, perPassLimit int,
) *ReviewDrafter {
	if maxExamples <= 0 {
		maxExamples = defaultDraftMaxExamples
	}
	if perPassLimit <= 0 {
		perPassLimit = defaultDraftPerPassLimit
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: reviewDrafterHTTPTimeout}
	}
	return &ReviewDrafter{
		reviewRepo:      reviewRepo,
		businessRepo:    businessRepo,
		httpClient:      httpClient,
		orchestratorURL: strings.TrimRight(orchestratorURL, "/"),
		maxExamples:     maxExamples,
		perPassLimit:    perPassLimit,
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
		// Don't abort the pass — examples are nice-to-have, the LLM still
		// produces a reasonable draft from business context alone.
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
			// generateOne already persisted draft_status=failed; loop on.
		}
	}
	return nil
}

// generateOne claims a single review row, calls the orchestrator, and
// persists the outcome. Returns the orchestrator/HTTP error so callers can
// log it; the draft_status update is best-effort and not part of the error.
func (d *ReviewDrafter) generateOne(ctx context.Context, business *domain.Business, review *domain.Review, examples []domain.Review) error {
	// Claim the row first so a parallel sync pass skips it. If the claim
	// itself fails (e.g. doc deleted between list and claim) we just bail —
	// the next pass picks it up.
	if err := d.reviewRepo.UpdateDraft(ctx, review.ID, "", domain.ReviewDraftStatusGenerating, ""); err != nil {
		return fmt.Errorf("claim row: %w", err)
	}

	// Build the request to the orchestrator. Examples are filtered to drop
	// any that match the current review (stale data shouldn't loop back).
	reqBody := draftReplyRequest{
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
		// Persist failure — best effort.
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

// callOrchestrator POSTs to /internal/draft-reply and returns the draft text.
func (d *ReviewDrafter) callOrchestrator(ctx context.Context, body draftReplyRequest) (string, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := d.orchestratorURL + "/internal/draft-reply"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post draft-reply: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Cap the body slurp so a misbehaving orchestrator can't OOM us.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errorSnippetMaxBytes))
		return "", fmt.Errorf("orchestrator returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var out draftReplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	draft := strings.TrimSpace(out.DraftReply)
	if draft == "" {
		return "", fmt.Errorf("orchestrator returned empty draft")
	}
	return draft, nil
}

// draftReplyRequest mirrors handler.DraftReplyRequest in services/orchestrator.
// Duplicated to keep the api free of the orchestrator import (otherwise a
// cyclic dep would form once the orchestrator imports pkg/domain types that
// transitively reference api repository helpers).
type draftReplyRequest struct {
	BusinessID          string                `json:"businessId"`
	BusinessName        string                `json:"businessName"`
	BusinessCategory    string                `json:"businessCategory,omitempty"`
	BusinessDescription string                `json:"businessDescription,omitempty"`
	Platform            string                `json:"platform"`
	ReviewText          string                `json:"reviewText"`
	Rating              int                   `json:"rating"`
	AuthorName          string                `json:"authorName,omitempty"`
	Examples            []draftReplyExamplePB `json:"examples,omitempty"`
}

type draftReplyExamplePB struct {
	ReviewText string `json:"reviewText"`
	ReplyText  string `json:"replyText"`
	Rating     int    `json:"rating,omitempty"`
}

type draftReplyResponse struct {
	DraftReply string `json:"draftReply"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
}

// buildExamples projects domain.Review records into the wire shape and drops
// the row matching skipID (so a row that just transitioned reply_status the
// other way doesn't feed itself back as an example).
func buildExamples(src []domain.Review, skipID string) []draftReplyExamplePB {
	out := make([]draftReplyExamplePB, 0, len(src))
	for i := range src {
		ex := src[i]
		if ex.ID == skipID {
			continue
		}
		if strings.TrimSpace(ex.Text) == "" || strings.TrimSpace(ex.ReplyText) == "" {
			continue
		}
		out = append(out, draftReplyExamplePB{
			ReviewText: ex.Text,
			ReplyText:  ex.ReplyText,
			Rating:     ex.Rating,
		})
	}
	return out
}
