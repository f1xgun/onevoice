package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/tools"
	apiplatform "github.com/f1xgun/onevoice/services/api/internal/platform"
)

func newYandexReview() *domain.Review {
	review := positiveTelegramReview()
	review.ID = uuid.NewString()
	review.Platform = "yandex_business"
	review.CreatedAt = time.Now().Add(-time.Hour)
	review.PlatformMeta = map[string]interface{}{apiplatform.YandexReviewPublishedAtKey: review.CreatedAt.Format(time.RFC3339)}
	return review
}

func wireYandexCounter(t *testing.T, drafter *ReviewDrafter) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	drafter.SetAutoPublishRedis(client)
	return server
}

func TestAutopilot_YandexPublishesNewReviewAndRecordsSuccess(t *testing.T) {
	pub, log := &fakeAutoPublisher{}, &captureAuditLogger{}
	drafter := newAutopilotDrafter(t, pub, log, "Спасибо!")
	wireYandexCounter(t, drafter)
	require.NoError(t, runDraftOne(t, drafter, businessWithID(), newYandexReview()))
	assert.Equal(t, 1, pub.count())
	require.Len(t, log.entries, 1)
	assert.Equal(t, audit.ActionReviewAutoReplied, log.entries[0].Action)
}

func TestAutopilot_ManualPermissionAppliesToEveryPlatform(t *testing.T) {
	for _, platform := range []string{"yandex_business", "telegram", "vk", "google_business"} {
		t.Run(platform, func(t *testing.T) {
			pub := &fakeAutoPublisher{}
			drafter := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
			wireYandexCounter(t, drafter)
			review, business := newYandexReview(), businessWithID()
			review.Platform = platform
			business.Settings = map[string]interface{}{"tool_approvals": map[string]interface{}{replyToolForPlatform(platform): "manual"}}
			require.NoError(t, runDraftOne(t, drafter, business, review))
			assert.Zero(t, pub.count(), "a manual override blocks even a safe five-star reply")
		})
	}
}

func TestAutopilot_YandexAgeAndRatingGuards(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*domain.Review)
	}{
		{"old publication despite recent ingestion", func(r *domain.Review) {
			r.PlatformMeta[apiplatform.YandexReviewPublishedAtKey] = time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
		}},
		{"unknown date", func(r *domain.Review) { r.PlatformMeta = nil }},
		{"future date", func(r *domain.Review) {
			r.PlatformMeta[apiplatform.YandexReviewPublishedAtKey] = time.Now().Add(time.Hour).Format(time.RFC3339)
		}},
		{"four stars", func(r *domain.Review) { r.Rating = 4 }},
		{"three stars", func(r *domain.Review) { r.Rating = 3 }},
		{"two stars", func(r *domain.Review) { r.Rating = 2 }},
		{"one star", func(r *domain.Review) { r.Rating = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			pub := &fakeAutoPublisher{}
			drafter := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
			wireYandexCounter(t, drafter)
			review := newYandexReview()
			test.change(review)
			require.NoError(t, runDraftOne(t, drafter, businessWithID(), review))
			assert.Zero(t, pub.count())
		})
	}
}

type deferredReviewRepo struct {
	*fakeReviewRepo
	ready    map[string]domain.Review
	deferred map[string]bool
}

func (r *deferredReviewRepo) SetAutoReplyDeferred(_ context.Context, _, id string, deferred bool) error {
	if deferred {
		r.deferred[id] = true
	} else {
		delete(r.deferred, id)
	}
	return nil
}

func (r *deferredReviewRepo) ListDeferredAutoReplies(_ context.Context, _, _ string, limit int) ([]domain.Review, error) {
	ids := make([]string, 0, len(r.deferred))
	for id := range r.deferred {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var result []domain.Review
	for _, id := range ids {
		if len(result) >= limit {
			break
		}
		result = append(result, r.ready[id])
	}
	return result, nil
}

func TestAutopilot_ChangedDeferredReviewDoesNotBlockLaterReplies(t *testing.T) {
	pub, business := &fakeAutoPublisher{}, businessWithID()
	repo := &deferredReviewRepo{fakeReviewRepo: &fakeReviewRepo{}, ready: map[string]domain.Review{}, deferred: map[string]bool{}}
	for i := 0; i < 11; i++ {
		review := newYandexReview()
		review.ID = fmt.Sprintf("queued-%02d", i)
		review.DraftReply, review.DraftStatus = "Спасибо!", domain.ReviewDraftStatusReady
		if i < 10 {
			review.Rating = 2
		}
		repo.ready[review.ID], repo.deferred[review.ID] = *review, true
	}
	drafter := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	drafter.reviewRepo, drafter.businessRepo = repo, &fakeBusinessRepo{business: business}
	wireYandexCounter(t, drafter)
	require.NoError(t, drafter.GenerateForBusiness(context.Background(), business.ID, "yandex_business"))
	require.Len(t, repo.deferred, 1, "ineligible first page releases its retry positions")
	assert.Zero(t, pub.count())
	require.NoError(t, drafter.GenerateForBusiness(context.Background(), business.ID, "yandex_business"))
	assert.Equal(t, 1, pub.count(), "the later eligible review is retried on the next pass")
}

func TestAutopilot_YandexHourlyCapDefersAndNextPassUsesSavedDraft(t *testing.T) {
	pub, log, business := &fakeAutoPublisher{}, &captureAuditLogger{}, businessWithID()
	repo := &deferredReviewRepo{fakeReviewRepo: &fakeReviewRepo{}, ready: map[string]domain.Review{}, deferred: map[string]bool{}}
	orch := &fakeDraftClient{resp: &orchestratorclient.DraftReplyResponse{DraftReply: "Спасибо!"}}
	drafter := NewReviewDrafter(repo, &fakeBusinessRepo{business: business}, orch, 5, 20)
	drafter.SetAutoPublisher(pub, log)
	server := wireYandexCounter(t, drafter)
	for i := 0; i < yandexAutoRepliesPerHour+1; i++ {
		review := newYandexReview()
		review.ID = fmt.Sprintf("new-%d", i)
		require.NoError(t, runDraftOne(t, drafter, business, review))
		review.DraftReply, review.DraftStatus = "Спасибо!", domain.ReviewDraftStatusReady
		repo.ready[review.ID] = *review
	}
	assert.Equal(t, yandexAutoRepliesPerHour, pub.count())
	require.Len(t, repo.deferred, 1)
	assert.Len(t, log.entries, yandexAutoRepliesPerHour)
	// A different business has its own cap.
	other := *business
	other.ID = uuid.New()
	otherReview := newYandexReview()
	otherReview.BusinessID = other.ID.String()
	require.NoError(t, runDraftOne(t, drafter, &other, otherReview))
	assert.Equal(t, yandexAutoRepliesPerHour+1, pub.count())
	server.FastForward(time.Hour + time.Second)
	require.NoError(t, drafter.GenerateForBusiness(context.Background(), business.ID, "yandex_business"))
	assert.Equal(t, yandexAutoRepliesPerHour+2, pub.count())
	assert.Len(t, orch.requests, yandexAutoRepliesPerHour+2, "deferred retry does not generate again")
}

func TestAutopilot_YandexExistingReadyDraftStaysManual(t *testing.T) {
	pub, business := &fakeAutoPublisher{}, businessWithID()
	review := newYandexReview()
	review.DraftReply, review.DraftStatus = "Готовый черновик", domain.ReviewDraftStatusReady
	repo := &deferredReviewRepo{fakeReviewRepo: &fakeReviewRepo{}, ready: map[string]domain.Review{review.ID: *review}, deferred: map[string]bool{}}
	drafter := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	drafter.reviewRepo, drafter.businessRepo = repo, &fakeBusinessRepo{business: business}
	wireYandexCounter(t, drafter)
	require.NoError(t, drafter.GenerateForBusiness(context.Background(), business.ID, "yandex_business"))
	assert.Zero(t, pub.count())
}

func TestAutopilot_YandexFailedSendHasNoReceiptAndKeepsDraft(t *testing.T) {
	pub, log := &fakeAutoPublisher{err: errors.New("RPA failed")}, &captureAuditLogger{}
	drafter := newAutopilotDrafter(t, pub, log, "Спасибо!")
	wireYandexCounter(t, drafter)
	require.NoError(t, runDraftOne(t, drafter, businessWithID(), newYandexReview()))
	assert.Empty(t, log.entries)
	repo := drafter.reviewRepo.(*fakeReviewRepo)
	require.NotEmpty(t, repo.updateDraftCalls)
	assert.Equal(t, domain.ReviewDraftStatusReady, repo.updateDraftCalls[len(repo.updateDraftCalls)-1].Status)
	assert.Equal(t, tools.YandexBusinessReplyReview, replyToolForPlatform("yandex_business"))
}
