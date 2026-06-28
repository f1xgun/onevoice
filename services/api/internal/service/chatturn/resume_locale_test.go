package chatturn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"golang.org/x/text/language"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// TestResumeStreamHeaders_ForwardsCtxLocale is the fail-on-revert guard for the
// HITL resume locale fix: the orchestrator resume endpoint has no locale body
// field, so the only way the post-approval LLM turn gets the right (EN vs RU)
// tool definitions is the Accept-Language header derived from the request ctx.
// Without the Headers line in runResumeStream the orchestrator's LocaleMiddleware
// resolves an empty header to the default tag (RU) for every tenant.
func TestResumeStreamHeaders_ForwardsCtxLocale(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "english locale propagates as en",
			ctx:  i18n.WithLocale(context.Background(), language.English),
			want: "en",
		},
		{
			name: "russian locale propagates as ru",
			ctx:  i18n.WithLocale(context.Background(), language.Russian),
			want: "ru",
		},
		{
			name: "missing locale falls back to default ru",
			ctx:  context.Background(),
			want: "ru",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resumeStreamHeaders(tc.ctx)
			assert.Equal(t, tc.want, got["Accept-Language"],
				"resume stream must forward the ctx locale as Accept-Language so the orchestrator localizes tool definitions")
		})
	}
}

// TestResumeApproved_ForwardsAcceptLanguageHeader proves the locale propagates
// end-to-end through the orchestrator client: an EN-locale request ctx makes the
// orchestrator's resume endpoint receive Accept-Language: en. Reverting the
// Headers line makes the orchestrator see no Accept-Language (resolved to RU by
// its LocaleMiddleware), degrading tool selection for EN tenants.
func TestResumeApproved_ForwardsAcceptLanguageHeader(t *testing.T) {
	var (
		mu        sync.Mutex
		gotHeader string
	)
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotHeader = r.Header.Get("Accept-Language")
		mu.Unlock()
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

	ctx := i18n.WithLocale(context.Background(), language.English)
	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(ctx, rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "en", gotHeader,
		"EN-locale resume must reach the orchestrator with Accept-Language: en so the post-approval tools are localized to EN")
}
