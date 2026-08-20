// Package service — landing.go.
//
// LandingService backs the two public marketing-landing capture endpoints:
// the closed-beta waitlist and the fake-door channel vote. Both are
// unauthenticated, so the service is the trust boundary — it normalizes and
// re-validates every field against a closed allow-list before persistence,
// independently of the handler's request-schema validation.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// ErrConsentRequired is returned when a waitlist signup arrives without the
// personal-data-processing consent flag set to true.
var ErrConsentRequired = errors.New("consent required")

// ErrInvalidEmail is returned when a waitlist signup carries an empty email
// after normalization.
var ErrInvalidEmail = errors.New("invalid email")

// ErrInvalidSegment is returned when an optional waitlist segmentation field
// (sphere or pain) is outside its closed allow-list.
var ErrInvalidSegment = errors.New("invalid segment")

// ErrUnknownVoteChannel is returned when a fake-door vote names a channel
// outside the closed allow-list.
var ErrUnknownVoteChannel = errors.New("unknown vote channel")

// allowedSpheres is the closed set of organization-sphere segments. Mirrors the
// WaitlistRequest.sphere enum and the waitlist_signups.sphere CHECK.
var allowedSpheres = map[string]struct{}{
	"cafe":     {},
	"beauty":   {},
	"services": {},
	"retail":   {},
	"other":    {},
}

// allowedPains is the closed set of strongest-pain segments. Mirrors the
// WaitlistRequest.pain enum and the waitlist_signups.pain CHECK.
var allowedPains = map[string]struct{}{
	"reviews": {},
	"posts":   {},
	"card":    {},
}

// allowedVoteChannels is the closed set of fake-door vote channels. Mirrors the
// PublicChannelVoteRequest.channel enum and the channel_votes.channel CHECK.
// Instagram is deliberately absent (Meta is blocked in RU) — latent demand is
// captured only through "other".
var allowedVoteChannels = map[string]struct{}{
	"whatsapp": {},
	"avito":    {},
	"2gis":     {},
	"other":    {},
}

// WaitlistInput is the service-layer view of one closed-beta signup. Sphere and
// Pain are empty when the visitor left them unset.
type WaitlistInput struct {
	Email   string
	Sphere  string
	Pain    string
	Consent bool
}

// ChannelVoteInput is the service-layer view of one fake-door vote.
type ChannelVoteInput struct {
	Channel string
	Note    string
}

// landingRepo is the narrow persistence surface LandingService depends on.
type landingRepo interface {
	InsertWaitlist(ctx context.Context, row repository.WaitlistSignupRow) error
	InsertChannelVote(ctx context.Context, row repository.ChannelVoteRow) error
}

// LandingService records public waitlist signups and fake-door channel votes.
type LandingService struct {
	repo landingRepo
}

// NewLandingService constructs a LandingService.
func NewLandingService(repo landingRepo) *LandingService {
	return &LandingService{repo: repo}
}

// JoinWaitlist normalizes the email (trim + lower-case), enforces consent, and
// validates the optional segments against their closed allow-lists before
// persisting. A duplicate email is a silent no-op at the repository layer, so
// this returns nil for a repeat signup — the caller must not surface a
// distinguishable "already exists" response.
func (s *LandingService) JoinWaitlist(ctx context.Context, in WaitlistInput) error {
	if !in.Consent {
		return ErrConsentRequired
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		return ErrInvalidEmail
	}

	row := repository.WaitlistSignupRow{Email: email, Consent: in.Consent}
	if in.Sphere != "" {
		if _, ok := allowedSpheres[in.Sphere]; !ok {
			return fmt.Errorf("%w: sphere %q", ErrInvalidSegment, in.Sphere)
		}
		sphere := in.Sphere
		row.Sphere = &sphere
	}
	if in.Pain != "" {
		if _, ok := allowedPains[in.Pain]; !ok {
			return fmt.Errorf("%w: pain %q", ErrInvalidSegment, in.Pain)
		}
		pain := in.Pain
		row.Pain = &pain
	}

	if err := s.repo.InsertWaitlist(ctx, row); err != nil {
		return fmt.Errorf("waitlist join: %w", err)
	}
	return nil
}

// RecordChannelVote validates the channel against the closed allow-list and
// persists one vote. An empty note is stored as SQL NULL.
func (s *LandingService) RecordChannelVote(ctx context.Context, in ChannelVoteInput) error {
	if _, ok := allowedVoteChannels[in.Channel]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownVoteChannel, in.Channel)
	}
	row := repository.ChannelVoteRow{Channel: in.Channel}
	if note := strings.TrimSpace(in.Note); note != "" {
		row.Note = &note
	}
	if err := s.repo.InsertChannelVote(ctx, row); err != nil {
		return fmt.Errorf("channel vote record: %w", err)
	}
	return nil
}
