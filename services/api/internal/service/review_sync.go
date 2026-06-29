package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// reviewSyncBatchSize is the per-call review/comment fetch limit. 50 matches
// the upper bound that platform agents accept and gives the syncer headroom
// to ingest a backlog after a long disconnect without paging.
const reviewSyncBatchSize = 50

// reviewSyncDispatchTimeout caps the per-platform NATS request budget for a
// single sync fetch.
const reviewSyncDispatchTimeout = 60 * time.Second

// reviewSyncConcurrency bounds how many (business, platform) pairs SyncAll syncs
// at once. Kept small so a fleet-wide tick spreads load across the platform
// agents (the Yandex RPA agent especially) rather than dispatching every pair
// simultaneously.
const reviewSyncConcurrency = 4

// reviewToolByPlatform maps a platform ID to the tool name that returns
// review/comment-like entries. Not every platform follows the __get_reviews
// suffix — VK uses __get_comments because wall.getComments is the native VK
// API method name and the tool was originally registered for on-demand LLM
// access.
var reviewToolByPlatform = map[string]string{
	a2a.AgentTelegram:       tools.TelegramGetReviews,
	a2a.AgentYandexBusiness: tools.YandexBusinessGetReviews,
	a2a.AgentVK:             tools.VKGetComments,
}

// reviewSupportedPlatforms lists the platforms the syncer should poll.
// Derived from reviewToolByPlatform.
var reviewSupportedPlatforms = func() []string {
	out := make([]string, 0, len(reviewToolByPlatform))
	for p := range reviewToolByPlatform {
		out = append(out, p)
	}
	return out
}()

// DraftPassRunner is the post-upsert hook surface ReviewSyncer calls into.
// *ReviewDrafter satisfies it; tests can pass a fake or nil to disable.
type DraftPassRunner interface {
	GenerateForBusiness(ctx context.Context, businessID uuid.UUID, platform string) error
}

// ReviewSyncer periodically fetches reviews from all active integrations
// that support reviews and upserts them into MongoDB.
type ReviewSyncer struct {
	nc           natsRequester
	integRepo    domain.IntegrationRepository
	reviewRepo   domain.ReviewRepository
	drafter      DraftPassRunner // nil = AI drafts disabled
	syncInterval time.Duration
}

// NewReviewSyncer creates a ReviewSyncer. syncInterval 0 disables the ticker
// but SyncAll can still be called manually. Pass drafter=nil to disable the
// AI-draft post-hook (matches REVIEW_DRAFT_ENABLED=false in config).
func NewReviewSyncer(
	nc *natslib.Conn,
	integRepo domain.IntegrationRepository,
	reviewRepo domain.ReviewRepository,
	drafter DraftPassRunner,
	syncInterval time.Duration,
) *ReviewSyncer {
	var requester natsRequester
	if nc != nil {
		requester = nc
	}
	return &ReviewSyncer{
		nc:           requester,
		integRepo:    integRepo,
		reviewRepo:   reviewRepo,
		drafter:      drafter,
		syncInterval: syncInterval,
	}
}

// Start runs SyncAll immediately, then repeats on syncInterval until ctx is done.
func (s *ReviewSyncer) Start(ctx context.Context) {
	s.runOnce(ctx)
	if s.syncInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.runOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *ReviewSyncer) runOnce(ctx context.Context) {
	if err := s.SyncAll(ctx); err != nil {
		slog.Error("review sync failed", "error", err)
	}
}

// SyncAll fetches reviews for every active integration on supported platforms.
// One NATS request is sent per unique (businessID, platform) pair to avoid
// redundant calls when a business has multiple integrations for the same platform.
func (s *ReviewSyncer) SyncAll(ctx context.Context) error {
	integrations, err := s.integRepo.ListAllActiveByPlatforms(ctx, reviewSupportedPlatforms)
	if err != nil {
		return fmt.Errorf("list integrations: %w", err)
	}

	type key struct {
		businessID uuid.UUID
		platform   string
	}
	seen := make(map[key]bool, len(integrations))
	pairs := make([]key, 0, len(integrations))
	for _, integ := range integrations {
		k := key{businessID: integ.BusinessID, platform: integ.Platform}
		if seen[k] {
			continue
		}
		seen[k] = true
		pairs = append(pairs, k)
	}

	// Sync the unique pairs concurrently, bounded so a fleet-wide tick never
	// fans out an unbounded burst of NATS dispatches at the platform agents.
	sem := make(chan struct{}, reviewSyncConcurrency)
	var wg sync.WaitGroup
	for _, p := range pairs {
		wg.Add(1)
		sem <- struct{}{}
		go func(k key) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.syncOne(ctx, k.businessID, k.platform); err != nil {
				slog.Error("review sync: error syncing integration",
					"business_id", k.businessID,
					"platform", k.platform,
					"error", err,
				)
			}
		}(p)
	}
	wg.Wait()
	return nil
}

// SyncForBusiness fetches reviews for every supported platform that this
// business has at least one active integration for, in parallel. The manual-
// refresh endpoint calls it so the operator can pull fresh data without
// waiting for the next ticker tick.
func (s *ReviewSyncer) SyncForBusiness(ctx context.Context, businessID uuid.UUID) error {
	integrations, err := s.integRepo.ListByBusinessID(ctx, businessID)
	if err != nil {
		return fmt.Errorf("list integrations: %w", err)
	}
	platforms := make(map[string]bool, len(integrations))
	for _, integ := range integrations {
		if integ.Status != domain.IntegrationStatusActive {
			continue
		}
		if _, ok := reviewToolByPlatform[integ.Platform]; ok {
			platforms[integ.Platform] = true
		}
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for platform := range platforms {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := s.syncOne(syncCtx, businessID, p); err != nil {
				slog.Error("review refresh: platform sync failed",
					"business_id", businessID, "platform", p, "error", err,
				)
			}
		}(platform)
	}
	wg.Wait()
	return nil
}

// syncOne fetches reviews for a single (businessID, platform) pair via NATS
// and upserts them into MongoDB.
func (s *ReviewSyncer) syncOne(ctx context.Context, businessID uuid.UUID, platform string) error {
	toolName, ok := reviewToolByPlatform[platform]
	if !ok {
		return fmt.Errorf("no review tool registered for platform %q", platform)
	}

	resp, err := dispatchTool(ctx, s.nc, platform, toolName,
		map[string]interface{}{"limit": float64(reviewSyncBatchSize)}, businessID.String(), reviewSyncDispatchTimeout)
	if err != nil {
		return err
	}

	reviewsRaw, ok := resp.Result["reviews"]
	if !ok {
		reviewsRaw, ok = resp.Result["comments"]
	}
	if !ok {
		return nil
	}
	reviewsList, ok := reviewsRaw.([]interface{})
	if !ok {
		return nil
	}

	reviews := dedupeReviewsByExternalID(reviewsList, businessID.String(), platform)

	upsertCtx, upsertCancel := context.WithTimeout(ctx, 10*time.Second)
	defer upsertCancel()

	if err := s.reviewRepo.BulkUpsert(upsertCtx, reviews); err != nil {
		slog.Error("review sync: bulk upsert failed",
			"business_id", businessID,
			"platform", platform,
			"count", len(reviews),
			"error", err,
		)
		return fmt.Errorf("bulk upsert reviews: %w", err)
	}

	if s.drafter != nil {
		draftCtx, draftCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer draftCancel()
		if err := s.drafter.GenerateForBusiness(draftCtx, businessID, platform); err != nil {
			slog.Warn("review sync: draft pass failed",
				"business_id", businessID,
				"platform", platform,
				"error", err,
			)
		}
	}
	return nil
}

// dedupeReviewsByExternalID converts a tool result's raw review list into
// domain.Reviews, dropping entries with an empty external_id and collapsing
// duplicates on external_id (keep-last, so the freshest copy wins). Platform is
// constant within a single syncOne call, so external_id alone is the natural
// key here. The live Yandex RPA get_reviews re-queries every visible card from
// index 0 on each 'load more' pass, so the same external_id is emitted more than
// once in one batch; without this collapse an unordered BulkWrite would insert
// the same review twice on any non-unique-index path.
func dedupeReviewsByExternalID(reviewsList []interface{}, businessID, platform string) []*domain.Review {
	seen := make(map[string]int, len(reviewsList))
	out := make([]*domain.Review, 0, len(reviewsList))
	for _, r := range reviewsList {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		review := reviewFromMap(m, businessID, platform)
		if review.ExternalID == "" {
			continue
		}
		if idx, ok := seen[review.ExternalID]; ok {
			out[idx] = review
			continue
		}
		seen[review.ExternalID] = len(out)
		out = append(out, review)
	}
	return out
}

// reviewFromMap converts a raw map from a tool result into a domain.Review.
// Handles two shapes:
//   - native review shape (Telegram, Yandex.Business): id, author, rating,
//     text, reply, created_at.
//   - VK comment shape: id (int), from_id, text, date (unix), post_id.
func reviewFromMap(m map[string]interface{}, businessID, platform string) *domain.Review {
	externalID := externalIDFromMap(m, platform)
	text, _ := m["text"].(string)
	reply, _ := m["reply"].(string)

	author, _ := m["author"].(string)
	if author == "" {
		if fromID, ok := metaInt(m, "from_id"); ok {
			author = fmt.Sprintf("vk_user_%d", fromID)
		}
	}

	rating := 0
	switch v := m["rating"].(type) {
	case float64:
		rating = int(v)
	case int:
		rating = v
	}

	createdAt := time.Now()
	if ts, ok := m["created_at"].(string); ok && ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			createdAt = t
		}
	} else if unix, ok := metaInt(m, "date"); ok && unix > 0 {
		createdAt = time.Unix(unix, 0).UTC()
	}

	replyStatus := "pending"
	if reply != "" {
		replyStatus = "replied"
	}

	return &domain.Review{
		ID:           uuid.NewString(),
		BusinessID:   businessID,
		Platform:     platform,
		ExternalID:   externalID,
		AuthorName:   author,
		Rating:       rating,
		Text:         text,
		ReplyText:    reply,
		ReplyStatus:  replyStatus,
		CreatedAt:    createdAt,
		PlatformMeta: domain.PlatformMetaFromMap(m),
	}
}

// externalIDFromMap returns a stable, unique-per-platform identifier for the
// comment/review. For VK the native id is an int scoped to a post, so it must
// be composed with post_id to avoid collisions across posts.
func externalIDFromMap(m map[string]interface{}, platform string) string {
	if s, ok := m["id"].(string); ok && s != "" {
		return s
	}
	id, hasID := metaInt(m, "id")
	if !hasID {
		return ""
	}
	if platform == a2a.AgentVK {
		if postID, ok := metaInt(m, "post_id"); ok {
			return fmt.Sprintf("%d_%d", postID, id)
		}
	}
	return fmt.Sprintf("%d", id)
}
