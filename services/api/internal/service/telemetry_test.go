package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
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
