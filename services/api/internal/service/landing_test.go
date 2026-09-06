package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// fakeLandingRepo records the rows each insert was asked to persist so the
// service's normalization + validation is observable without a database.
type fakeLandingRepo struct {
	waitlist []repository.WaitlistSignupRow
	votes    []repository.ChannelVoteRow
	events   []repository.LandingEventRow
	eventErr error
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

func (f *fakeLandingRepo) InsertLandingEvent(_ context.Context, row repository.LandingEventRow) error {
	if f.eventErr != nil {
		return f.eventErr
	}
	f.events = append(f.events, row)
	return nil
}

func TestLandingEventValidationAndCounter(t *testing.T) {
	for _, cta := range []string{"hero-waitlist", "hero-register", "nav-register", "nav-login", "pricing-free-register", "pricing-pro-waitlist", "waitlist-success-register"} {
		t.Run(cta, func(t *testing.T) {
			repo := &fakeLandingRepo{}
			svc := NewLandingService(repo)
			counter := landingCTAClicks.WithLabelValues(cta)
			before := testutil.ToFloat64(counter)
			require.NoError(t, svc.RecordLandingEvent(context.Background(), LandingEventInput{CTA: cta, Path: "/ru"}))
			require.Equal(t, []repository.LandingEventRow{{CTA: cta, Path: "/ru"}}, repo.events)
			assert.Equal(t, before+1, testutil.ToFloat64(counter))
			repo.eventErr = errors.New("store unavailable")
			require.Error(t, svc.RecordLandingEvent(context.Background(), LandingEventInput{CTA: cta, Path: "/en"}))
			assert.Equal(t, before+1, testutil.ToFloat64(counter))
		})
	}
	for _, in := range []LandingEventInput{
		{CTA: "unknown", Path: "/"}, {CTA: "nav-login", Path: ""},
		{CTA: "nav-login", Path: "/" + strings.Repeat("x", 1024)},
		{CTA: "nav-login", Path: "//example.org"}, {CTA: "nav-login", Path: "/?email=private"},
		{CTA: "nav-login", Path: "/#fragment"}, {CTA: "nav-login", Path: "/\n"},
		{CTA: "nav-login", Path: "/\\host"},
	} {
		t.Run(in.CTA+in.Path, func(t *testing.T) {
			repo := &fakeLandingRepo{}
			require.ErrorIs(t, NewLandingService(repo).RecordLandingEvent(context.Background(), in), ErrInvalidLandingEvent)
			assert.Empty(t, repo.events)
		})
	}
}

func TestWaitlistAttribution(t *testing.T) {
	for _, tc := range []struct {
		source, plan string
		valid        bool
	}{
		{"", "", true}, {"landing", "", true}, {"billing", "pro", true}, {"business-limit", "pro", true},
		{"unknown", "pro", false}, {"billing", "free", false},
	} {
		t.Run(tc.source+tc.plan, func(t *testing.T) {
			repo := &fakeLandingRepo{}
			err := NewLandingService(repo).JoinWaitlist(context.Background(), WaitlistInput{Email: "owner@example.org", Consent: true, Source: tc.source, Plan: tc.plan})
			if !tc.valid {
				require.ErrorIs(t, err, ErrInvalidSegment)
				require.Empty(t, repo.waitlist)
				return
			}
			require.NoError(t, err)
			require.Len(t, repo.waitlist, 1)
			if tc.source != "" {
				assert.Equal(t, tc.source, *repo.waitlist[0].Source)
			} else {
				assert.Nil(t, repo.waitlist[0].Source)
			}
			if tc.plan != "" {
				assert.Equal(t, tc.plan, *repo.waitlist[0].Plan)
			} else {
				assert.Nil(t, repo.waitlist[0].Plan)
			}
		})
	}
}
