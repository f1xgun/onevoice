// Package service — channel_request.go.
//
// ChannelRequestService records demand for not-yet-supported channels (the
// fake-door behind the "request this channel" affordance) and reports the
// per-channel aggregate for a business. It validates the channel against the
// closed allow-list and scopes every write and read to the business, so an
// unknown channel never reaches the store and a signal is always attributed to
// the caller's tenant.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// ErrUnknownChannel is returned when Record is asked to persist a channel
// outside the closed allow-list. It is the service-layer guard that keeps the
// demand store clean even if a caller bypasses the handler's enum validation.
var ErrUnknownChannel = errors.New("unknown channel")

// ErrMissingBusiness is returned when Record is called with a nil business ID —
// a demand signal is meaningless without the business it belongs to.
var ErrMissingBusiness = errors.New("missing business id")

// allowedChannels is the closed set of channels a business may express demand
// for. It mirrors the CreateChannelRequestRequest enum in the OpenAPI spec and
// the CHECK constraint on channel_demand_signals.
var allowedChannels = map[string]struct{}{
	"avito":       {},
	"wildberries": {},
	"ozon":        {},
	"2gis":        {},
	"other":       {},
}

// ChannelRequestInput is the service-layer view of one demand signal.
type ChannelRequestInput struct {
	Channel string
	Note    string
}

// ChannelDemandCount is one per-channel aggregate returned by Summary.
type ChannelDemandCount struct {
	Channel string
	Count   int
}

// channelDemandRepo is the narrow persistence surface ChannelRequestService
// depends on.
type channelDemandRepo interface {
	Insert(ctx context.Context, row repository.ChannelDemandSignalRow) error
	SummaryByBusiness(ctx context.Context, businessID uuid.UUID) ([]repository.ChannelDemandCount, error)
}

// ChannelRequestService records and aggregates channel-demand signals.
type ChannelRequestService struct {
	repo channelDemandRepo
}

// NewChannelRequestService constructs a ChannelRequestService.
func NewChannelRequestService(repo channelDemandRepo) *ChannelRequestService {
	return &ChannelRequestService{repo: repo}
}

// Record validates the channel against the closed allow-list and persists one
// demand signal scoped to businessID. An unknown channel is rejected with
// ErrUnknownChannel before any write; a nil businessID is rejected with
// ErrMissingBusiness. An empty note is stored as SQL NULL.
func (s *ChannelRequestService) Record(ctx context.Context, businessID uuid.UUID, in ChannelRequestInput) error {
	if businessID == uuid.Nil {
		return ErrMissingBusiness
	}
	if _, ok := allowedChannels[in.Channel]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownChannel, in.Channel)
	}
	row := repository.ChannelDemandSignalRow{
		BusinessID: businessID,
		Channel:    in.Channel,
	}
	if in.Note != "" {
		note := in.Note
		row.Note = &note
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		return fmt.Errorf("channel demand record: %w", err)
	}
	return nil
}

// Summary returns the per-channel demand counts for one business, scoped to
// businessID so a caller only ever reads its own tenant's signals.
func (s *ChannelRequestService) Summary(ctx context.Context, businessID uuid.UUID) ([]ChannelDemandCount, error) {
	if businessID == uuid.Nil {
		return nil, ErrMissingBusiness
	}
	rows, err := s.repo.SummaryByBusiness(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("channel demand summary: %w", err)
	}
	out := make([]ChannelDemandCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, ChannelDemandCount{Channel: row.Channel, Count: row.Count})
	}
	return out, nil
}
