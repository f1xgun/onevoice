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
	nc           *natslib.Conn
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
	return &ReviewSyncer{
		nc:           nc,
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

	type key struct{ businessID, platform string }
	seen := make(map[key]bool, len(integrations))

	for _, integ := range integrations {
		k := key{integ.BusinessID.String(), integ.Platform}
		if seen[k] {
			continue
		}
		seen[k] = true

		if err := s.syncOne(ctx, integ.BusinessID, integ.Platform); err != nil {
			slog.Error("review sync: error syncing integration",
				"business_id", integ.BusinessID,
				"platform", integ.Platform,
				"error", err,
			)
		}
	}
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

	upsertCtx, upsertCancel := context.WithTimeout(ctx, 10*time.Second)
	defer upsertCancel()

	for _, r := range reviewsList {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		review := reviewFromMap(m, businessID.String(), platform)
		if review.ExternalID == "" {
			continue
		}
		if err := s.reviewRepo.Upsert(upsertCtx, review); err != nil {
			slog.Error("review sync: upsert failed",
				"business_id", businessID,
				"platform", platform,
				"external_id", review.ExternalID,
				"error", err,
			)
		}
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
		ID:          uuid.NewString(),
		BusinessID:  businessID,
		Platform:    platform,
		ExternalID:  externalID,
		AuthorName:  author,
		Rating:      rating,
		Text:        text,
		ReplyText:   reply,
		ReplyStatus: replyStatus,
		CreatedAt:   createdAt,
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
