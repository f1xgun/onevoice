// Package repository — landing.go.
//
// LandingRepository stores anonymous waitlist signups, channel votes, and CTA
// events. These records carry no tenant key.
package repository

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
)

// WaitlistSignupRow is one closed-beta signup to insert. Email is stored
// pre-normalized (lower-cased) by the service so the UNIQUE dedup is
// case-insensitive. Sphere and Pain are nullable segmentation fields.
type WaitlistSignupRow struct {
	Email   string
	Sphere  *string
	Pain    *string
	Consent bool
	Source  *string
	Plan    *string
}

// ChannelVoteRow is one public fake-door vote to insert. Note is nullable.
type ChannelVoteRow struct {
	Channel string
	Note    *string
}

// LandingRepository owns SQL for waitlist_signups and channel_votes.
type LandingRepository struct {
	pool pgxPool
	psql sq.StatementBuilderType
}

// NewLandingRepository constructs the landing-capture repo.
func NewLandingRepository(pool pgxPool) *LandingRepository {
	return &LandingRepository{
		pool: pool,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// InsertWaitlist records one signup. A duplicate
// email updates supplied attribution without erasing omitted fields, so the
// handler can return the same 204 whether the email is new or already known —
// no email-enumeration oracle.
func (r *LandingRepository) InsertWaitlist(ctx context.Context, row WaitlistSignupRow) error {
	sqlStr, args, err := r.psql.
		Insert("waitlist_signups").
		Columns("email", "sphere", "pain", "consent", "source", "plan").
		Values(row.Email, row.Sphere, row.Pain, row.Consent, row.Source, row.Plan).
		Suffix("ON CONFLICT (email) DO UPDATE SET source = COALESCE(EXCLUDED.source, waitlist_signups.source), plan = COALESCE(EXCLUDED.plan, waitlist_signups.plan)").
		ToSql()
	if err != nil {
		return fmt.Errorf("waitlist_signups insert build: %w", err)
	}
	if _, err := r.pool.Exec(ctx, sqlStr, args...); err != nil {
		return fmt.Errorf("waitlist_signups insert: %w", err)
	}
	return nil
}

// InsertChannelVote records one fake-door vote. A nil Note is stored as SQL
// NULL. The DB-level CHECK on channel is defense in depth behind the app-layer
// enum validation.
func (r *LandingRepository) InsertChannelVote(ctx context.Context, row ChannelVoteRow) error {
	sqlStr, args, err := r.psql.
		Insert("channel_votes").
		Columns("channel", "note").
		Values(row.Channel, row.Note).
		ToSql()
	if err != nil {
		return fmt.Errorf("channel_votes insert build: %w", err)
	}
	if _, err := r.pool.Exec(ctx, sqlStr, args...); err != nil {
		return fmt.Errorf("channel_votes insert: %w", err)
	}
	return nil
}

// LandingEventRow is an anonymous CTA click.
type LandingEventRow struct {
	CTA  string
	Path string
}

// InsertLandingEvent persists one validated click.
func (r *LandingRepository) InsertLandingEvent(ctx context.Context, row LandingEventRow) error {
	query, args, err := r.psql.Insert("landing_events").Columns("cta", "path").Values(row.CTA, row.Path).ToSql()
	if err != nil {
		return fmt.Errorf("landing event build: %w", err)
	}
	if _, err := r.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("landing event insert: %w", err)
	}
	return nil
}
