package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

type capturedApprovalEvents struct {
	events []approvaltelemetry.Event
}

func (s *capturedApprovalEvents) RecordApproval(_ context.Context, events ...approvaltelemetry.Event) {
	s.events = append(s.events, events...)
}

func TestReviewTelemetry_RetryCorrelatesResultWithoutInventingAnotherDecision(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.DispatchApprovalID = "batch-1-tc_a"
	review.AuthorName = "private@example.com"
	repo := &stubReviewRepo{review: review}
	rows := &channelTelemetryRepo{rows: make(chan []repository.TelemetryEventRow, 4)}
	svc := &reviewService{repo: repo, nc: &capturingRequester{}, dispatchTimeout: time.Second, approvalTelemetry: NewTelemetryService(rows)}
	require.NoError(t, svc.RetryReply(context.Background(), biz, review.ID))
	var batch []repository.TelemetryEventRow
	select {
	case batch = <-rows.rows:
	case <-time.After(time.Second):
		t.Fatal("missing review result event")
	}
	require.Len(t, batch, 1)
	require.Equal(t, "send_result", batch[0].Action)
	var metadata map[string]string
	require.NoError(t, json.Unmarshal(batch[0].Metadata, &metadata))
	require.Equal(t, "success", metadata["outcome"])
	require.Equal(t, "review_reply", metadata["kind"])
	require.Equal(t, "23fb79b48147b1a1cefe8c99c56039c3c8365ef7588a15ac7db23a54e3f5cd62", metadata["draft_id"])
	require.Equal(t, "e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d", metadata["approval_id"])
	require.Nil(t, batch[0].UserID)
	require.NotContains(t, string(batch[0].Metadata), "private@")
	require.NotContains(t, string(batch[0].Metadata), review.ReplyText)
	select {
	case extra := <-rows.rows:
		t.Fatalf("retry emitted an extra approval event: %+v", extra)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestReviewTelemetry_DirectReplyRecordsDecisionAndConfirmedDispatchResult(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.ReplyStatus = domain.ReviewReplyStatusPending
	review.ReplyText = ""
	review.DraftReply = "ORIGINAL PRIVATE DRAFT"
	review.DraftStatus = domain.ReviewDraftStatusReady
	sink := &capturedApprovalEvents{}
	svc := &reviewService{
		repo:              &stubReviewRepo{review: review},
		nc:                &capturingRequester{},
		dispatchTimeout:   time.Second,
		approvalTelemetry: sink,
	}

	require.NoError(t, svc.Reply(context.Background(), biz, review.ID, "EDITED PRIVATE DRAFT"))
	require.Equal(t, []approvaltelemetry.Event{
		{
			DraftID: "review-reply-rev-retry", ApprovalID: "review-reply-rev-retry",
			Kind: "review_reply", Source: "reviews", Action: "decision_recorded", Outcome: "edit",
		},
		{
			DraftID: "review-reply-rev-retry", ApprovalID: "review-reply-rev-retry",
			Kind: "review_reply", Source: "reviews", Action: "send_result", Outcome: "success",
		},
		{
			DraftID: "review-reply-rev-retry", ApprovalID: "review-reply-rev-retry",
			Kind: "review_reply", Source: "reviews", Action: "edit_saved",
		},
	}, sink.events)
}

func TestReviewTelemetry_EditSavedRequiresPersistedNonEmptyChangedDraft(t *testing.T) {
	biz := uuid.New()
	tests := []struct {
		name            string
		draft           string
		draftStatus     string
		final           string
		updateErr       error
		dispatchedErr   error
		requester       natsRequester
		decisionOutcome string
		wantEditSaved   int
		wantErr         bool
	}{
		{name: "no draft", final: "MANUAL PRIVATE REPLY", requester: &capturingRequester{}, decisionOutcome: "approve"},
		{name: "whitespace draft", draft: "   ", draftStatus: domain.ReviewDraftStatusReady, final: "MANUAL PRIVATE REPLY", requester: &capturingRequester{}, decisionOutcome: "approve"},
		{name: "draft not ready", draft: "PRIVATE DRAFT", final: "EDITED PRIVATE DRAFT", requester: &capturingRequester{}, decisionOutcome: "approve"},
		{name: "unchanged", draft: "PRIVATE DRAFT", draftStatus: domain.ReviewDraftStatusReady, final: "PRIVATE DRAFT", requester: &capturingRequester{}, decisionOutcome: "approve"},
		{name: "successful dispatch first write persists despite second failure", draft: "PRIVATE DRAFT", draftStatus: domain.ReviewDraftStatusReady, final: "EDITED PRIVATE DRAFT", requester: &capturingRequester{}, updateErr: errors.New("mongo unavailable"), decisionOutcome: "edit", wantEditSaved: 1, wantErr: true},
		{name: "successful dispatch first write failure", draft: "PRIVATE DRAFT", draftStatus: domain.ReviewDraftStatusReady, final: "EDITED PRIVATE DRAFT", requester: &capturingRequester{}, dispatchedErr: errors.New("mongo unavailable"), decisionOutcome: "edit", wantErr: true},
		{name: "dispatch error and update failure", draft: "PRIVATE DRAFT", draftStatus: domain.ReviewDraftStatusReady, final: "EDITED PRIVATE DRAFT", requester: &failingRequester{}, updateErr: errors.New("mongo unavailable"), decisionOutcome: "edit", wantErr: true},
		{name: "dispatch error and update success", draft: "PRIVATE DRAFT", draftStatus: domain.ReviewDraftStatusReady, final: "EDITED PRIVATE DRAFT", requester: &failingRequester{}, decisionOutcome: "edit", wantEditSaved: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			review := failedTelegramReview(biz)
			review.ReplyStatus = domain.ReviewReplyStatusPending
			review.ReplyText = ""
			review.DraftReply = tt.draft
			review.DraftStatus = tt.draftStatus
			sink := &capturedApprovalEvents{}
			svc := &reviewService{
				repo: &stubReviewRepo{review: review, updateReplyErr: tt.updateErr, dispatchedReplyErr: tt.dispatchedErr}, nc: tt.requester,
				dispatchTimeout: time.Second, approvalTelemetry: sink,
			}

			err := svc.Reply(context.Background(), biz, review.ID, tt.final)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, "decision_recorded", sink.events[0].Action)
			require.Equal(t, tt.decisionOutcome, sink.events[0].Outcome)
			editSaved := 0
			for _, event := range sink.events {
				if event.Action == "edit_saved" {
					editSaved++
				}
			}
			require.Equal(t, tt.wantEditSaved, editSaved)
		})
	}
}

func TestReviewTelemetry_DispatchFailureIsAnErrorResult(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.ReplyStatus = domain.ReviewReplyStatusPending
	review.ReplyText = ""
	sink := &capturedApprovalEvents{}
	svc := &reviewService{
		repo:              &stubReviewRepo{review: review},
		nc:                &failingRequester{},
		dispatchTimeout:   time.Second,
		approvalTelemetry: sink,
	}

	require.Error(t, svc.Reply(context.Background(), biz, review.ID, "PRIVATE"))
	require.Len(t, sink.events, 2)
	require.Equal(t, "decision_recorded", sink.events[0].Action)
	require.Equal(t, "send_result", sink.events[1].Action)
	require.Equal(t, "error", sink.events[1].Outcome)
}

func TestReviewTelemetry_AutoPublishEmitsNoApprovalEvents(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.ReplyStatus = domain.ReviewReplyStatusPending
	review.ReplyText = ""
	sink := &capturedApprovalEvents{}
	svc := &reviewService{
		repo:              &stubReviewRepo{review: review},
		nc:                &capturingRequester{},
		dispatchTimeout:   time.Second,
		approvalTelemetry: sink,
	}

	require.NoError(t, svc.Reply(withAutoPublishSignal(context.Background()), biz, review.ID, "PRIVATE"))
	require.Empty(t, sink.events)
}

func TestReviewTelemetry_RejectedBeforeDispatchEmitsNothing(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.ReplyStatus = domain.ReviewReplyStatusReplied
	sink := &capturedApprovalEvents{}
	svc := &reviewService{
		repo:              &stubReviewRepo{review: review},
		nc:                &capturingRequester{},
		dispatchTimeout:   time.Second,
		approvalTelemetry: sink,
	}

	require.ErrorIs(t, svc.Reply(context.Background(), biz, review.ID, "PRIVATE"), domain.ErrReviewAlreadyAnswered)
	require.Empty(t, sink.events)
}
