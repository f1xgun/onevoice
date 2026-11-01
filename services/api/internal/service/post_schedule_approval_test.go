package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

type scheduleApprovalRepo struct {
	domain.PendingToolCallRepository
	batch *domain.PendingToolCallBatch
}

func (r scheduleApprovalRepo) GetByBatchID(context.Context, string) (*domain.PendingToolCallBatch, error) {
	return r.batch, nil
}

type scheduleConversationRepo struct {
	domain.ConversationRepository
	conversation *domain.Conversation
}

func (r scheduleConversationRepo) GetByID(context.Context, string) (*domain.Conversation, error) {
	return r.conversation, nil
}

type approvedSchedulePostRepo struct {
	schedulePostRepo
	creates int
}

func (r *approvedSchedulePostRepo) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	if r.post.ID != id {
		return nil, domain.ErrPostNotFound
	}
	return r.mutationPostRepo.GetByID(ctx, id)
}
func (r *approvedSchedulePostRepo) Create(ctx context.Context, p *domain.Post) error {
	r.creates++
	id := p.ID
	err := r.schedulePostRepo.Create(ctx, p)
	p.ID = id
	r.post.ID = id
	return err
}
func TestApprovedScheduleProofAndReplay(t *testing.T) {
	business, user, batchID := uuid.New(), uuid.New(), uuid.NewString()
	args := map[string]interface{}{"channel_id": "@own", "text": "draft", "scheduled_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	batch := &domain.PendingToolCallBatch{ID: batchID, BusinessID: business.String(), UserID: user.String(), ConversationID: "conversation", MessageID: "message", Status: "resuming", Calls: []domain.PendingCall{{CallID: "call", ToolName: tools.TelegramSendChannelPost, Verdict: actionEdit, Arguments: map[string]interface{}{"channel_id": "@own", "text": "old"}, EditedArgs: map[string]interface{}{"text": "draft", "scheduled_at": args["scheduled_at"]}}}}
	posts := &approvedSchedulePostRepo{}
	requester := &mutationRequester{}
	svc := WithPostScheduling(WithPostDispatcher(NewPostService(posts, nil), requester), scheduleIntegrations{business: business})
	svc = WithPostApproval(svc, scheduleApprovalRepo{batch: batch}, scheduleConversationRepo{conversation: &domain.Conversation{ID: "conversation", BusinessID: business.String(), UserID: user.String()}})
	request := ApprovedPostSchedule{BusinessID: business.String(), ConversationID: "conversation", ApprovalID: batchID + "-call", Tool: tools.TelegramSendChannelPost, Args: args}
	first, err := svc.ScheduleApproved(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "message", first.Post.BroadcastGroupID)
	replay, err := svc.ScheduleApproved(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.Post.ID, replay.Post.ID)
	require.Equal(t, 1, posts.creates)
	require.Zero(t, requester.calls)
	request.BusinessID = uuid.NewString()
	_, err = svc.ScheduleApproved(context.Background(), request)
	require.ErrorIs(t, err, domain.ErrForbidden)
	request.BusinessID = business.String()
	request.Args = map[string]interface{}{"text": "unapproved"}
	_, err = svc.ScheduleApproved(context.Background(), request)
	require.ErrorIs(t, err, domain.ErrForbidden)
	request.Args = args
	batch.Calls[0].Verdict = actionReject
	_, err = svc.ScheduleApproved(context.Background(), request)
	require.ErrorIs(t, err, domain.ErrForbidden)
}

type ambiguousScheduleIntegration struct{ scheduleIntegrations }

func (r ambiguousScheduleIntegration) ListByBusinessAndPlatform(context.Context, uuid.UUID, string) ([]domain.Integration, error) {
	return []domain.Integration{{BusinessID: r.business, ExternalID: "@one", Platform: "telegram", Status: "active"}, {BusinessID: r.business, ExternalID: "@two", Platform: "telegram", Status: "active"}}, nil
}
func TestApprovedScheduleAmbiguousTargetFailsClosed(t *testing.T) {
	business, user, batchID := uuid.New(), uuid.New(), uuid.NewString()
	args := map[string]interface{}{"text": "draft", "scheduled_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	batch := &domain.PendingToolCallBatch{ID: batchID, BusinessID: business.String(), UserID: user.String(), ConversationID: "conversation", Status: "resuming", Calls: []domain.PendingCall{{CallID: "call", ToolName: tools.TelegramSendChannelPost, Verdict: actionApprove, Arguments: args}}}
	posts := &approvedSchedulePostRepo{}
	requester := &mutationRequester{}
	svc := WithPostApproval(WithPostScheduling(WithPostDispatcher(NewPostService(posts, nil), requester), ambiguousScheduleIntegration{scheduleIntegrations{business: business}}), scheduleApprovalRepo{batch: batch}, scheduleConversationRepo{conversation: &domain.Conversation{BusinessID: business.String(), UserID: user.String()}})
	_, err := svc.ScheduleApproved(context.Background(), ApprovedPostSchedule{BusinessID: business.String(), ConversationID: "conversation", ApprovalID: batchID + "-call", Tool: tools.TelegramSendChannelPost, Args: args})
	require.ErrorIs(t, err, domain.ErrPostConflict)
	require.Contains(t, err.Error(), "choose the Telegram channel")
	require.Zero(t, posts.creates)
	require.Zero(t, requester.calls)
}

func TestApprovedExplicitNowEditAndElapsedReplay(t *testing.T) {
	business, user, batchID := uuid.New(), uuid.New(), uuid.NewString()
	past := time.Now().Add(-time.Hour).UTC()
	args := map[string]interface{}{"channel_id": "@own", "text": "text", "scheduled_at": ""}
	call := domain.PendingCall{CallID: "call", ToolName: tools.TelegramSendChannelPost, Verdict: actionEdit, Arguments: map[string]interface{}{"channel_id": "@own", "text": "text", "scheduled_at": past.Format(time.RFC3339)}, EditedArgs: map[string]interface{}{"scheduled_at": ""}}
	batch := &domain.PendingToolCallBatch{ID: batchID, BusinessID: business.String(), UserID: user.String(), ConversationID: "conversation", Status: "resuming", Calls: []domain.PendingCall{call}}
	posts := &approvedSchedulePostRepo{}
	requester := &mutationRequester{}
	svc := WithPostApproval(WithPostScheduling(WithPostDispatcher(NewPostService(posts, nil), requester), scheduleIntegrations{business: business}), scheduleApprovalRepo{batch: batch}, scheduleConversationRepo{conversation: &domain.Conversation{BusinessID: business.String(), UserID: user.String()}})
	request := ApprovedPostSchedule{BusinessID: business.String(), ConversationID: "conversation", ApprovalID: batchID + "-call", Tool: tools.TelegramSendChannelPost, Args: args}
	approved, err := svc.ScheduleApproved(context.Background(), request)
	require.NoError(t, err)
	require.True(t, approved.Immediate)
	require.Nil(t, approved.Post)
	require.Zero(t, posts.creates)
	batch.Calls[0].Verdict = actionApprove
	batch.Calls[0].Arguments = args
	batch.Calls[0].EditedArgs = nil
	_, err = svc.ScheduleApproved(context.Background(), request)
	require.ErrorIs(t, err, domain.ErrForbidden)
	args["scheduled_at"] = past.Format(time.RFC3339)
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(request.BusinessID+":"+request.ApprovalID)).String()
	posts.post = domain.Post{ID: id, BusinessID: business.String(), Status: "published", ScheduledAt: &past}
	replay, err := svc.ScheduleApproved(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, id, replay.Post.ID)
	require.Zero(t, posts.creates)
	posts.post = domain.Post{}
	_, err = svc.ScheduleApproved(context.Background(), request)
	require.ErrorIs(t, err, domain.ErrPostConflict)
	require.Zero(t, posts.creates)
	require.Zero(t, requester.calls)
}
