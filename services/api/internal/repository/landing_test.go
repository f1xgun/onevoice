package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLanding_InsertWaitlist_DedupSuffix(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewLandingRepository(mock)

	sphere := "cafe"
	mock.ExpectExec(`INSERT INTO waitlist_signups .* ON CONFLICT \(email\) DO UPDATE SET source = COALESCE\(EXCLUDED.source, waitlist_signups.source\), plan = COALESCE\(EXCLUDED.plan, waitlist_signups.plan\)`).
		WithArgs("owner@cafe.ru", &sphere, (*string)(nil), true, (*string)(nil), (*string)(nil)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.InsertWaitlist(context.Background(), WaitlistSignupRow{
		Email:   "owner@cafe.ru",
		Sphere:  &sphere,
		Consent: true,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLanding_HasRegistrationAccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		email   string
		allowed bool
	}{
		{name: "granted", email: " Owner@Cafe.RU ", allowed: true},
		{name: "not granted", email: "other@example.org", allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			t.Cleanup(func() { mock.Close() })
			mock.ExpectQuery(`SELECT EXISTS .* FROM waitlist_signups .* email = \$1 .* consent = TRUE .* access_granted_at IS NOT NULL`).
				WithArgs(strings.ToLower(strings.TrimSpace(tc.email))).
				WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(tc.allowed))

			allowed, err := NewLandingRepository(mock).HasRegistrationAccess(context.Background(), tc.email)

			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLanding_HasRegistrationAccess_QueryFailure(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs("owner@example.org").WillReturnError(assert.AnError)

	allowed, err := NewLandingRepository(mock).HasRegistrationAccess(context.Background(), "owner@example.org")

	require.ErrorContains(t, err, "registration access lookup")
	require.False(t, allowed)
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

func TestLanding_InsertEvent(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	mock.ExpectExec(`INSERT INTO landing_events \(cta,path\) VALUES \(\$1,\$2\)`).WithArgs("nav-login", "/ru").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	require.NoError(t, NewLandingRepository(mock).InsertLandingEvent(context.Background(), LandingEventRow{CTA: "nav-login", Path: "/ru"}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLandingWaitlistAttributionUpsert(t *testing.T) {
	for _, source := range []string{"billing", "business-limit"} {
		t.Run(source, func(t *testing.T) {
			pool, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer pool.Close()
			plan := "pro"
			pool.ExpectExec(`INSERT INTO waitlist_signups \(email,sphere,pain,consent,source,plan\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\) ON CONFLICT \(email\) DO UPDATE SET source = COALESCE\(EXCLUDED.source, waitlist_signups.source\), plan = COALESCE\(EXCLUDED.plan, waitlist_signups.plan\)`).
				WithArgs("owner@example.org", (*string)(nil), (*string)(nil), true, &source, &plan).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
			require.NoError(t, NewLandingRepository(pool).InsertWaitlist(context.Background(), WaitlistSignupRow{Email: "owner@example.org", Consent: true, Source: &source, Plan: &plan}))
			require.NoError(t, pool.ExpectationsWereMet())
		})
	}
}
