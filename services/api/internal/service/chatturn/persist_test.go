package chatturn

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// fakeMessageRepo records Create/Update calls so the pause-persist tests can
// assert on the persisted Message shape. Unimplemented methods nil-panic —
// these tests only drive persistAfterStream's branches. The fresh-turn
// lifecycle reserves an in_progress placeholder via Create, then finalizes it
// via Update, so the finalize assertions read from `updated`.
type fakeMessageRepo struct {
	domain.MessageRepository
	created []domain.Message
	updated []domain.Message
}

func (f *fakeMessageRepo) Create(_ context.Context, m *domain.Message) error {
	f.created = append(f.created, *m)
	return nil
}

func (f *fakeMessageRepo) Update(_ context.Context, m *domain.Message) error {
	f.updated = append(f.updated, *m)
	return nil
}

// bumpRecordingConv records BumpLastMessageAt calls so the persist-path tests
// can assert the recency sort key is advanced on every message append.
type bumpRecordingConv struct {
	domain.ConversationRepository
	bumps []struct {
		id string
		ts time.Time
	}
}

func (c *bumpRecordingConv) BumpLastMessageAt(_ context.Context, id string, ts time.Time) error {
	c.bumps = append(c.bumps, struct {
		id string
		ts time.Time
	}{id: id, ts: ts})
	return nil
}

// TestPersistPaths_BumpLastMessageAt — every message-append path
// (user / pause / complete) must advance the conversation's last_message_at so
// it never sinks/truncates out of the search allowlist. The bump uses the
// message's own CreatedAt.
func TestPersistPaths_BumpLastMessageAt(t *testing.T) {
	cases := []struct {
		name    string
		persist func(*Turn, context.Context, *domain.Message) error
	}{
		{"user", (*Turn).persistUserMessage},
		{"pause", (*Turn).persistAssistantPause},
		{"complete", (*Turn).persistAssistantComplete},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conv := &bumpRecordingConv{}
			turn := &Turn{deps: Deps{Messages: &fakeMessageRepo{}, Conversations: conv}}
			ts := time.Now().Truncate(time.Millisecond)
			msg := &domain.Message{ConversationID: "conv-bump", CreatedAt: ts}

			require.NoError(t, c.persist(turn, context.Background(), msg))

			require.Len(t, conv.bumps, 1, "append must bump last_message_at exactly once")
			assert.Equal(t, "conv-bump", conv.bumps[0].id)
			assert.Equal(t, ts, conv.bumps[0].ts, "bump must use the appended message's CreatedAt")
		})
	}
}

// TestPersistAfterStream_Pause_PersistsApprovalRequiredToolCalls — approval-
// required tools arrive only in the tool_approval_required pause event (not as
// tool_call frames), so state.toolCalls is empty. The persisted
// pending_approval message must still carry the calls from pauseEvent.Calls;
// otherwise a page reload renders an empty assistant bubble because the calls
// would live only in the pending_tool_calls batch.
func TestPersistAfterStream_Pause_PersistsApprovalRequiredToolCalls(t *testing.T) {
	repo := &fakeMessageRepo{}
	turn := &Turn{deps: Deps{Messages: repo}}

	state := &streamState{
		pauseEvent: &sse.Event{
			Type:    "tool_approval_required",
			BatchID: "batch-9",
			Calls: []sse.ApprovalCall{{
				CallID:   "call-1",
				ToolName: tools.TelegramSendChannelPost,
				Args:     map[string]interface{}{"text": "hi"},
			}},
		},
	}
	req := TurnRequest{ConversationID: "conv-1", Locale: language.Russian}

	turn.persistAfterStream(context.Background(), req, &enrichmentResult{}, "msg-1", time.Now(), state)

	require.Len(t, repo.updated, 1, "pause branch must finalize exactly one message")
	msg := repo.updated[0]
	assert.Equal(t, domain.MessageStatusPendingApproval, msg.Status)
	require.Len(t, msg.ToolCalls, 1, "approval-required call must be persisted on the message")
	assert.Equal(t, "call-1", msg.ToolCalls[0].ID)
	assert.Equal(t, tools.TelegramSendChannelPost, msg.ToolCalls[0].Name)
	assert.Equal(t, "batch-9-call-1", msg.ToolCalls[0].ApprovalID)
	assert.Equal(t, domain.ToolCallStatusPending, msg.ToolCalls[0].Status)
}

// TestPersistAfterStream_Pause_MergesAutoAndPendingCalls — a turn can
// auto-execute one tool (streamed as a tool_call + tool_result) and then pause
// on a second that needs approval. Both must land on the persisted message:
// the auto call (with its result) and the pending one.
func TestPersistAfterStream_Pause_MergesAutoAndPendingCalls(t *testing.T) {
	repo := &fakeMessageRepo{}
	turn := &Turn{deps: Deps{Messages: repo}}

	state := &streamState{
		toolCalls: []domain.ToolCall{{
			ID:        "auto-1",
			Name:      tools.VKPublishPost,
			Arguments: map[string]interface{}{"text": "done"},
		}},
		toolResults: []domain.ToolResult{{
			ToolCallID: "auto-1",
			Content:    map[string]interface{}{"ok": true},
		}},
		pauseEvent: &sse.Event{
			Type:    "tool_approval_required",
			BatchID: "batch-7",
			Calls: []sse.ApprovalCall{{
				CallID:   "pending-1",
				ToolName: tools.TelegramSendChannelPost,
				Args:     map[string]interface{}{"text": "later"},
			}},
		},
	}
	req := TurnRequest{ConversationID: "conv-2", Locale: language.English}

	turn.persistAfterStream(context.Background(), req, &enrichmentResult{}, "msg-2", time.Now(), state)

	require.Len(t, repo.updated, 1)
	msg := repo.updated[0]
	require.Len(t, msg.ToolCalls, 2, "both the auto and the pending call must persist")
	assert.Equal(t, "auto-1", msg.ToolCalls[0].ID)
	assert.Empty(t, msg.ToolCalls[0].Status, "auto-executed call is not pending approval")
	assert.Equal(t, "pending-1", msg.ToolCalls[1].ID)
	assert.Equal(t, domain.ToolCallStatusPending, msg.ToolCalls[1].Status)
	require.Len(t, msg.ToolResults, 1, "the auto call's result must persist")
	assert.Equal(t, "auto-1", msg.ToolResults[0].ToolCallID)
}

// TestPersistAfterStream_Pause_DedupesCallInBothStreams — some providers emit a
// tool_call frame AND list the same call in the pause event. The call must be
// persisted exactly once, in its pending form (ApprovalID + pending status),
// not duplicated.
func TestPersistAfterStream_Pause_DedupesCallInBothStreams(t *testing.T) {
	repo := &fakeMessageRepo{}
	turn := &Turn{deps: Deps{Messages: repo}}

	state := &streamState{
		toolCalls: []domain.ToolCall{{
			ID:        "toolu_abc",
			Name:      tools.TelegramSendChannelPost,
			Arguments: map[string]interface{}{"text": "привет"},
		}},
		pauseEvent: &sse.Event{
			Type:    "tool_approval_required",
			BatchID: "batch-1",
			Calls: []sse.ApprovalCall{{
				CallID:   "toolu_abc",
				ToolName: tools.TelegramSendChannelPost,
				Args:     map[string]interface{}{"text": "привет"},
			}},
		},
	}
	req := TurnRequest{ConversationID: "conv-3", Locale: language.Russian}

	turn.persistAfterStream(context.Background(), req, &enrichmentResult{}, "msg-3", time.Now(), state)

	require.Len(t, repo.updated, 1)
	msg := repo.updated[0]
	require.Len(t, msg.ToolCalls, 1, "a call in both streams must persist exactly once")
	assert.Equal(t, "toolu_abc", msg.ToolCalls[0].ID)
	assert.Equal(t, "batch-1-toolu_abc", msg.ToolCalls[0].ApprovalID)
	assert.Equal(t, domain.ToolCallStatusPending, msg.ToolCalls[0].Status)
}

// TestPersistContext_PropagatesCorrelationIDAndLocale — the async persist +
// auto-titler goroutines run on the detached persistContext, so their log
// lines can only be correlated to the request edge if correlation_id (and
// locale) ride along. The docstring promises both; assert the value copy.
func TestPersistContext_PropagatesCorrelationIDAndLocale(t *testing.T) {
	turn := &Turn{}

	parent := logger.WithCorrelationID(context.Background(), "corr-abc-123")
	parent = i18n.WithLocale(parent, language.Russian)

	ctx, cancel := turn.persistContext(parent)
	defer cancel()

	assert.Equal(t, "corr-abc-123", logger.CorrelationIDFromContext(ctx),
		"persist context must carry the parent's correlation_id")
	assert.Equal(t, language.Russian, i18n.LocaleFromContext(ctx),
		"persist context must carry the parent's locale")
}

// TestPersistContext_DetachesFromParentCancellation — persistContext must NOT
// inherit the request ctx's cancellation/deadline (that's the whole point: the
// SSE ctx dies when the user navigates away). Only the values are copied.
func TestPersistContext_DetachesFromParentCancellation(t *testing.T) {
	turn := &Turn{}

	parent, parentCancel := context.WithCancel(logger.WithCorrelationID(context.Background(), "corr-detach"))
	parentCancel()

	ctx, cancel := turn.persistContext(parent)
	defer cancel()

	require.NoError(t, ctx.Err(), "detached persist context must not be canceled by the parent")
	assert.Equal(t, "corr-detach", logger.CorrelationIDFromContext(ctx),
		"values must still be copied off a canceled parent")
}
