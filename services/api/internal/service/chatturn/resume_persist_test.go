package chatturn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// The resume path never calls the enrich-side readers, so these satisfy New()'s
// non-nil invariants and nil-panic if ever touched.
type resumeStubBusiness struct{ BusinessReader }
type resumeStubInteg struct{ IntegrationLister }
type resumeStubProject struct{ ProjectReader }
type resumeStubConv struct{ domain.ConversationRepository }

// BumpLastMessageAt is a no-op override so the resume write-back paths can bump
// the recency sort key without nil-panicking on the embedded interface.
func (resumeStubConv) BumpLastMessageAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}

// GetByID reports not-found so the ownership gate treats these resume fixtures as
// a pass-through (the resume path's behavior under test is independent of the
// conversation row). The cross-tenant rejection is proven by a dedicated test
// that injects an owner-mismatched conversation.
func (resumeStubConv) GetByID(_ context.Context, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrConversationNotFound
}

type resumePendingRepo struct {
	domain.PendingToolCallRepository
	batch *domain.PendingToolCallBatch
}

func (r *resumePendingRepo) GetByBatchID(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return r.batch, nil
}

// AtomicTransitionResolvingToResuming always lets the single resume in these
// fixtures win the claim — the concurrency rejection is exercised by a
// dedicated test that races two ResumeApproved calls.
func (r *resumePendingRepo) AtomicTransitionResolvingToResuming(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return r.batch, nil
}

type resumeMsgRepo struct {
	domain.MessageRepository
	active      *domain.Message
	updated     *domain.Message
	updateErr   error
	updateCalls int
}

func (r *resumeMsgRepo) FindByConversationActive(_ context.Context, _ string) (*domain.Message, error) {
	return r.active, nil
}

func (r *resumeMsgRepo) Update(_ context.Context, m *domain.Message) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
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

// TestResumeApproved_RecordsPostsOnDone — the resume path is where manual-floor
// publishing tools actually execute, so it is the only place a Post record can
// be created. The orchestrator SSE emits tool_call(create_post) → tool_result
// (success) → done; the resume must record exactly one Post and drive the
// AgentTask lifecycle (one Create at running, one Update at done). Before the
// fix the resume path proxied these frames straight to the browser without the
// postal hooks, so the publish vanished from the feed and the Tasks page.
func TestResumeApproved_RecordsPostsOnDone(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_a","tool_name":"yandex_business__create_post","tool_args":{"text":"hello world"}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	businessID := uuid.New().String()
	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_a", Name: "yandex_business__create_post", Status: domain.ToolCallStatusPending},
			},
		},
	}
	posts := &fakePostRepo{}
	tasks := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "yandex_business"}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Posts:         posts,
		AgentTasks:    tasks,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-1", ConversationID: "conv-1", BusinessID: businessID,
		}},
		Orch: orchestratorclient.New(orch.URL, http.DefaultClient),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.Len(t, posts.created, 1, "approved create_post must record exactly one Post")
	assert.Equal(t, "hello world", posts.created[0].Content)
	assert.Equal(t, businessID, posts.created[0].BusinessID)
	_, hasYandex := posts.created[0].PlatformResults["yandex_business"]
	assert.True(t, hasYandex, "post recorded under the yandex_business platform")

	require.Len(t, tasks.created, 1, "one AgentTask created in running state")
	assert.Equal(t, "running", tasks.created[0].Status)
	require.Len(t, tasks.updated, 1, "one AgentTask transitioned to done")
	assert.Equal(t, "done", tasks.updated[0].Status)
}

// TestResumeApproved_FlipsTokenExpiredOnCodedError — when an approved tool's
// resume result carries code:"integration_token_invalid", the resume path must
// flip the integration to token_expired so the dashboard prompts a reconnect.
// Before the fix the resume tool_result skipped onToolResult entirely, so the
// reconnect badge never fired on the manual-approve floor.
func TestResumeApproved_FlipsTokenExpiredOnCodedError(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_a","tool_name":"telegram__send_channel_post","tool_args":{"text":"hi"}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"error":"Unauthorized: bot kicked"},"error":"telegram: Unauthorized: bot kicked","code":"integration_token_invalid"}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	businessID := uuid.New()
	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_a", Name: "telegram__send_channel_post", Status: domain.ToolCallStatusPending},
			},
		},
	}
	integ := &fakeIntegrations{}
	tasks := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "telegram"}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  integ,
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		AgentTasks:    tasks,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-1", ConversationID: "conv-1", BusinessID: businessID.String(),
		}},
		Orch: orchestratorclient.New(orch.URL, http.DefaultClient),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.Len(t, integ.marked, 1, "MarkTokenExpired must fire once on integration_token_invalid")
	assert.Equal(t, businessID, integ.marked[0].businessID)
	assert.Equal(t, "telegram", integ.marked[0].platform)
}

// TestResumeApproved_RecordsExecutedToolOnRePause — in a sequential fan-out the
// resume loop executes the approved tool and then RE-pauses on the next
// manual-floor tool (gpt-oss-120b:free emits one tool call per turn). The tool
// that actually ran in THIS resume stream is never re-emitted on later resumes,
// so its Post record and RPA audit row must be written on the re-pause path —
// not deferred to a terminal 'done' that, for this tool, never arrives. The
// pre-fix re-pause skipped recordPostsAndReviews/auditRPAMutations entirely, so
// every approved post except the final one silently vanished from the feed and
// its 152-FZ "who changed the third-party listing" audit row was lost.
func TestResumeApproved_RecordsExecutedToolOnRePause(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_yandex","tool_name":"yandex_business__create_post","tool_args":{"text":"ya"}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_yandex","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_approval_required","batch_id":"batch-tg","calls":[{"call_id":"tc_tg","tool_name":"telegram__send_channel_post","args":{"text":"hi"}}]}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	businessID := uuid.New().String()
	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_yandex", Name: "yandex_business__create_post", Status: domain.ToolCallStatusPending},
			},
		},
	}
	posts := &fakePostRepo{}
	tasks := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "yandex_business"}
	auditLog := &fakeAuditLogger{}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Posts:         posts,
		AgentTasks:    tasks,
		Audit:         auditLog,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-yandex", ConversationID: "conv-1", BusinessID: businessID,
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

	require.Len(t, posts.created, 1,
		"the tool executed in this resume stream must be recorded on re-pause, not dropped")
	assert.Equal(t, "ya", posts.created[0].Content)
	assert.Equal(t, businessID, posts.created[0].BusinessID)
	assert.Equal(t, "published", posts.created[0].Status)
	yandexResult, hasYandex := posts.created[0].PlatformResults["yandex_business"]
	require.True(t, hasYandex, "post recorded under the yandex_business platform")
	assert.Equal(t, "published", yandexResult.Status)

	require.Len(t, auditLog.entries, 1,
		"the executed RPA write must leave one audit row on re-pause")
	assert.Equal(t, audit.ActionRPAPostPublished, auditLog.entries[0].Action)
}

// TestResumeApproved_RecordsExecutedToolOnNoTerminalFallback — when the
// orchestrator stream ends after an approved tool has already executed but
// WITHOUT any terminal event (no done/error AND no re-pause — the post-approval
// LLM continuation stalls past the budget or the connection drops, including a
// clean EOF), runResumeStream falls through to the post-stream fallback branch.
// That branch finalizes the message but must ALSO record the post and write the
// RPA audit row for the tool that already published live on the third-party
// platform — otherwise an already-published post is dropped from the feed and
// the 152-FZ "who changed the third-party listing" audit row is lost.
func TestResumeApproved_RecordsExecutedToolOnNoTerminalFallback(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_yandex","tool_name":"yandex_business__create_post","tool_args":{"text":"ya"}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_yandex","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	businessID := uuid.New().String()
	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_yandex", Name: "yandex_business__create_post", Status: domain.ToolCallStatusPending},
			},
		},
	}
	posts := &fakePostRepo{}
	tasks := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "yandex_business"}
	auditLog := &fakeAuditLogger{}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Posts:         posts,
		AgentTasks:    tasks,
		Audit:         auditLog,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-yandex", ConversationID: "conv-1", BusinessID: businessID,
		}},
		Orch: orchestratorclient.New(orch.URL, http.DefaultClient),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-yandex", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.NotNil(t, msgRepo.updated, "fallback MUST persist the finalized message")
	assert.Equal(t, domain.MessageStatusComplete, msgRepo.updated.Status)

	require.Len(t, posts.created, 1,
		"the tool executed before the stream dropped must be recorded on the fallback, not dropped")
	assert.Equal(t, "ya", posts.created[0].Content)
	assert.Equal(t, businessID, posts.created[0].BusinessID)
	assert.Equal(t, "published", posts.created[0].Status)

	require.Len(t, auditLog.entries, 1,
		"the executed RPA write must leave one audit row on the no-terminal fallback")
	assert.Equal(t, audit.ActionRPAPostPublished, auditLog.entries[0].Action)
}

// TestResumeApproved_NoDoubleRecord_RePauseThenDone proves the executed tool is
// recorded EXACTLY ONCE across a re-pause-then-done chain: each resume stream
// carries its own freshly-accumulated recCalls/recResults holding only the tool
// that ran in THAT stream, so re-pause records the first tool and the following
// 'done' resume records the second — never the first again.
func TestResumeApproved_NoDoubleRecord_RePauseThenDone(t *testing.T) {
	rePauseOrch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_yandex","tool_name":"yandex_business__create_post","tool_args":{"text":"ya"}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_yandex","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_approval_required","batch_id":"batch-tg","calls":[{"call_id":"tc_tg","tool_name":"vk__publish_post","args":{"text":"vk"}}]}` + "\n\n"))
		fl.Flush()
	}))
	defer rePauseOrch.Close()

	businessID := uuid.New().String()
	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_yandex", Name: "yandex_business__create_post", Status: domain.ToolCallStatusPending},
			},
		},
	}
	posts := &fakePostRepo{}
	tasks := &fakeAgentTaskRepoWithUpdate{reloadPlatform: "yandex_business"}
	auditLog := &fakeAuditLogger{}
	deps := Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Posts:         posts,
		AgentTasks:    tasks,
		Audit:         auditLog,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-yandex", ConversationID: "conv-1", BusinessID: businessID,
		}},
		Orch: orchestratorclient.New(rePauseOrch.URL, http.DefaultClient),
	}
	turn := New(deps)

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-yandex", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomePauseHITL, outcome)
	require.Len(t, posts.created, 1, "first resume records the Yandex post once")

	doneOrch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_tg","tool_name":"vk__publish_post","tool_args":{"text":"vk"}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_tg","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer doneOrch.Close()

	deps.Orch = orchestratorclient.New(doneOrch.URL, http.DefaultClient)
	deps.Pending = &resumePendingRepo{batch: &domain.PendingToolCallBatch{
		ID: "batch-tg", ConversationID: "conv-1", BusinessID: businessID,
	}}
	turn2 := New(deps)

	rr2 := httptest.NewRecorder()
	outcome2, err := turn2.ResumeApproved(context.Background(), rr2, "conv-1", "batch-tg", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome2)

	require.Len(t, posts.created, 2,
		"the done resume records only the VK post — the Yandex post is NOT recorded a second time")
	assert.Equal(t, "ya", posts.created[0].Content)
	assert.Equal(t, "vk", posts.created[1].Content)
	require.Len(t, auditLog.entries, 1,
		"only the Yandex RPA write is audited; VK is an API publish, not an RPA mutation")
}
