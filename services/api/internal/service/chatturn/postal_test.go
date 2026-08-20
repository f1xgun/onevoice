package chatturn

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// counterValue reads the current value of a {name, labels} counter series from
// the default Prometheus gatherer, returning 0 when the series has no samples
// yet. Used to assert recordPostsAndReviews emits the product metrics.
func counterValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelsMatch(got []*dto.LabelPair, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, l := range got {
		if want[l.GetName()] != l.GetValue() {
			return false
		}
	}
	return true
}

// fakeIntegrations records MarkTokenExpired calls so the test can assert the
// token-health flip fires only on integration_token_invalid. The embedded
// IntegrationLister leaves ListByBusinessID nil — the postal path never calls
// it.
type fakeIntegrations struct {
	IntegrationLister
	marked []markCall
}

type markCall struct {
	businessID uuid.UUID
	platform   string
	externalID string
}

func (f *fakeIntegrations) MarkTokenExpired(_ context.Context, businessID uuid.UUID, platform, externalID string) error {
	f.marked = append(f.marked, markCall{businessID: businessID, platform: platform, externalID: externalID})
	return nil
}

// fakeAgentTaskRepo records every Create / Update call so the test can assert
// on the persisted shape. Other methods nil-panic — the tests in this file
// only exercise the onToolCall / onToolResult paths.
type fakeAgentTaskRepo struct {
	domain.AgentTaskRepository
	created []domain.AgentTask
}

func (f *fakeAgentTaskRepo) Create(_ context.Context, t *domain.AgentTask) error {
	t.ID = "fake-id"
	f.created = append(f.created, *t)
	return nil
}

// newTurnForPostalTest builds a Turn with only the AgentTasks dep wired —
// uses the unexported struct literal (accessible from same-package tests) so
// the chatturn.New panic-on-nil guards don't fire for deps the postal path
// doesn't touch.
func newTurnForPostalTest(repo domain.AgentTaskRepository) *Turn {
	return &Turn{deps: Deps{AgentTasks: repo}}
}

// TestOnToolCall_PersistsDisplayNameKey verifies the i18n catalog key
// arriving on the orchestrator SSE frame must reach the agent_tasks document
// so the FE can render the task title in the user's locale.
func TestOnToolCall_PersistsDisplayNameKey(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",
		"tools.telegram.send_channel_post.name",
		map[string]interface{}{"text": "hi"},
		"",
		idMap,
	)

	require.Len(t, repo.created, 1, "onToolCall should persist exactly one task")
	got := repo.created[0]
	assert.Equal(t, "Отправить пост", got.DisplayName, "legacy DisplayName preserved")
	assert.Equal(t, "tools.telegram.send_channel_post.name", got.DisplayNameKey,
		"DisplayNameKey must reach the persisted agent_tasks document")
	assert.Equal(t, "telegram", got.Platform)
	assert.Equal(t, "send_channel_post", got.Type)
	assert.Equal(t, "running", got.Status)
}

// TestOnToolCall_PersistsDispatchApprovalID verifies the original approved
// dispatch key ("<batch_id>-<call_id>") is stamped onto the created agent_tasks
// document, so a later retry reads it and dedupes an already-landed call instead
// of re-executing it. Reverting the DispatchApprovalID stamp fails this test.
func TestOnToolCall_PersistsDispatchApprovalID(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",
		"tools.telegram.send_channel_post.name",
		map[string]interface{}{"text": "hi"},
		"batch-9-call-1",
		idMap,
	)

	require.Len(t, repo.created, 1)
	assert.Equal(t, "batch-9-call-1", repo.created[0].DispatchApprovalID,
		"the original dispatch key must reach the persisted agent_tasks document")
}

// TestOnToolCall_EmptyDisplayNameKey_BackwardCompat — orchestrators predating
// the i18n key still persist (FE falls back to the legacy DisplayName field).
func TestOnToolCall_EmptyDisplayNameKey_BackwardCompat(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		tools.TelegramSendChannelPost,
		"Отправить пост",
		"",
		map[string]interface{}{"text": "hi"},
		"",
		idMap,
	)

	require.Len(t, repo.created, 1)
	assert.Equal(t, "", repo.created[0].DisplayNameKey)
	assert.Equal(t, "Отправить пост", repo.created[0].DisplayName)
}

// fakeAgentTaskRepoWithUpdate captures Update calls so the test can assert
// that ErrorCode is persisted on the AgentTask document.
type fakeAgentTaskRepoWithUpdate struct {
	fakeAgentTaskRepo
	updated []domain.AgentTask
	// reloadPlatform is the Platform stamped on the task returned by GetByID,
	// used by the token-flip path to resolve which integration to expire.
	reloadPlatform string
}

func (f *fakeAgentTaskRepoWithUpdate) Update(_ context.Context, t *domain.AgentTask) error {
	f.updated = append(f.updated, *t)
	return nil
}

func (f *fakeAgentTaskRepoWithUpdate) GetByID(_ context.Context, businessID, taskID string) (*domain.AgentTask, error) {
	return &domain.AgentTask{ID: taskID, BusinessID: businessID, Platform: f.reloadPlatform}, nil
}

// TestOnToolResult_StampsErrorCode — when the SSE tool_result frame carries a
// typed Code, onToolResult must forward it onto AgentTask.ErrorCode so the
// repository writes error_code into Mongo on the same Update.
func TestOnToolResult_StampsErrorCode(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{"call-1": "task-1"}
	turn.onToolResult(
		context.Background(),
		"biz-1",
		"call-1",
		map[string]interface{}{"error": "Unauthorized: bot kicked"},
		nil,
		"telegram: send message: Unauthorized: bot kicked",
		"integration_token_invalid",
		idMap,
	)

	require.Len(t, repo.updated, 1)
	got := repo.updated[0]
	assert.Equal(t, "error", got.Status)
	assert.Equal(t, "Unauthorized: bot kicked", got.Error)
	assert.Equal(t, "integration_token_invalid", got.ErrorCode)
}

// TestOnToolResult_NoCode_LeavesErrorCodeEmpty — uncoded errors do not write
// an ErrorCode so the repository's selective $set leaves any prior value
// (or absence) untouched.
func TestOnToolResult_NoCode_LeavesErrorCodeEmpty(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{"call-2": "task-2"}
	turn.onToolResult(
		context.Background(),
		"biz-1",
		"call-2",
		map[string]interface{}{"error": "transient network error"},
		nil,
		"transient network error",
		"",
		idMap,
	)

	require.Len(t, repo.updated, 1)
	assert.Empty(t, repo.updated[0].ErrorCode)
}

// TestOnToolResult_IntegrationTokenInvalid_FlipsStatus — a rejected-token tool
// result must flip the integration status for the task's platform so the
// dashboard prompts a reconnect. When the failing tool call identifies the
// channel the flip must be scoped to that one integration so a sibling channel
// on the same platform is not falsely forced to reconnect.
func TestOnToolResult_IntegrationTokenInvalid_FlipsStatus(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "telegram"}
	integ := &fakeIntegrations{}
	turn := &Turn{deps: Deps{AgentTasks: repo, Integrations: integ}}

	businessID := uuid.New()
	idMap := map[string]string{"call-1": "task-1"}
	turn.onToolResult(
		context.Background(),
		businessID.String(),
		"call-1",
		map[string]interface{}{"error": "Unauthorized: bot kicked"},
		map[string]interface{}{"channel_id": "@first_channel", "text": "hi"},
		"telegram: send message: Unauthorized: bot kicked",
		"integration_token_invalid",
		idMap,
	)

	require.Len(t, integ.marked, 1, "MarkTokenExpired must fire on integration_token_invalid")
	assert.Equal(t, businessID, integ.marked[0].businessID)
	assert.Equal(t, "telegram", integ.marked[0].platform)
	assert.Equal(t, "@first_channel", integ.marked[0].externalID,
		"flip must be scoped to the channel the failing call targeted")
}

// TestOnToolResult_IntegrationTokenInvalid_NoExternalID_FlipsPlatformWide — when
// the failing tool call carries no channel identifier the flip falls back to the
// platform-wide behavior (empty external_id) so the rejection is not silently
// dropped.
func TestOnToolResult_IntegrationTokenInvalid_NoExternalID_FlipsPlatformWide(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "telegram"}
	integ := &fakeIntegrations{}
	turn := &Turn{deps: Deps{AgentTasks: repo, Integrations: integ}}

	businessID := uuid.New()
	idMap := map[string]string{"call-1": "task-1"}
	turn.onToolResult(
		context.Background(),
		businessID.String(),
		"call-1",
		map[string]interface{}{"error": "Unauthorized: bot kicked"},
		map[string]interface{}{"text": "hi"},
		"telegram: send message: Unauthorized: bot kicked",
		"integration_token_invalid",
		idMap,
	)

	require.Len(t, integ.marked, 1, "MarkTokenExpired must fire on integration_token_invalid")
	assert.Empty(t, integ.marked[0].externalID,
		"missing channel identifier falls back to a platform-wide flip")
}

// TestOnToolResult_Success_DoesNotFlipStatus — a successful tool result must
// never touch integration status.
func TestOnToolResult_Success_DoesNotFlipStatus(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "telegram"}
	integ := &fakeIntegrations{}
	turn := &Turn{deps: Deps{AgentTasks: repo, Integrations: integ}}

	idMap := map[string]string{"call-1": "task-1"}
	turn.onToolResult(
		context.Background(),
		uuid.New().String(),
		"call-1",
		map[string]interface{}{"message_id": float64(42)},
		nil,
		"",
		"",
		idMap,
	)

	assert.Empty(t, integ.marked, "MarkTokenExpired must not fire on a successful tool result")
}

// TestOnToolResult_OtherErrorCode_DoesNotFlipStatus — a non-token error code
// (e.g. rate_limit_exceeded) must not flip integration status.
func TestOnToolResult_OtherErrorCode_DoesNotFlipStatus(t *testing.T) {
	repo := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "vk"}
	integ := &fakeIntegrations{}
	turn := &Turn{deps: Deps{AgentTasks: repo, Integrations: integ}}

	idMap := map[string]string{"call-1": "task-1"}
	turn.onToolResult(
		context.Background(),
		uuid.New().String(),
		"call-1",
		map[string]interface{}{"error": "Too Many Requests"},
		nil,
		"vk: rate limited",
		"rate_limit_exceeded",
		idMap,
	)

	assert.Empty(t, integ.marked, "MarkTokenExpired must only fire on integration_token_invalid")
}

// TestOnToolCall_InternalToolSkipped — internal tools (no "__" separator) do
// not surface on the Tasks page; the SSE handler must skip persistence
// regardless of the displayNameKey value.
func TestOnToolCall_InternalToolSkipped(t *testing.T) {
	repo := &fakeAgentTaskRepo{}
	turn := newTurnForPostalTest(repo)

	idMap := map[string]string{}
	turn.onToolCall(
		context.Background(),
		"biz-1",
		"call-1",
		"get_business_info",
		"Внутренний инструмент",
		"tools.internal.get_business_info.name",
		nil,
		"",
		idMap,
	)

	assert.Empty(t, repo.created, "internal tools must not be persisted as agent_tasks")
}

// fakePostRepo records Create calls so the posting-tool coverage test can
// assert the persisted Post shape.
type fakePostRepo struct {
	domain.PostRepository
	created []domain.Post
}

func (f *fakePostRepo) Create(_ context.Context, p *domain.Post) error {
	f.created = append(f.created, *p)
	return nil
}

// TestRecordPosts_CoversAllPostingTools — every content-publishing tool must
// produce a Post record, or a successful publish silently vanishes from the
// feed. Guards the three tools that were missing from postingTools
// (vk__post_photo, vk__schedule_post, yandex_business__create_post) and asserts
// the scheduled tool records as "scheduled" with a parsed ScheduledAt rather
// than an immediate "published".
func TestRecordPosts_CoversAllPostingTools(t *testing.T) {
	repo := &fakePostRepo{}
	turn := &Turn{deps: Deps{Posts: repo}}

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: tools.VKPostPhoto, Arguments: map[string]interface{}{"caption": "photo cap", "photo_url": "https://x/img.jpg"}},
		{ID: "c2", Name: tools.VKSchedulePost, Arguments: map[string]interface{}{"text": "later", "publish_date": "2026-03-20T12:00:00Z"}},
		{ID: "c3", Name: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "ya post"}},
	}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1"},
		{ToolCallID: "c2"},
		{ToolCallID: "c3"},
	}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.Len(t, repo.created, 3, "each posting tool must persist a Post")
	byContent := map[string]domain.Post{}
	for _, p := range repo.created {
		byContent[p.Content] = p
	}

	photo := byContent["photo cap"]
	assert.Equal(t, "published", photo.Status)
	assert.Equal(t, []string{"https://x/img.jpg"}, photo.MediaURLs)
	_, hasVK := photo.PlatformResults["vk"]
	assert.True(t, hasVK, "vk photo post recorded under the vk platform")

	scheduled := byContent["later"]
	assert.Equal(t, "scheduled", scheduled.Status, "vk schedule_post records as scheduled, not published")
	assert.Nil(t, scheduled.PublishedAt)
	require.NotNil(t, scheduled.ScheduledAt, "publish_date must parse into ScheduledAt")
	assert.Equal(t, 2026, scheduled.ScheduledAt.Year())

	yandex := byContent["ya post"]
	assert.Equal(t, "published", yandex.Status)
	_, hasYandex := yandex.PlatformResults["yandex_business"]
	assert.True(t, hasYandex, "yandex create_post recorded under the yandex_business platform")
}

// TestRecordPosts_BroadcastGroup_SharedAcrossOneTurn — the Post records fanned
// out by ONE turn (one recordPostsAndReviews invocation) must share the turn's
// broadcast group id, including the failed channel, so the history can render
// the broadcast as one card with a visible partial failure.
func TestRecordPosts_BroadcastGroup_SharedAcrossOneTurn(t *testing.T) {
	repo := &fakePostRepo{}
	turn := &Turn{deps: Deps{Posts: repo}}

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
		{ID: "c2", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "hi"}},
		{ID: "c3", Name: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "hi"}},
	}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1"},
		{ToolCallID: "c2", IsError: true, Content: map[string]interface{}{"error": "rate limited"}},
		{ToolCallID: "c3"},
	}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "turn-msg-1", toolCalls, toolResults)

	require.Len(t, repo.created, 3, "each channel of the broadcast must persist its own Post")
	for _, p := range repo.created {
		assert.Equal(t, "turn-msg-1", p.BroadcastGroupID,
			"every post of one turn — the failed channel included — must carry the turn's group id")
	}
}

// TestRecordPosts_BroadcastGroup_StableAcrossResumeOfSameTurn — a sequential
// HITL fan-out records the same turn's posts over SEVERAL invocations (pause →
// approve → re-pause → approve), each passing the SAME assistant message id.
// The group must not split. This is also the retry contract: a re-dispatch that
// records under the original turn's id can never mint a new group.
func TestRecordPosts_BroadcastGroup_StableAcrossResumeOfSameTurn(t *testing.T) {
	repo := &fakePostRepo{}
	turn := &Turn{deps: Deps{Posts: repo}}

	turn.recordPostsAndReviews(context.Background(), "biz-1", "turn-msg-7",
		[]domain.ToolCall{{ID: "c1", Name: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "hi"}}},
		[]domain.ToolResult{{ToolCallID: "c1"}},
	)
	turn.recordPostsAndReviews(context.Background(), "biz-1", "turn-msg-7",
		[]domain.ToolCall{{ID: "c2", Name: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}}},
		[]domain.ToolResult{{ToolCallID: "c2"}},
	)

	require.Len(t, repo.created, 2)
	assert.Equal(t, repo.created[0].BroadcastGroupID, repo.created[1].BroadcastGroupID,
		"posts recorded by separate resume invocations of the same turn must share one group")
}

// TestRecordPosts_BroadcastGroup_DistinctAcrossTurns — posts produced by
// different turns carry different group ids and must never be folded into one
// broadcast card.
func TestRecordPosts_BroadcastGroup_DistinctAcrossTurns(t *testing.T) {
	repo := &fakePostRepo{}
	turn := &Turn{deps: Deps{Posts: repo}}

	turn.recordPostsAndReviews(context.Background(), "biz-1", "turn-msg-a",
		[]domain.ToolCall{{ID: "c1", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "first"}}},
		[]domain.ToolResult{{ToolCallID: "c1"}},
	)
	turn.recordPostsAndReviews(context.Background(), "biz-1", "turn-msg-b",
		[]domain.ToolCall{{ID: "c2", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "second"}}},
		[]domain.ToolResult{{ToolCallID: "c2"}},
	)

	require.Len(t, repo.created, 2)
	assert.NotEqual(t, repo.created[0].BroadcastGroupID, repo.created[1].BroadcastGroupID,
		"posts of different turns must not share a broadcast group")
}

// TestRecordPosts_CapturesPermalinkFromToolResult — a successful posting tool
// result carrying url/post_id must persist them onto the PlatformResult so the
// history can offer an "open" permalink; an errored result must not (the post
// never landed, a stale url would lie).
func TestRecordPosts_CapturesPermalinkFromToolResult(t *testing.T) {
	repo := &fakePostRepo{}
	turn := &Turn{deps: Deps{Posts: repo}}

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "vk post"}},
		{ID: "c2", Name: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "tg post"}},
		{ID: "c3", Name: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "ya post"}},
		{ID: "c4", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "vk failed"}},
	}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1", Content: map[string]interface{}{"post_id": float64(321), "url": "https://vk.com/wall-77_321"}},
		{ToolCallID: "c2", Content: map[string]interface{}{"status": "sent", "message_id": float64(45), "url": "https://t.me/mychannel/45"}},
		{ToolCallID: "c3", Content: map[string]interface{}{"status": "created"}},
		{ToolCallID: "c4", IsError: true, Content: map[string]interface{}{"error": "boom", "url": "https://vk.com/wall-77_999"}},
	}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "turn-msg-1", toolCalls, toolResults)

	require.Len(t, repo.created, 4)
	byContent := map[string]domain.Post{}
	for _, p := range repo.created {
		byContent[p.Content] = p
	}

	vk := byContent["vk post"].PlatformResults["vk"]
	assert.Equal(t, "https://vk.com/wall-77_321", vk.URL)
	assert.Equal(t, "321", vk.PostID, "numeric post_id must persist as its decimal string")

	tg := byContent["tg post"].PlatformResults["telegram"]
	assert.Equal(t, "https://t.me/mychannel/45", tg.URL)
	assert.Equal(t, "45", tg.PostID, "telegram message_id must persist as the post id")

	ya := byContent["ya post"].PlatformResults["yandex_business"]
	assert.Empty(t, ya.URL, "a platform without a public permalink stays url-less")
	assert.Empty(t, ya.PostID)

	failed := byContent["vk failed"].PlatformResults["vk"]
	assert.Empty(t, failed.URL, "an errored result must never persist a permalink")
	assert.Equal(t, "boom", failed.Error)
}

// failingPostRepo always returns an error from Create so the test can assert the
// publish metric is still counted (with result="error" derived from the tool
// result) even when persistence fails.
type failingPostRepo struct {
	domain.PostRepository
}

func (failingPostRepo) Create(_ context.Context, _ *domain.Post) error {
	return errors.New("mongo down")
}

// fakeReviewRepo records Upsert calls and optionally fails them so the test can
// exercise both the success (replied/pending) and upsert-error metric paths.
type fakeReviewRepo struct {
	domain.ReviewRepository
	fail     bool
	upserted []domain.Review
}

func (f *fakeReviewRepo) Upsert(_ context.Context, r *domain.Review) error {
	if f.fail {
		return errors.New("mongo down")
	}
	f.upserted = append(f.upserted, *r)
	return nil
}

// TestRecordPosts_EmitsPublishMetric — a successful publish increments
// posts_published_total{platform,result} and an errored tool result is counted
// with result="error" rather than silently dropped.
func TestRecordPosts_EmitsPublishMetric(t *testing.T) {
	repo := &fakePostRepo{}
	turn := &Turn{deps: Deps{Posts: repo}}

	okLabels := map[string]string{"platform": "telegram", "result": "published"}
	errLabels := map[string]string{"platform": "vk", "result": "error"}
	beforeOK := counterValue(t, "posts_published_total", okLabels)
	beforeErr := counterValue(t, "posts_published_total", errLabels)

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
		{ID: "c2", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "boom"}},
	}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1"},
		{ToolCallID: "c2", IsError: true, Content: map[string]interface{}{"error": "rate limited"}},
	}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.InDelta(t, beforeOK+1, counterValue(t, "posts_published_total", okLabels), 0.0001,
		"successful publish must increment {telegram,published}")
	require.InDelta(t, beforeErr+1, counterValue(t, "posts_published_total", errLabels), 0.0001,
		"errored publish must increment {vk,error}")
}

// TestRecordPosts_EmitsPublishMetricOnPersistFailure — a Posts.Create failure is
// a persistence problem, not a publish outcome, so the publish metric is still
// counted (the post did land on the platform).
func TestRecordPosts_EmitsPublishMetricOnPersistFailure(t *testing.T) {
	turn := &Turn{deps: Deps{Posts: failingPostRepo{}}}

	labels := map[string]string{"platform": "yandex_business", "result": "published"}
	before := counterValue(t, "posts_published_total", labels)

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "ya"}},
	}
	toolResults := []domain.ToolResult{{ToolCallID: "c1"}}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.InDelta(t, before+1, counterValue(t, "posts_published_total", labels), 0.0001,
		"publish metric must fire even when the Post record fails to persist")
}

// TestRecordReviews_EmitsReplyMetric — each upserted review increments
// reviews_replied_total with the review's reply state, and an errored
// get_reviews result is counted with result="error".
func TestRecordReviews_EmitsReplyMetric(t *testing.T) {
	repo := &fakeReviewRepo{}
	turn := &Turn{deps: Deps{Reviews: repo}}

	repliedLabels := map[string]string{"platform": "yandex_business", "result": "replied"}
	pendingLabels := map[string]string{"platform": "yandex_business", "result": "pending"}
	errLabels := map[string]string{"platform": "vk", "result": "error"}
	beforeReplied := counterValue(t, "reviews_replied_total", repliedLabels)
	beforePending := counterValue(t, "reviews_replied_total", pendingLabels)
	beforeErr := counterValue(t, "reviews_replied_total", errLabels)

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: "yandex_business__get_reviews"},
		{ID: "c2", Name: "vk__get_reviews"},
	}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1", Content: map[string]interface{}{"reviews": []interface{}{
			map[string]interface{}{"id": "r1", "reply": "thanks!"},
			map[string]interface{}{"id": "r2"},
		}}},
		{ToolCallID: "c2", IsError: true, Content: map[string]interface{}{"error": "session expired"}},
	}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.Len(t, repo.upserted, 2, "both reviews must upsert")
	require.InDelta(t, beforeReplied+1, counterValue(t, "reviews_replied_total", repliedLabels), 0.0001,
		"review carrying a reply increments {yandex_business,replied}")
	require.InDelta(t, beforePending+1, counterValue(t, "reviews_replied_total", pendingLabels), 0.0001,
		"review without a reply increments {yandex_business,pending}")
	require.InDelta(t, beforeErr+1, counterValue(t, "reviews_replied_total", errLabels), 0.0001,
		"errored get_reviews increments {vk,error}")
}

// TestRecordReviews_EmitsErrorMetricOnUpsertFailure — an Upsert failure is
// counted with result="error" so a persistence problem is not silently dropped.
func TestRecordReviews_EmitsErrorMetricOnUpsertFailure(t *testing.T) {
	turn := &Turn{deps: Deps{Reviews: &fakeReviewRepo{fail: true}}}

	labels := map[string]string{"platform": "yandex_business", "result": "error"}
	before := counterValue(t, "reviews_replied_total", labels)

	toolCalls := []domain.ToolCall{{ID: "c1", Name: "yandex_business__get_reviews"}}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1", Content: map[string]interface{}{"reviews": []interface{}{
			map[string]interface{}{"id": "r1", "reply": "thanks!"},
		}}},
	}
	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.InDelta(t, before+1, counterValue(t, "reviews_replied_total", labels), 0.0001,
		"upsert failure must increment {yandex_business,error}")
}

// reconcilingReviewRepo serves one review by its natural key and records the
// UpdateReply call the chat-reply reconciliation must perform. Lookups for a
// non-matching key return ErrReviewNotFound.
type reconcilingReviewRepo struct {
	domain.ReviewRepository
	review        *domain.Review
	updateCalls   []updateReplyCall
	lastLookupKey [3]string
}

type updateReplyCall struct {
	id         string
	text       string
	status     string
	approvalID string
}

func (r *reconcilingReviewRepo) GetByExternalID(_ context.Context, businessID, platform, externalID string) (*domain.Review, error) {
	r.lastLookupKey = [3]string{businessID, platform, externalID}
	if r.review != nil &&
		r.review.BusinessID == businessID &&
		r.review.Platform == platform &&
		r.review.ExternalID == externalID {
		return r.review, nil
	}
	return nil, domain.ErrReviewNotFound
}

func (r *reconcilingReviewRepo) UpdateReplyDispatched(_ context.Context, id, replyText, replyStatus, dispatchApprovalID string) error {
	r.updateCalls = append(r.updateCalls, updateReplyCall{id: id, text: replyText, status: replyStatus, approvalID: dispatchApprovalID})
	return nil
}

// TestReconcileReviewReplies_FlipsStatusOnSuccessfulYandexReply is the
// fail-on-revert anchor for fix #1: a successful yandex_business__reply_review
// chat tool must reconcile the stored review to reply_status="replied" via the
// same UpdateReply method the manual path uses, so the manual re-reply guard
// fires and a second public reply is never posted. Reverting the
// reconcileReviewReplies call leaves the review "pending" and this assertion
// fails.
func TestReconcileReviewReplies_FlipsStatusOnSuccessfulYandexReply(t *testing.T) {
	repo := &reconcilingReviewRepo{review: &domain.Review{
		ID:          "rev-stored-1",
		BusinessID:  "biz-1",
		Platform:    a2a.AgentYandexBusiness,
		ExternalID:  "yreview-77",
		ReplyStatus: domain.ReviewReplyStatusPending,
	}}
	turn := &Turn{deps: Deps{Reviews: repo}}

	toolCalls := []domain.ToolCall{{
		ID:         "c1",
		Name:       tools.YandexBusinessReplyReview,
		Arguments:  map[string]interface{}{"review_id": "yreview-77", "text": "Спасибо за отзыв!"},
		ApprovalID: "batch-7-c1",
	}}
	toolResults := []domain.ToolResult{{ToolCallID: "c1"}}

	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.Len(t, repo.updateCalls, 1, "a successful reply tool must reconcile the stored review exactly once")
	got := repo.updateCalls[0]
	assert.Equal(t, "rev-stored-1", got.id, "reconcile must target the resolved review")
	assert.Equal(t, domain.ReviewReplyStatusReplied, got.status,
		"chat reply must flip the stored review to replied so the manual guard fires")
	assert.Equal(t, "Спасибо за отзыв!", got.text, "reconcile must persist the reply text the LLM posted")
	assert.Equal(t, "batch-7-c1", got.approvalID,
		"reconcile must persist the original dispatch key so a manual retry dedupes the already-posted reply")
	assert.Equal(t, [3]string{"biz-1", a2a.AgentYandexBusiness, "yreview-77"}, repo.lastLookupKey,
		"review must be resolved by (business_id, platform, external_id) from the reply args")
}

// TestReconcileReviewReplies_VKComposesExternalID — VK comment replies carry
// post_id + comment_id; the reconciler must compose "{post_id}_{comment_id}"
// (the same key reviewFromMap stores) to resolve and flip the review.
func TestReconcileReviewReplies_VKComposesExternalID(t *testing.T) {
	repo := &reconcilingReviewRepo{review: &domain.Review{
		ID:          "rev-vk-1",
		BusinessID:  "biz-1",
		Platform:    a2a.AgentVK,
		ExternalID:  "11_42",
		ReplyStatus: domain.ReviewReplyStatusPending,
	}}
	turn := &Turn{deps: Deps{Reviews: repo}}

	toolCalls := []domain.ToolCall{{
		ID:        "c1",
		Name:      tools.VKReplyComment,
		Arguments: map[string]interface{}{"post_id": float64(11), "comment_id": float64(42), "text": "Спасибо!"},
	}}
	toolResults := []domain.ToolResult{{ToolCallID: "c1"}}

	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	require.Len(t, repo.updateCalls, 1, "vk reply must reconcile the composed-external-id review")
	assert.Equal(t, domain.ReviewReplyStatusReplied, repo.updateCalls[0].status)
	assert.Equal(t, [3]string{"biz-1", a2a.AgentVK, "11_42"}, repo.lastLookupKey)
}

// TestReconcileReviewReplies_SkipsFailedReply — a failed reply tool must not
// flip the review (the public post never landed).
func TestReconcileReviewReplies_SkipsFailedReply(t *testing.T) {
	repo := &reconcilingReviewRepo{review: &domain.Review{
		ID:          "rev-stored-1",
		BusinessID:  "biz-1",
		Platform:    a2a.AgentYandexBusiness,
		ExternalID:  "yreview-77",
		ReplyStatus: domain.ReviewReplyStatusPending,
	}}
	turn := &Turn{deps: Deps{Reviews: repo}}

	toolCalls := []domain.ToolCall{{
		ID:        "c1",
		Name:      tools.YandexBusinessReplyReview,
		Arguments: map[string]interface{}{"review_id": "yreview-77", "text": "thx"},
	}}
	toolResults := []domain.ToolResult{{ToolCallID: "c1", IsError: true, Content: map[string]interface{}{"error": "captcha"}}}

	turn.recordPostsAndReviews(context.Background(), "biz-1", "msg-1", toolCalls, toolResults)

	assert.Empty(t, repo.updateCalls, "a failed reply must not reconcile the stored review")
}
