package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type approvalRows struct {
	rows chan repository.TelemetryEventRow
}

func (r *approvalRows) InsertBatch(_ context.Context, rows []repository.TelemetryEventRow) error {
	for _, row := range rows {
		r.rows <- row
	}
	return nil
}

func TestHITLTelemetry_RecordsCommittedPostEditAndReviewRejection(t *testing.T) {
	bizID := uuid.New().String()
	pending := newStubPendingRepo()
	seedBatch(pending, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "PRIVATE POST"}},
		{CallID: "tc_b", ToolName: tools.YandexBusinessReplyReview, Arguments: map[string]interface{}{"text": "PRIVATE REVIEW"}},
	})
	svc := newSvc(t, pending, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})
	rows := &approvalRows{rows: make(chan repository.TelemetryEventRow, 4)}
	svc.SetApprovalTelemetry(service.NewTelemetryService(rows))
	input := service.ResolveInput{ConversationID: "conv-1", BatchID: "batch-1", ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "edit", EditedArgs: map[string]interface{}{"text": "PRIVATE EDIT"}},
			{ID: "tc_b", Action: "reject", RejectReason: "private@example.com"},
		},
	}
	_, err := svc.Resolve(context.Background(), input)
	require.NoError(t, err)
	got := map[string]map[string]string{}
	for range 2 {
		select {
		case row := <-rows.rows:
			require.Equal(t, "decision_recorded", row.Action)
			var metadata map[string]string
			require.NoError(t, json.Unmarshal(row.Metadata, &metadata))
			got[metadata["kind"]] = metadata
			require.NotContains(t, string(row.Metadata), "PRIVATE")
			require.NotContains(t, string(row.Metadata), "private@")
		case <-time.After(time.Second):
			t.Fatal("missing committed decision telemetry")
		}
	}
	require.Equal(t, "8f5815465303ca0a3b700c07068ef3ef0bf6f4d8a6cd60bf7220cd3d33e7c77e", got["post"]["batch_id"])
	require.Equal(t, "edit", got["post"]["outcome"])
	require.Equal(t, "e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d", got["post"]["draft_id"])
	require.Equal(t, "reject", got["review_reply"]["outcome"])
	_, err = svc.Resolve(context.Background(), input)
	require.Error(t, err)
	select {
	case <-rows.rows:
		t.Fatal("a refused duplicate emitted a decision")
	case <-time.After(20 * time.Millisecond):
	}
}
