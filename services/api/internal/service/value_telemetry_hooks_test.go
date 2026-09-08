package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/valuetelemetry"
)

type capturedValueEvents struct{ events []valuetelemetry.Event }

func (s *capturedValueEvents) RecordValue(_ context.Context, event valuetelemetry.Event) {
	s.events = append(s.events, event)
}

func TestUserValueTelemetry_OnlyAfterRegistrationCommit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		commitErr error
		wantErr   bool
		wantEvent bool
	}{
		{name: "committed", wantEvent: true},
		{name: "commit failed", commitErr: errors.New("commit failed"), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewUserService(&mockUserRepository{}, nil, "test-secret-must-be-32bytes-ok!!")
			require.NoError(t, err)
			pool := &fakeRegisterPool{commitErr: tt.commitErr}
			concrete := svc.(*userService)
			concrete.SetRegisterCollaborators(pool, &fakeRegisterUserExt{}, &fakeConsentInserter{}, &fakeVerifyIssuer{}, nil)
			sink := &capturedValueEvents{}
			WithUserValueTelemetry(svc, sink)

			user, gotErr := svc.Register(context.Background(), "value@example.com", "Zx9!mK7-qP2w")
			if tt.wantErr {
				require.Error(t, gotErr)
				require.Empty(t, sink.events)
				return
			}
			require.NoError(t, gotErr)
			require.Len(t, sink.events, 1)
			require.Equal(t, valuetelemetry.SignupCompleted, sink.events[0].Action)
			require.Equal(t, user.ID, sink.events[0].UserID)
			require.Equal(t, user.ID.String(), sink.events[0].SourceID)
		})
	}
}

func TestBusinessValueTelemetry_OnlyAfterBusinessMembershipCommit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		commitErr error
		wantEvent bool
	}{
		{name: "committed", wantEvent: true},
		{name: "commit failed", commitErr: errors.New("commit failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := pgxmock.NewPool()
			require.NoError(t, err)
			t.Cleanup(func() { pool.Close() })
			pool.ExpectBegin()
			commit := pool.ExpectCommit()
			if tt.commitErr != nil {
				commit.WillReturnError(tt.commitErr)
			}
			repo := &mockBusinessRepository{createFunc: func(_ context.Context, b *domain.Business) error {
				b.CreatedAt, b.UpdatedAt = time.Now(), time.Now()
				return nil
			}}
			sink := &capturedValueEvents{}
			svc := WithBusinessValueTelemetry(NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, pool, audit.Nop()), sink)
			ownerID, businessID := uuid.New(), uuid.New()
			_, gotErr := svc.Create(context.Background(), &domain.Business{ID: businessID, Name: "Value org"}, ownerID)
			if tt.commitErr != nil {
				require.Error(t, gotErr)
				require.Empty(t, sink.events)
			} else {
				require.NoError(t, gotErr)
				require.Equal(t, []valuetelemetry.Event{{Action: valuetelemetry.OrgCreated, SourceID: businessID.String(), UserID: ownerID, BusinessID: businessID}}, sink.events)
			}
			require.NoError(t, pool.ExpectationsWereMet())
		})
	}
}

func TestIntegrationValueTelemetry_OnlyAfterCreate(t *testing.T) {
	businessID, actorID := uuid.New(), uuid.New()
	for _, tt := range []struct {
		name      string
		createErr error
		wantEvent bool
	}{
		{name: "persisted", wantEvent: true},
		{name: "persistence failed", createErr: errors.New("insert failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sink := &capturedValueEvents{}
			repo := &mockIntegrationRepository{createFunc: func(_ context.Context, _ *domain.Integration) error { return tt.createErr }}
			svc := WithIntegrationValueTelemetry(NewIntegrationService(repo, testEnvelope(t, testEncryptor(t)), nil, nil, audit.Nop()), sink)
			integration, gotErr := svc.Connect(context.Background(), ConnectParams{BusinessID: businessID, ActorID: actorID, Platform: "telegram", AccessToken: "token"})
			if tt.createErr != nil {
				require.Error(t, gotErr)
				require.Empty(t, sink.events)
				return
			}
			require.NoError(t, gotErr)
			require.Equal(t, []valuetelemetry.Event{{Action: valuetelemetry.IntegrationConnected, SourceID: integration.ID.String(), UserID: actorID, BusinessID: businessID, Platform: "telegram", Kind: "integration"}}, sink.events)
		})
	}
}

func TestReviewValueTelemetry_RequiresDispatchAndBothPersistenceWrites(t *testing.T) {
	businessID := uuid.New()
	for _, tt := range []struct {
		name          string
		requester     natsRequester
		dispatchedErr error
		updateErr     error
		wantEvent     bool
	}{
		{name: "success", requester: &capturingRequester{}, wantEvent: true},
		{name: "dispatch failed", requester: &failingRequester{}},
		{name: "dispatch persistence failed", requester: &capturingRequester{}, dispatchedErr: errors.New("mongo failed")},
		{name: "final persistence failed", requester: &capturingRequester{}, updateErr: errors.New("mongo failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			review := failedTelegramReview(businessID)
			review.ReplyStatus, review.ReplyText = domain.ReviewReplyStatusPending, ""
			repo := &stubReviewRepo{review: review, dispatchedReplyErr: tt.dispatchedErr, updateReplyErr: tt.updateErr}
			sink := &capturedValueEvents{}
			svc := &reviewService{repo: repo, nc: tt.requester, dispatchTimeout: time.Second, valueTelemetry: sink}
			gotErr := svc.Reply(context.Background(), businessID, review.ID, "reply")
			if tt.wantEvent {
				require.NoError(t, gotErr)
				require.Equal(t, []valuetelemetry.Event{{Action: valuetelemetry.ReviewReplied, SourceID: businessID.String() + ":" + review.ID, BusinessID: businessID, Platform: review.Platform, Kind: "review_reply"}}, sink.events)
			} else {
				require.Error(t, gotErr)
				require.Empty(t, sink.events)
			}
		})
	}
}

func TestReviewValueTelemetry_ReplyAndRetryUseStableIdentity(t *testing.T) {
	businessID := uuid.New()
	review := failedTelegramReview(businessID)
	review.ReplyStatus, review.ReplyText = domain.ReviewReplyStatusPending, ""
	sink := &capturedValueEvents{}
	svc := &reviewService{repo: &stubReviewRepo{review: review}, nc: &capturingRequester{}, dispatchTimeout: time.Second, valueTelemetry: sink}
	require.NoError(t, svc.Reply(context.Background(), businessID, review.ID, "reply"))
	review.ReplyStatus, review.ReplyText = domain.ReviewReplyStatusError, "reply"
	require.NoError(t, svc.RetryReply(context.Background(), businessID, review.ID))
	require.Len(t, sink.events, 2)
	require.Equal(t, sink.events[0].SourceID, sink.events[1].SourceID)
}
