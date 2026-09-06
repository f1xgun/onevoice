package chatturn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

func TestRun_TerminalStreamOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, ending, code string
		wantErr            bool
	}{
		{name: "done", ending: "data: {\"type\":\"done\"}\n\n"},
		{name: "explicit error", ending: "data: {\"type\":\"error\",\"code\":\"PROVIDER_ERROR\"}\n\n", code: "PROVIDER_ERROR"},
		{name: "scanner failure", ending: strings.Repeat("x", 2<<20), code: "STREAM_INTERRUPTED", wantErr: true},
		{name: "premature EOF", code: "STREAM_INTERRUPTED", wantErr: true},
		{name: "scanner failure after done", ending: "data: {\"type\":\"done\"}\n\n" + strings.Repeat("x", 2<<20), code: "STREAM_INTERRUPTED", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("data: {\"type\":\"text\",\"content\":\"Partial answer\"}\n\n" + tc.ending))
			}))
			defer server.Close()
			user, business := uuid.New(), uuid.New()
			repo := newStatefulMsgRepo()
			turn := New(Deps{
				Business: ownershipStubBusiness{}, Integrations: ownershipStubInteg{}, Projects: ownershipStubProject{},
				Conversations: &freshTurnConvRepo{conv: &domain.Conversation{ID: "conversation", UserID: user.String(), BusinessID: business.String()}},
				Messages:      repo, Pending: noBatchPendingRepo{}, Orch: orchestratorclient.New(server.URL, server.Client()),
			})
			recorder := httptest.NewRecorder()
			outcome, err := turn.Run(context.Background(), recorder, TurnRequest{
				ConversationID: "conversation", UserID: user, BusinessID: business, Message: "Write a reply",
			}, nil)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			wantStatus, wantOutcome := domain.MessageStatusComplete, OutcomeDone
			if tc.code != "" {
				wantStatus, wantOutcome = domain.MessageStatusError, OutcomeError
				assert.Contains(t, recorder.Body.String(), tc.code)
			}
			assert.Equal(t, wantOutcome, outcome)
			require.Equal(t, 1, repo.updateCalls)
			for _, message := range repo.all {
				if message.Role == domain.MessageRoleAssistant {
					assert.Equal(t, wantStatus, message.Status)
					assert.Equal(t, tc.code, message.ErrorCode)
					assert.Equal(t, tc.code == "", message.SuccessfulOutcome)
					assert.Equal(t, "Partial answer", message.Content)
				}
			}
		})
	}
}
