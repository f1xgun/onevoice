package repository

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestLanding_InsertWaitlist_DedupSuffix(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewLandingRepository(mock)

	sphere := "cafe"
	mock.ExpectExec(`INSERT INTO waitlist_signups .* ON CONFLICT \(email\) DO NOTHING`).
		WithArgs("owner@cafe.ru", &sphere, (*string)(nil), true).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.InsertWaitlist(context.Background(), WaitlistSignupRow{
		Email:   "owner@cafe.ru",
		Sphere:  &sphere,
		Consent: true,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLanding_InsertChannelVote_WithNote(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewLandingRepository(mock)

	note := "хотим Дзен"
	mock.ExpectExec(`INSERT INTO channel_votes`).
		WithArgs("other", &note).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.InsertChannelVote(context.Background(), ChannelVoteRow{Channel: "other", Note: &note})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLanding_InsertChannelVote_NullNote(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewLandingRepository(mock)

	mock.ExpectExec(`INSERT INTO channel_votes`).
		WithArgs("whatsapp", (*string)(nil)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.InsertChannelVote(context.Background(), ChannelVoteRow{Channel: "whatsapp"})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
