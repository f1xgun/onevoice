package chatturn

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// fakeAuditLogger captures every emitted Entry so the test can assert on the
// audit trail shape without a database.
type fakeAuditLogger struct {
	entries []audit.Entry
}

func (f *fakeAuditLogger) Log(_ context.Context, e audit.Entry) { f.entries = append(f.entries, e) }
func (f *fakeAuditLogger) LogSync(_ context.Context, e audit.Entry) error {
	f.entries = append(f.entries, e)
	return nil
}

// TestAuditRPAMutations_EmitsOnlySuccessfulRPAMutations is the core guard: one
// audit row per landed RPA write, none for reads, non-RPA tools, or failures,
// attributed to the business + actor, and carrying NO review/photo content.
func TestAuditRPAMutations_EmitsOnlySuccessfulRPAMutations(t *testing.T) {
	biz := uuid.New()
	actor := uuid.New()
	fake := &fakeAuditLogger{}
	turn := &Turn{deps: Deps{Audit: fake}}

	toolCalls := []domain.ToolCall{
		{ID: "c1", Name: tools.YandexBusinessReplyReview, Arguments: map[string]interface{}{"review_id": "rev-42", "text": "secret reply text"}},
		{ID: "c2", Name: tools.YandexBusinessUploadPhoto, Arguments: map[string]interface{}{"photo_url": "http://x/p.png"}},
		{ID: "c3", Name: tools.YandexBusinessGetReviews},                                                 // read — not audited
		{ID: "c4", Name: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}}, // non-RPA
		{ID: "c5", Name: tools.YandexBusinessUpdateHours, Arguments: map[string]interface{}{"hours": "9-18"}},
	}
	toolResults := []domain.ToolResult{
		{ToolCallID: "c1", IsError: false},
		{ToolCallID: "c2", IsError: false},
		{ToolCallID: "c3", IsError: false},
		{ToolCallID: "c4", IsError: false},
		{ToolCallID: "c5", IsError: true}, // failed mutation — not audited
	}

	turn.auditRPAMutations(context.Background(), biz.String(), actor.String(), toolCalls, toolResults)

	require.Len(t, fake.entries, 2)
	byAction := map[string]audit.Entry{}
	for _, e := range fake.entries {
		byAction[e.Action] = e
	}

	reply, ok := byAction[audit.ActionRPAReviewReplied]
	require.True(t, ok, "expected a review-replied audit entry")
	require.NotNil(t, reply.BusinessID)
	assert.Equal(t, biz, *reply.BusinessID)
	require.NotNil(t, reply.UserID)
	assert.Equal(t, actor, *reply.UserID)
	assert.Contains(t, string(reply.Details), "rev-42")
	assert.NotContains(t, string(reply.Details), "secret reply text")

	photo, ok := byAction[audit.ActionRPAPhotoUploaded]
	require.True(t, ok, "expected a photo-uploaded audit entry")
	assert.NotContains(t, string(photo.Details), "p.png")

	_, hoursAudited := byAction[audit.ActionRPAHoursUpdated]
	assert.False(t, hoursAudited, "failed mutation must not be audited")
}

// TestAuditRPAMutations_MalformedActorStillBusinessScoped: an unresolvable
// actor must not drop the row — it stays attributable to the business.
func TestAuditRPAMutations_MalformedActorStillBusinessScoped(t *testing.T) {
	biz := uuid.New()
	fake := &fakeAuditLogger{}
	turn := &Turn{deps: Deps{Audit: fake}}

	turn.auditRPAMutations(context.Background(), biz.String(), "not-a-uuid",
		[]domain.ToolCall{{ID: "c1", Name: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "x"}}},
		[]domain.ToolResult{{ToolCallID: "c1", IsError: false}})

	require.Len(t, fake.entries, 1)
	assert.Equal(t, audit.ActionRPAPostPublished, fake.entries[0].Action)
	assert.Nil(t, fake.entries[0].UserID)
	require.NotNil(t, fake.entries[0].BusinessID)
	assert.Equal(t, biz, *fake.entries[0].BusinessID)
}

// TestAuditRPAMutations_NilLoggerNoop: no Audit dep → no panic, no work.
func TestAuditRPAMutations_NilLoggerNoop(t *testing.T) {
	turn := &Turn{deps: Deps{}}
	turn.auditRPAMutations(context.Background(), uuid.NewString(), uuid.NewString(),
		[]domain.ToolCall{{ID: "c1", Name: tools.YandexBusinessReplyReview}},
		[]domain.ToolResult{{ToolCallID: "c1"}})
}
