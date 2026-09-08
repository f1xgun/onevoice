package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

type fakeTelemetryRepo struct {
	calls int
	rows  []repository.TelemetryEventRow
	err   error
}

func (f *fakeTelemetryRepo) InsertBatch(_ context.Context, rows []repository.TelemetryEventRow) error {
	f.calls++
	f.rows = rows
	return f.err
}

func TestTelemetryService_Ingest_StampsUserAndMaps(t *testing.T) {
	repo := &fakeTelemetryRepo{}
	svc := NewTelemetryService(repo)
	uid := uuid.New()

	err := svc.Ingest(context.Background(), uid, []TelemetryEvent{
		{EventType: "page_view", Action: "load", Page: "/x", CorrelationID: "c1", ClientTS: "2026-01-01T00:00:00Z"},
		{EventType: "click", Action: "save", Page: "/y", Metadata: map[string]string{"k": "v"}},
	})
	require.NoError(t, err)
	require.Len(t, repo.rows, 2)

	require.NotNil(t, repo.rows[0].UserID)
	assert.Equal(t, uid, *repo.rows[0].UserID)
	assert.Equal(t, "page_view", repo.rows[0].EventType)
	require.NotNil(t, repo.rows[0].CorrelationID)
	assert.Equal(t, "c1", *repo.rows[0].CorrelationID)
	require.NotNil(t, repo.rows[0].ClientTS)
	assert.Nil(t, repo.rows[0].Metadata)

	assert.JSONEq(t, `{"k":"v"}`, string(repo.rows[1].Metadata))
}

func TestTelemetryService_Ingest_NilUserStoredAsNull(t *testing.T) {
	repo := &fakeTelemetryRepo{}
	svc := NewTelemetryService(repo)

	require.NoError(t, svc.Ingest(context.Background(), uuid.Nil, []TelemetryEvent{{EventType: "x", Action: "y"}}))
	require.Len(t, repo.rows, 1)
	assert.Nil(t, repo.rows[0].UserID)
}

func TestTelemetryService_Ingest_EmptyNoCall(t *testing.T) {
	repo := &fakeTelemetryRepo{}
	svc := NewTelemetryService(repo)

	require.NoError(t, svc.Ingest(context.Background(), uuid.New(), nil))
	assert.Zero(t, repo.calls)
}

func TestTelemetryService_Ingest_RepoError(t *testing.T) {
	repo := &fakeTelemetryRepo{err: errors.New("boom")}
	svc := NewTelemetryService(repo)

	err := svc.Ingest(context.Background(), uuid.New(), []TelemetryEvent{{EventType: "x", Action: "y"}})
	require.Error(t, err)
}

func TestTelemetryService_ApprovalPrivacy(t *testing.T) {
	repo := &fakeTelemetryRepo{}
	svc := NewTelemetryService(repo)
	err := svc.Ingest(context.Background(), uuid.New(), []TelemetryEvent{
		{EventType: "approval", Action: "draft_shown", Page: "/reviews/private@example.com", CorrelationID: "private@example.com", ClientTS: "PRIVATE", Metadata: map[string]string{
			"draft_id": strings.Repeat("a", 64), "kind": "review_reply", "source": "reviews", "text": "PRIVATE", "args": "SECRET", "author": "private@example.com", "approval_id": strings.Repeat("b", 64),
		}},
		{EventType: "approval", Action: "send_result", Metadata: map[string]string{"draft_id": strings.Repeat("a", 64)}},
	})
	require.NoError(t, err)
	require.Len(t, repo.rows, 1)
	row := repo.rows[0]
	assert.Nil(t, row.UserID)
	assert.Nil(t, row.ClientTS)
	assert.Equal(t, "/reviews", row.Page)
	require.NotNil(t, row.CorrelationID)
	assert.Equal(t, strings.Repeat("a", 64), *row.CorrelationID)
	assert.JSONEq(t, `{"draft_id":"`+strings.Repeat("a", 64)+`","kind":"review_reply","source":"reviews"}`, string(row.Metadata))
}

func TestTelemetryService_ServerApprovalCorrelation(t *testing.T) {
	repo := &channelTelemetryRepo{rows: make(chan []repository.TelemetryEventRow, 1)}
	svc := NewTelemetryService(repo)
	svc.RecordApproval(context.Background(), approvaltelemetry.Event{
		DraftID: "batch-1-tc_a", ApprovalID: "batch-1-tc_a", Kind: "post", Source: "chat", Action: "decision_recorded", Outcome: "edit",
	})
	var rows []repository.TelemetryEventRow
	select {
	case rows = <-repo.rows:
	case <-time.After(time.Second):
		t.Fatal("missing server event")
	}
	require.Len(t, rows, 1)
	row := rows[0]
	assert.Equal(t, "approval_server", row.EventType)
	assert.Equal(t, "decision_recorded", row.Action)
	assert.Nil(t, row.UserID)
	assert.Nil(t, row.BusinessID)
	assert.NotContains(t, string(row.Metadata), "batch-1")
	assert.JSONEq(t, `{"draft_id":"e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d","approval_id":"e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d","kind":"post","source":"chat","outcome":"edit"}`, string(row.Metadata))
}

func TestApprovalTelemetryRow_ServerEditSavedHasNoOutcome(t *testing.T) {
	row, ok := approvalTelemetryRow(approvaltelemetry.Event{
		DraftID: "review-reply-review", ApprovalID: "review-reply-review",
		Kind: "review_reply", Source: "reviews", Action: "edit_saved",
	})
	require.True(t, ok)
	require.NotContains(t, string(row.Metadata), "outcome")

	_, ok = approvalTelemetryRow(approvaltelemetry.Event{
		DraftID: "review-reply-review", ApprovalID: "review-reply-review",
		Kind: "review_reply", Source: "reviews", Action: "edit_saved", Outcome: "edit",
	})
	require.False(t, ok)
}

type channelTelemetryRepo struct {
	rows    chan []repository.TelemetryEventRow
	release chan struct{}
}

func (r *channelTelemetryRepo) InsertBatch(ctx context.Context, rows []repository.TelemetryEventRow) error {
	r.rows <- rows
	if r.release == nil {
		return nil
	}
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestTelemetryService_SlowApprovalStoreDoesNotBlockOrSpawnUnboundedWrites(t *testing.T) {
	repo := &channelTelemetryRepo{rows: make(chan []repository.TelemetryEventRow, 100), release: make(chan struct{})}
	defer close(repo.release)
	svc := NewTelemetryService(repo)
	done := make(chan struct{})
	go func() {
		for range 100 {
			svc.RecordApproval(context.Background(), approvaltelemetry.Event{DraftID: "draft", ApprovalID: "draft", Kind: "post", Source: "chat", Action: "send_result", Outcome: "success"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("telemetry held the caller while its store was blocked")
	}
	require.Eventually(t, func() bool { return len(repo.rows) == 8 }, time.Second, time.Millisecond)
}
