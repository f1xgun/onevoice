package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// fakeLandingRepo records the rows each insert was asked to persist so the
// service's normalization + validation is observable without a database.
type fakeLandingRepo struct {
	waitlist []repository.WaitlistSignupRow
	votes    []repository.ChannelVoteRow
}

func (f *fakeLandingRepo) InsertWaitlist(_ context.Context, row repository.WaitlistSignupRow) error {
	f.waitlist = append(f.waitlist, row)
	return nil
}

func (f *fakeLandingRepo) InsertChannelVote(_ context.Context, row repository.ChannelVoteRow) error {
	f.votes = append(f.votes, row)
	return nil
}

// TestJoinWaitlist_RejectsMissingConsentBeforePersistence is the fail-on-revert
// guard for the consent gate: a signup without consent must be rejected and
// MUST NOT reach the store.
func TestJoinWaitlist_RejectsMissingConsentBeforePersistence(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.JoinWaitlist(context.Background(), WaitlistInput{Email: "a@b.com", Consent: false})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConsentRequired))
	assert.Empty(t, repo.waitlist, "signup without consent must never be persisted")
}

func TestJoinWaitlist_NormalizesEmail(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.JoinWaitlist(context.Background(), WaitlistInput{Email: "  Owner@Cafe.RU ", Consent: true})

	require.NoError(t, err)
	require.Len(t, repo.waitlist, 1)
	assert.Equal(t, "owner@cafe.ru", repo.waitlist[0].Email, "email must be trimmed + lower-cased for case-insensitive dedup")
}

func TestJoinWaitlist_EmptyEmailRejected(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.JoinWaitlist(context.Background(), WaitlistInput{Email: "   ", Consent: true})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidEmail))
	assert.Empty(t, repo.waitlist)
}

func TestJoinWaitlist_OptionalSegmentsRoundTrip(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.JoinWaitlist(context.Background(), WaitlistInput{
		Email:   "x@y.com",
		Sphere:  "cafe",
		Pain:    "reviews",
		Consent: true,
	})

	require.NoError(t, err)
	require.Len(t, repo.waitlist, 1)
	require.NotNil(t, repo.waitlist[0].Sphere)
	assert.Equal(t, "cafe", *repo.waitlist[0].Sphere)
	require.NotNil(t, repo.waitlist[0].Pain)
	assert.Equal(t, "reviews", *repo.waitlist[0].Pain)
}

func TestJoinWaitlist_OmittedSegmentsStoredAsNull(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.JoinWaitlist(context.Background(), WaitlistInput{Email: "x@y.com", Consent: true})

	require.NoError(t, err)
	require.Len(t, repo.waitlist, 1)
	assert.Nil(t, repo.waitlist[0].Sphere)
	assert.Nil(t, repo.waitlist[0].Pain)
}

func TestJoinWaitlist_UnknownSphereRejected(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.JoinWaitlist(context.Background(), WaitlistInput{Email: "x@y.com", Sphere: "factory", Consent: true})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidSegment))
	assert.Empty(t, repo.waitlist)
}

func TestRecordChannelVote_RejectsUnknownChannel(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.RecordChannelVote(context.Background(), ChannelVoteInput{Channel: "instagram"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownVoteChannel), "want ErrUnknownVoteChannel, got %v", err)
	assert.Empty(t, repo.votes, "unknown channel must never be persisted")
}

func TestRecordChannelVote_AllAllowedChannelsAccepted(t *testing.T) {
	for _, ch := range []string{"whatsapp", "avito", "2gis", "other"} {
		repo := &fakeLandingRepo{}
		svc := NewLandingService(repo)
		err := svc.RecordChannelVote(context.Background(), ChannelVoteInput{Channel: ch})
		require.NoErrorf(t, err, "channel %q should be accepted", ch)
		require.Len(t, repo.votes, 1)
	}
}

func TestRecordChannelVote_EmptyNoteStoredAsNull(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.RecordChannelVote(context.Background(), ChannelVoteInput{Channel: "other", Note: "  "})

	require.NoError(t, err)
	require.Len(t, repo.votes, 1)
	assert.Nil(t, repo.votes[0].Note, "blank note must be persisted as SQL NULL")
}

func TestRecordChannelVote_NoteRoundTrips(t *testing.T) {
	repo := &fakeLandingRepo{}
	svc := NewLandingService(repo)

	err := svc.RecordChannelVote(context.Background(), ChannelVoteInput{Channel: "other", Note: "хотим Дзен"})

	require.NoError(t, err)
	require.Len(t, repo.votes, 1)
	require.NotNil(t, repo.votes[0].Note)
	assert.Equal(t, "хотим Дзен", *repo.votes[0].Note)
}
