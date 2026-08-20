// Package handler — landing.go.
//
// LandingHandler serves the two public, unauthenticated marketing-landing
// capture endpoints: POST /waitlist (closed-beta signup) and POST
// /channel-votes (fake-door vote). Both are rate-limited per IP at the router
// and return 204 with no body, so neither leaks whether a record already
// existed.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// maxLandingPayloadBytes caps both landing request bodies. It sits generously
// above the largest field (a 280-rune note / 320-char email) so a multi-byte
// Cyrillic value at its limit is never truncated before the validator sees it.
const maxLandingPayloadBytes = 4 * 1024

// landingService is the narrow service surface the handler depends on.
type landingService interface {
	JoinWaitlist(ctx context.Context, in service.WaitlistInput) error
	RecordChannelVote(ctx context.Context, in service.ChannelVoteInput) error
}

// LandingHandler handles public marketing-landing capture.
type LandingHandler struct {
	svc landingService
}

// NewLandingHandler constructs a LandingHandler.
func NewLandingHandler(svc landingService) *LandingHandler {
	return &LandingHandler{svc: svc}
}

// JoinWaitlist handles POST /waitlist. The email/enum/consent checks are
// enforced by decodeAndValidate from the spec-derived validate tags; the
// service re-validates and normalizes as defense in depth.
func (h *LandingHandler) JoinWaitlist(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLandingPayloadBytes)
	req, ok := decodeAndValidate[openapi.WaitlistRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	in := service.WaitlistInput{Email: string(req.Email), Consent: req.Consent}
	if req.Sphere != nil {
		in.Sphere = string(*req.Sphere)
	}
	if req.Pain != nil {
		in.Pain = string(*req.Pain)
	}

	if err := h.svc.JoinWaitlist(r.Context(), in); err != nil {
		if errors.Is(err, service.ErrConsentRequired) ||
			errors.Is(err, service.ErrInvalidEmail) ||
			errors.Is(err, service.ErrInvalidSegment) {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		slog.ErrorContext(r.Context(), "waitlist join failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RecordChannelVote handles POST /channel-votes.
func (h *LandingHandler) RecordChannelVote(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLandingPayloadBytes)
	req, ok := decodeAndValidate[openapi.PublicChannelVoteRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	in := service.ChannelVoteInput{Channel: string(req.Channel)}
	if req.Note != nil {
		in.Note = *req.Note
	}

	if err := h.svc.RecordChannelVote(r.Context(), in); err != nil {
		if errors.Is(err, service.ErrUnknownVoteChannel) {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		slog.ErrorContext(r.Context(), "channel vote record failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
