package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type internalScheduleProof struct {
	domain.PendingToolCallRepository
	batch *domain.PendingToolCallBatch
}

func (r internalScheduleProof) GetByBatchID(context.Context, string) (*domain.PendingToolCallBatch, error) {
	return r.batch, nil
}

type internalScheduleConversation struct {
	domain.ConversationRepository
	conversation *domain.Conversation
}

func (r internalScheduleConversation) GetByID(context.Context, string) (*domain.Conversation, error) {
	return r.conversation, nil
}

type internalScheduleIntegration struct {
	domain.IntegrationRepository
	business uuid.UUID
}

func (r internalScheduleIntegration) GetByBusinessPlatformExternal(context.Context, uuid.UUID, string, string) (*domain.Integration, error) {
	return &domain.Integration{BusinessID: r.business, ExternalID: "@own", Platform: "telegram", Status: "active"}, nil
}

type internalScheduleDispatch struct{ calls int }

func (n *internalScheduleDispatch) RequestMsgWithContext(context.Context, *natslib.Msg) (*natslib.Msg, error) {
	n.calls++
	return nil, context.DeadlineExceeded
}

func TestInternalApprovedScheduleHTTPReplay(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("isolated MONGODB_TEST_URI required")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("test_schedule_bridge_" + uuid.NewString()[:8])
	t.Cleanup(func() {
		require.NoError(t, db.Drop(context.Background()))
		require.NoError(t, client.Disconnect(context.Background()))
	})
	business, user, batchID := uuid.New(), uuid.New(), uuid.NewString()
	args := map[string]interface{}{"channel_id": "@own", "text": "approved text", "scheduled_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	batch := &domain.PendingToolCallBatch{ID: batchID, UserID: user.String(), BusinessID: business.String(), ConversationID: "conversation", MessageID: "message", Status: "resuming", Calls: []domain.PendingCall{{CallID: "call", ToolName: tools.TelegramSendChannelPost, Verdict: "approve", Arguments: args}}}
	posts := repository.NewPostRepository(db)
	dispatch := &internalScheduleDispatch{}
	svc := service.WithPostApproval(service.WithPostScheduling(service.WithPostDispatcher(service.NewPostService(posts, nil), dispatch), internalScheduleIntegration{business: business}), internalScheduleProof{batch: batch}, internalScheduleConversation{conversation: &domain.Conversation{ID: "conversation", UserID: user.String(), BusinessID: business.String()}})
	h, err := NewPostHandler(svc)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(h.ScheduleApprovedPost))
	defer server.Close()
	request := service.ApprovedPostSchedule{BusinessID: business.String(), ConversationID: "conversation", ApprovalID: batchID + "-call", Tool: tools.TelegramSendChannelPost, Args: args}
	var recordID string
	for range 2 {
		payload, err := json.Marshal(request)
		require.NoError(t, err)
		response, err := server.Client().Post(server.URL+"/internal/v1/posts/schedule", "application/json", bytes.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		var receipt map[string]interface{}
		require.NoError(t, json.NewDecoder(response.Body).Decode(&receipt))
		require.NoError(t, response.Body.Close())
		require.Equal(t, true, receipt["api_owned_schedule"])
		require.Equal(t, "scheduled", receipt["status"])
		if recordID != "" {
			require.Equal(t, recordID, receipt["post_record_id"])
		}
		recordID = receipt["post_record_id"].(string)
	}
	count, err := db.Collection("posts").CountDocuments(context.Background(), map[string]interface{}{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.Zero(t, dispatch.calls)
	post, err := posts.GetByID(context.Background(), recordID)
	require.NoError(t, err)
	require.Equal(t, "message", post.BroadcastGroupID)
	request.BusinessID = uuid.NewString()
	payload, err := json.Marshal(request)
	require.NoError(t, err)
	response, err := server.Client().Post(server.URL, "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	require.NoError(t, response.Body.Close())
}
