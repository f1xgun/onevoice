package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tools"
)

type immediateScheduleFake struct{ calls int }

func (f *immediateScheduleFake) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return f.ExecuteWithApproval(ctx, args, "")
}
func (f *immediateScheduleFake) ExecuteWithApproval(context.Context, map[string]interface{}, string) (interface{}, error) {
	f.calls++
	return map[string]interface{}{"message_id": 42}, nil
}
func TestTelegramApprovedScheduleNeverDispatchesImmediately(t *testing.T) {
	for _, tc := range []struct {
		name, date, proof string
		wantHTTP          bool
		wantError         bool
	}{
		{name: "approved", date: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), proof: "approval", wantHTTP: true},
		{name: "missing proof", date: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), wantError: true},
		{name: "bad time", date: "tomorrow", proof: "approval", wantError: true},
		{name: "past replay", date: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), proof: "approval", wantHTTP: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "/internal/v1/posts/schedule", r.URL.Path)
				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "business", body["business_id"])
				require.Equal(t, "conversation", body["conversation_id"])
				require.Equal(t, tc.proof, body["approval_id"])
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "scheduled", "post_record_id": "record", "api_owned_schedule": true, "external_id": "@own"})
			}))
			defer srv.Close()
			immediate := &immediateScheduleFake{}
			executor := &telegramScheduleExecutor{tool: tools.TelegramSendChannelPost, baseURL: srv.URL, client: srv.Client(), immediate: immediate}
			ctx := a2a.WithConversationID(a2a.WithBusinessID(context.Background(), "business"), "conversation")
			result, err := executor.ExecuteWithApproval(ctx, map[string]interface{}{"channel_id": "@own", "text": "text", "scheduled_at": tc.date}, tc.proof)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "scheduled", result.(map[string]interface{})["status"])
			}
			require.Zero(t, immediate.calls)
			require.Equal(t, tc.wantHTTP, calls == 1)
		})
	}
}

func TestTelegramScheduleAPIFailureNeverFallsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	immediate := &immediateScheduleFake{}
	executor := &telegramScheduleExecutor{tool: tools.TelegramSendChannelPost, baseURL: server.URL, client: server.Client(), immediate: immediate}
	_, err := executor.ExecuteWithApproval(context.Background(), map[string]interface{}{"scheduled_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, "proof")
	require.Error(t, err)
	require.Zero(t, immediate.calls)
}

func TestTelegramExplicitNowRequiresAPIApproval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "", request["args"].(map[string]interface{})["scheduled_at"])
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "approved_now", "approval_id": "proof", "external_id": "@own"})
	}))
	defer server.Close()
	immediate := &immediateScheduleFake{}
	executor := &telegramScheduleExecutor{tool: tools.TelegramSendChannelPost, baseURL: server.URL, client: server.Client(), immediate: immediate}
	_, err := executor.ExecuteWithApproval(context.Background(), map[string]interface{}{"scheduled_at": "", "text": "now"}, "proof")
	require.NoError(t, err)
	require.Equal(t, 1, immediate.calls)
	_, err = executor.ExecuteWithApproval(context.Background(), map[string]interface{}{"scheduled_at": "", "text": "now"}, "")
	require.Error(t, err)
	require.Equal(t, 1, immediate.calls)
}
