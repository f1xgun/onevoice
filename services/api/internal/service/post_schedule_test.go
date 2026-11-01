package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type schedulePostRepo struct {
	mutationPostRepo
	created *domain.Post
}

func (r *schedulePostRepo) Create(_ context.Context, p *domain.Post) error {
	p.ID = "scheduled"
	r.created = p
	r.post = *p
	return nil
}
func (r *schedulePostRepo) UpdateScheduled(_ context.Context, business, id string, at *time.Time, content *string, status string) (bool, error) {
	if r.claimed || r.post.Status != "scheduled" {
		return false, nil
	}
	if at != nil {
		r.post.ScheduledAt = at
	}
	if content != nil {
		r.post.Content = *content
	}
	r.post.Status = status
	return true, nil
}
func (r *schedulePostRepo) ClaimDue(_ context.Context, now time.Time) (*domain.Post, error) {
	if r.post.Status != "scheduled" || r.post.ScheduledAt.After(now) {
		return nil, nil
	}
	r.claimed = true
	r.post.Status = "processing"
	p := r.post
	p.OperationID = "operation"
	return &p, nil
}

type scheduleIntegrations struct {
	domain.IntegrationRepository
	business uuid.UUID
}

func (r scheduleIntegrations) GetByBusinessPlatformExternal(_ context.Context, business uuid.UUID, platform, external string) (*domain.Integration, error) {
	if business != r.business || platform != "telegram" || external != "@own" {
		return nil, domain.ErrIntegrationNotFound
	}
	return &domain.Integration{BusinessID: business, Platform: platform, ExternalID: external, Status: "active"}, nil
}
func TestScheduledPostQueuesAndPublishesOnce(t *testing.T) {
	business := uuid.New()
	repo := &schedulePostRepo{}
	requester := &mutationRequester{}
	svc := WithPostScheduling(WithPostDispatcher(NewPostService(repo, nil), requester), scheduleIntegrations{business: business})
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, Permissions: []authz.Permission{authz.PermContentCreate}})
	future := time.Now().Add(time.Hour)
	post, err := svc.Schedule(ctx, business, "@own", "caption", []string{"https://example.org/photo.png"}, future)
	require.NoError(t, err)
	require.Equal(t, "scheduled", post.Status)
	require.Zero(t, requester.calls)
	repo.post.ScheduledAt = ptrPastSchedule()
	count, err := svc.PublishDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, 1, requester.calls)
	require.Equal(t, "@own", requester.req.Args["channel_id"])
	require.Equal(t, true, requester.req.Args["strict_target"])
	require.Equal(t, "https://example.org/photo.png", requester.req.Args["photo_url"])
	require.Equal(t, "published", repo.completed.Status)
	require.Equal(t, "42", repo.completed.PlatformResults["telegram"].PostID)
	_, err = svc.PublishDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, requester.calls)
}
func ptrPastSchedule() *time.Time { at := time.Now().Add(-time.Minute); return &at }

func TestScheduledPostRejectsForeignTargetAndPastTime(t *testing.T) {
	business := uuid.New()
	repo := &schedulePostRepo{}
	svc := WithPostScheduling(WithPostDispatcher(NewPostService(repo, nil), &mutationRequester{}), scheduleIntegrations{business: business})
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, Permissions: []authz.Permission{authz.PermContentCreate}})
	_, err := svc.Schedule(ctx, business, "@foreign", "text", nil, time.Now().Add(time.Hour))
	require.Error(t, err)
	require.Nil(t, repo.created)
	_, err = svc.Schedule(ctx, business, "@own", "text", nil, *ptrPastSchedule())
	require.Error(t, err)
	require.Nil(t, repo.created)
}

func TestScheduledPostRescheduleAndOwnerCancellation(t *testing.T) {
	business := uuid.New()
	repo := &schedulePostRepo{}
	requester := &mutationRequester{}
	svc := WithPostScheduling(WithPostDispatcher(NewPostService(repo, nil), requester), scheduleIntegrations{business: business})
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, RoleID: uuid.MustParse(domain.SystemRoleOwnerID), Permissions: []authz.Permission{authz.PermContentCreate, authz.PermContentUpdate, authz.PermContentDelete}})
	_, err := svc.Schedule(ctx, business, "@own", "old", nil, time.Now().Add(time.Hour))
	require.NoError(t, err)
	later := time.Now().Add(2 * time.Hour)
	text := "new"
	post, err := svc.Reschedule(ctx, business, "scheduled", &later, &text)
	require.NoError(t, err)
	require.Equal(t, "new", post.Content)
	require.True(t, post.ScheduledAt.Equal(later))
	post, err = svc.Mutate(ctx, business, "scheduled", "delete", nil)
	require.NoError(t, err)
	require.Equal(t, "canceled", post.Status)
	require.Zero(t, requester.calls)
	_, err = svc.Reschedule(ctx, business, "scheduled", &later, nil)
	require.ErrorIs(t, err, domain.ErrPostConflict)
}

func TestScheduledPostTimeoutCannotBeSentAgain(t *testing.T) {
	business := uuid.New()
	repo := &schedulePostRepo{}
	requester := &mutationRequester{err: context.DeadlineExceeded}
	svc := WithPostScheduling(WithPostDispatcher(NewPostService(repo, nil), requester), scheduleIntegrations{business: business})
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, Permissions: []authz.Permission{authz.PermContentCreate}})
	_, err := svc.Schedule(ctx, business, "@own", "text", nil, time.Now().Add(time.Hour))
	require.NoError(t, err)
	repo.post.ScheduledAt = ptrPastSchedule()
	_, err = svc.PublishDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, "uncertain", repo.completed.Status)
	_, err = svc.PublishDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, requester.calls)
}
