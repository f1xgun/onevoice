package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// reviewToolByPlatform maps a platform ID to the tool name that returns
// review/comment-like entries. Not every platform follows the __get_reviews
// suffix — VK uses __get_comments because wall.getComments is the native VK
// API method name and the tool was originally registered for on-demand LLM
// access.
var reviewToolByPlatform = map[string]string{
	a2a.AgentTelegram:       "telegram__get_reviews",
	a2a.AgentYandexBusiness: "yandex_business__get_reviews",
	a2a.AgentVK:             "vk__get_comments",
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

// ReviewSyncer periodically fetches reviews from all active integrations
// that support reviews and upserts them into MongoDB.
type ReviewSyncer struct {
	nc           *natslib.Conn
	integRepo    domain.IntegrationRepository
	reviewRepo   domain.ReviewRepository
	syncInterval time.Duration
}

// NewReviewSyncer creates a ReviewSyncer. syncInterval 0 disables the ticker
// but SyncAll can still be called manually.
func NewReviewSyncer(
	nc *natslib.Conn,
	integRepo domain.IntegrationRepository,
	reviewRepo domain.ReviewRepository,
	syncInterval time.Duration,
) *ReviewSyncer {
	return &ReviewSyncer{
		nc:           nc,
		integRepo:    integRepo,
		reviewRepo:   reviewRepo,
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
			// Continue with remaining integrations
		}
	}
	return nil
}

// syncOne fetches reviews for a single (businessID, platform) pair via NATS
// and upserts them into MongoDB.
func (s *ReviewSyncer) syncOne(ctx context.Context, businessID uuid.UUID, platform string) error {
	toolName, ok := reviewToolByPlatform[platform]
	if !ok {
		return fmt.Errorf("no review tool registered for platform %q", platform)
	}

	req := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       toolName,
		Args:       map[string]interface{}{"limit": float64(50)},
		BusinessID: businessID.String(),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	msg, err := s.nc.RequestMsgWithContext(reqCtx, &natslib.Msg{
		Subject: a2a.Subject(platform),
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("nats request to %s: %w", a2a.Subject(platform), err)
	}

	var resp a2a.ToolResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("agent error: %s", resp.Error)
	}

	// Different tools return the list under different keys:
	// telegram/yandex_business use "reviews"; VK's get_comments returns "comments".
	reviewsRaw, ok := resp.Result["reviews"]
	if !ok {
		reviewsRaw, ok = resp.Result["comments"]
	}
	if !ok {
		return nil // no review-like field — nothing to persist
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
		if fromID, ok := intFromMap(m, "from_id"); ok {
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
	} else if unix, ok := intFromMap(m, "date"); ok && unix > 0 {
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
	id, hasID := intFromMap(m, "id")
	if !hasID {
		return ""
	}
	if platform == a2a.AgentVK {
		if postID, ok := intFromMap(m, "post_id"); ok {
			return fmt.Sprintf("%d_%d", postID, id)
		}
	}
	return fmt.Sprintf("%d", id)
}

// intFromMap extracts an integer from a JSON-unmarshalled map, tolerating the
// float64 representation Go uses by default.
func intFromMap(m map[string]interface{}, key string) (int64, bool) {
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}
