package chatturn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

type approvalEvents struct{ events []approvaltelemetry.Event }

func (s *approvalEvents) RecordApproval(_ context.Context, events ...approvaltelemetry.Event) {
	s.events = append(s.events, events...)
}

func TestResumeApproved_CorrelatesOnlyApprovedPostAndReviewResults(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","tool_name":"private@example.com","result":{"text":"PRIVATE POST"}}

data: {"type":"tool_result","tool_call_id":"tc_a","result":{"text":"PRIVATE DUPLICATE"}}

data: {"type":"tool_result","tool_call_id":"tc_b","error":"PRIVATE ERROR","result":{"text":"PRIVATE REVIEW"}}

data: {"type":"tool_result","tool_call_id":"tc_rejected","result":{"text":"PRIVATE REJECTED"}}

data: {"type":"tool_result","tool_call_id":"orphan","result":{"ok":true}}

data: {"type":"done"}

`))
	}))
	defer orch.Close()
	sink := &approvalEvents{}
	turn := New(Deps{
		Business: resumeStubBusiness{}, Integrations: resumeStubInteg{}, Projects: resumeStubProject{}, Conversations: resumeStubConv{},
		Messages: &resumeMsgRepo{active: &domain.Message{ID: "msg-1", ConversationID: "conv-1", Status: domain.MessageStatusPendingApproval, ToolCalls: []domain.ToolCall{
			{ID: "tc_a", Name: tools.TelegramSendChannelPost},
			{ID: "tc_b", Name: tools.YandexBusinessReplyReview},
			{ID: "tc_rejected", Name: tools.VKPublishPost},
		}}},
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{ID: "batch-1", ConversationID: "conv-1", Calls: []domain.PendingCall{
			{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Verdict: "edit"},
			{CallID: "tc_b", ToolName: tools.YandexBusinessReplyReview, Verdict: "approve"},
			{CallID: "tc_rejected", ToolName: tools.VKPublishPost, Verdict: "reject"},
		}}},
		Orch: orchestratorclient.New(orch.URL, http.DefaultClient),
	})
	turn.SetApprovalTelemetry(sink)
	_, err := turn.ResumeApproved(context.Background(), httptest.NewRecorder(), "conv-1", "batch-1", nil)
	require.NoError(t, err)
	require.Equal(t, []approvaltelemetry.Event{
		{BatchID: "batch-1", DraftID: "batch-1-tc_a", ApprovalID: "batch-1-tc_a", Kind: "post", Source: "chat", Action: "send_result", Outcome: "success"},
		{BatchID: "batch-1", DraftID: "batch-1-tc_b", ApprovalID: "batch-1-tc_b", Kind: "review_reply", Source: "chat", Action: "send_result", Outcome: "error"},
	}, sink.events)
}
