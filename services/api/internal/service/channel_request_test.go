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

// fakeChannelDemandRepo records the rows Insert was asked to persist and serves
// a canned summary, so the service's validation + tenant-scoping behavior is
// observable without a database.
type fakeChannelDemandRepo struct {
	inserted  []repository.ChannelDemandSignalRow
	summaryFn func(ctx context.Context, businessID uuid.UUID) ([]repository.ChannelDemandCount, error)
}

func (f *fakeChannelDemandRepo) Insert(_ context.Context, row repository.ChannelDemandSignalRow) error {
	f.inserted = append(f.inserted, row)
	return nil
}

func (f *fakeChannelDemandRepo) SummaryByBusiness(ctx context.Context, businessID uuid.UUID) ([]repository.ChannelDemandCount, error) {
	if f.summaryFn != nil {
		return f.summaryFn(ctx, businessID)
	}
	return nil, nil
}

// TestRecord_RejectsUnknownChannelBeforePersistence is the fail-on-revert guard
// for the core demand-capture guarantee: an unknown channel must be rejected
// with ErrUnknownChannel and MUST NOT reach the store. If the enum guard is
// reverted the unknown channel would be persisted and this test fails.
func TestRecord_RejectsUnknownChannelBeforePersistence(t *testing.T) {
	repo := &fakeChannelDemandRepo{}
	svc := NewChannelRequestService(repo)

	err := svc.Record(context.Background(), uuid.New(), ChannelRequestInput{Channel: "tiktok"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownChannel), "want ErrUnknownChannel, got %v", err)
	assert.Empty(t, repo.inserted, "unknown channel must never be persisted")
}

// TestRecord_ScopesWriteToBusinessID is the second half of the guarantee: the
// signal is attributed to exactly the caller-supplied businessID (never a
// client value) and the channel/note round-trip unchanged.
func TestRecord_ScopesWriteToBusinessID(t *testing.T) {
	repo := &fakeChannelDemandRepo{}
	svc := NewChannelRequestService(repo)

	bizID := uuid.New()
	err := svc.Record(context.Background(), bizID, ChannelRequestInput{Channel: "wildberries", Note: "хотим WB"})

	require.NoError(t, err)
	require.Len(t, repo.inserted, 1)
	assert.Equal(t, bizID, repo.inserted[0].BusinessID)
	assert.Equal(t, "wildberries", repo.inserted[0].Channel)
	require.NotNil(t, repo.inserted[0].Note)
	assert.Equal(t, "хотим WB", *repo.inserted[0].Note)
}

func TestRecord_EmptyNoteStoredAsNull(t *testing.T) {
	repo := &fakeChannelDemandRepo{}
	svc := NewChannelRequestService(repo)

	err := svc.Record(context.Background(), uuid.New(), ChannelRequestInput{Channel: "2gis"})

	require.NoError(t, err)
	require.Len(t, repo.inserted, 1)
	assert.Nil(t, repo.inserted[0].Note, "empty note must be persisted as SQL NULL")
}

func TestRecord_NilBusinessRejected(t *testing.T) {
	repo := &fakeChannelDemandRepo{}
	svc := NewChannelRequestService(repo)

	err := svc.Record(context.Background(), uuid.Nil, ChannelRequestInput{Channel: "avito"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingBusiness))
	assert.Empty(t, repo.inserted)
}

func TestRecord_AllAllowedChannelsAccepted(t *testing.T) {
	for _, ch := range []string{"avito", "wildberries", "ozon", "2gis", "other"} {
		repo := &fakeChannelDemandRepo{}
		svc := NewChannelRequestService(repo)
		err := svc.Record(context.Background(), uuid.New(), ChannelRequestInput{Channel: ch})
		require.NoErrorf(t, err, "channel %q should be accepted", ch)
		require.Len(t, repo.inserted, 1)
	}
}

func TestSummary_PassesBusinessIDThroughAndMaps(t *testing.T) {
	bizID := uuid.New()
	var gotID uuid.UUID
	repo := &fakeChannelDemandRepo{
		summaryFn: func(_ context.Context, id uuid.UUID) ([]repository.ChannelDemandCount, error) {
			gotID = id
			return []repository.ChannelDemandCount{{Channel: "avito", Count: 2}}, nil
		},
	}
	svc := NewChannelRequestService(repo)

	got, err := svc.Summary(context.Background(), bizID)

	require.NoError(t, err)
	assert.Equal(t, bizID, gotID, "summary must be scoped to the caller's business")
	assert.Equal(t, []ChannelDemandCount{{Channel: "avito", Count: 2}}, got)
}

func TestSummary_NilBusinessRejected(t *testing.T) {
	svc := NewChannelRequestService(&fakeChannelDemandRepo{})
	_, err := svc.Summary(context.Background(), uuid.Nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingBusiness))
}
