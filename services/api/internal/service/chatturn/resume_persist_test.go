package chatturn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// The resume path never calls the enrich-side readers, so these satisfy New()'s
// non-nil invariants and nil-panic if ever touched.
type resumeStubBusiness struct{ BusinessReader }
type resumeStubInteg struct{ IntegrationLister }
type resumeStubProject struct{ ProjectReader }
type resumeStubConv struct{ domain.ConversationRepository }

type resumePendingRepo struct {
	domain.PendingToolCallRepository
	batch *domain.PendingToolCallBatch
}

func (r *resumePendingRepo) GetByBatchID(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return r.batch, nil
}

type resumeMsgRepo struct {
	domain.MessageRepository
	active  *domain.Message
	updated *domain.Message
}

func (r *resumeMsgRepo) FindByConversationActive(_ context.Context, _ string) (*domain.Message, error) {
	return r.active, nil
}

func (r *resumeMsgRepo) Update(_ context.Context, m *domain.Message) error {
	r.updated = m
	return nil
}

// TestResumeApproved_PersistsFinalizedMessage is the regression for the bug
// where POST /chat/{id}/resume streamed the approved-tool result to the browser
// but never wrote it back: the orchestrator marked the batch resolved while the
// message stayed pending_approval with no result, so a reload rendered the call
// as aborted (0/1) and every later message dead-ended on turn_already_in_progress.
// ResumeApproved must finalize the message — complete status, the tool result,
// and the call flipped to approved.
func TestResumeApproved_PersistsFinalizedMessage(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
	}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-1", ConversationID: "conv-1",
		}},
		Orch: orchestratorclient.New(orch.URL, http.DefaultClient),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.NotNil(t, msgRepo.updated, "resume MUST persist the finalized message")
	assert.Equal(t, domain.MessageStatusComplete, msgRepo.updated.Status)
	require.Len(t, msgRepo.updated.ToolResults, 1, "tool result must be persisted")
	assert.Equal(t, "tc_a", msgRepo.updated.ToolResults[0].ToolCallID)
	assert.Equal(t, domain.ToolCallStatusApproved, msgRepo.updated.ToolCalls[0].Status)
}

// TestResumeApproved_RePause_KeepsMessageActive is the regression for the
// sequential fan-out bug: gpt-oss-120b:free emits one tool call per turn, so
// approving Yandex resumes the loop which immediately pauses AGAIN on Telegram.
// The orchestrator emits tool_result(yandex) + tool_approval_required(next) with
// NO done. The pre-fix fallback marked the message Complete, so the next approve
// found no active message and failed with no_active_approval_for_conversation
// ("only Yandex fired"). ResumeApproved must keep the message PendingApproval,
// flip Yandex to approved, and append the Telegram call as pending.
func TestResumeApproved_RePause_KeepsMessageActive(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_yandex","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_approval_required","batch_id":"batch-tg","calls":[{"call_id":"tc_tg","tool_name":"telegram__send_channel_post","args":{"text":"hi"}}]}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_yandex", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
	}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-yandex", ConversationID: "conv-1",
		}},
		Orch: orchestratorclient.New(orch.URL, http.DefaultClient),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-yandex", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomePauseHITL, outcome, "re-pause must NOT finalize the turn")

	require.NotNil(t, msgRepo.updated, "re-pause MUST persist the still-active message")
	assert.Equal(t, domain.MessageStatusPendingApproval, msgRepo.updated.Status,
		"message must stay active so the next approve finds it")
	require.Len(t, msgRepo.updated.ToolCalls, 2, "Telegram call must be appended")
	assert.Equal(t, domain.ToolCallStatusApproved, msgRepo.updated.ToolCalls[0].Status,
		"Yandex flips to approved after its result")
	assert.Equal(t, "tc_tg", msgRepo.updated.ToolCalls[1].ID)
	assert.Equal(t, "telegram__send_channel_post", msgRepo.updated.ToolCalls[1].Name)
	assert.Equal(t, domain.ToolCallStatusPending, msgRepo.updated.ToolCalls[1].Status)
	assert.Equal(t, "batch-tg-tc_tg", msgRepo.updated.ToolCalls[1].ApprovalID)
	require.Len(t, msgRepo.updated.ToolResults, 1)
	assert.Equal(t, "tc_yandex", msgRepo.updated.ToolResults[0].ToolCallID)
}

// TestResumeApproved_NoActiveMessage_InlineError — if there is no active message
// to finalize (anomalous), ResumeApproved must not proceed to stream; it emits
// an inline error instead of stranding a half-run resume.
func TestResumeApproved_NoActiveMessage_InlineError(t *testing.T) {
	msgRepo := &resumeMsgRepo{active: nil}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-1", ConversationID: "conv-1",
		}},
		Orch: orchestratorclient.New("http://unused", http.DefaultClient),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeInlineError, outcome)
	assert.Contains(t, rr.Body.String(), "no_active_approval_for_conversation")
	assert.Nil(t, msgRepo.updated)
}
