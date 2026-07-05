// Package handler — channel_request.go.
//
// ChannelRequestHandler implements the not-yet-supported-channel fake-door:
// POST /businesses/{id}/channel-requests records demand for a channel OneVoice
// does not support yet, and GET returns the per-channel demand aggregate for the
// business. Both are business-scoped (RequireBusinessAccess + content perm) so a
// caller only ever writes or reads its own tenant's signals.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// maxChannelRequestPayloadBytes caps the request body. It sits generously above
// the 280-rune note cap so a multi-byte Cyrillic note at the rune limit is never
// truncated before validator counts its runes.
const maxChannelRequestPayloadBytes = 4 * 1024

// channelRequestService is the narrow service surface the handler depends on.
type channelRequestService interface {
	Record(ctx context.Context, businessID uuid.UUID, in service.ChannelRequestInput) error
	Summary(ctx context.Context, businessID uuid.UUID) ([]service.ChannelDemandCount, error)
}

// ChannelRequestHandler handles channel-demand capture.
type ChannelRequestHandler struct {
	svc channelRequestService
}

// NewChannelRequestHandler constructs a ChannelRequestHandler.
func NewChannelRequestHandler(svc channelRequestService) *ChannelRequestHandler {
	return &ChannelRequestHandler{svc: svc}
}

// Create handles POST /businesses/{id}/channel-requests. The enum + note-length
// checks are enforced by decodeAndValidate from the spec-derived validate tags;
// the service re-validates the channel as defense in depth.
func (h *ChannelRequestHandler) Create(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "CreateChannelRequest", authz.PermContentCreate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChannelRequestPayloadBytes)
	req, ok := decodeAndValidate[openapi.CreateChannelRequestRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	in := service.ChannelRequestInput{Channel: string(req.Channel)}
	if req.Note != nil {
		in.Note = *req.Note
	}

	if err := h.svc.Record(r.Context(), bc.BusinessID, in); err != nil {
		if errors.Is(err, service.ErrUnknownChannel) {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		slog.ErrorContext(r.Context(), "channel request record failed", "error", err, "businessID", bc.BusinessID)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /businesses/{id}/channel-requests, returning the per-channel
// demand aggregate for the caller's business.
func (h *ChannelRequestHandler) List(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListChannelRequests", authz.PermContentRead)
	if !ok {
		return
	}

	counts, err := h.svc.Summary(r.Context(), bc.BusinessID)
	if err != nil {
		slog.ErrorContext(r.Context(), "channel request summary failed", "error", err, "businessID", bc.BusinessID)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	out := make([]openapi.ChannelDemandCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, openapi.ChannelDemandCount{
			Channel: openapi.ChannelRequestChannel(c.Channel),
			Count:   c.Count,
		})
	}

	writeJSON(w, http.StatusOK, openapi.ChannelDemandSummary{Channels: out})
}
