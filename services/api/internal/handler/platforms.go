package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// PlatformAvailability tells the PlatformsHandler which integration platforms
// have their credentials configured in the current deployment. The handler
// uses this to downgrade the default Active status to OAuthNotConfigured for
// platforms whose credentials are missing, so the frontend can hide broken
// connect flows.
//
// Each field is true iff the minimum credentials required to run the
// corresponding flow are present (non-empty). Telegram uses a bot token
// rather than OAuth; the field name keeps the OAuth-centric naming for
// frontend symmetry.
type PlatformAvailability struct {
	Telegram       bool
	VK             bool
	YandexBusiness bool
	GoogleBusiness bool
}

// PlatformsHandler exposes the canonical integration platform registry.
type PlatformsHandler struct {
	availability PlatformAvailability
}

// NewPlatformsHandler creates a PlatformsHandler that reports the registry
// merged with the supplied availability flags.
func NewPlatformsHandler(availability PlatformAvailability) *PlatformsHandler {
	return &PlatformsHandler{availability: availability}
}

// List handles GET /api/v1/platforms.
//
// The endpoint is intentionally unauthenticated so the marketing landing page
// can render the list of supported platforms without prompting for login. The
// returned data is non-sensitive — only platform names, descriptions, and a
// derived availability status, no credentials and no per-tenant data.
func (h *PlatformsHandler) List(w http.ResponseWriter, r *http.Request) {
	platforms := domain.Platforms()
	for i := range platforms {
		if platforms[i].Status != domain.PlatformStatusActive {
			continue
		}
		if !h.isConfigured(platforms[i].ID) {
			platforms[i].Status = domain.PlatformStatusOAuthNotConfigured
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if err := json.NewEncoder(w).Encode(platforms); err != nil {
		slog.ErrorContext(r.Context(), "platforms list: encode", "error", err)
	}
}

func (h *PlatformsHandler) isConfigured(id string) bool {
	switch id {
	case "telegram":
		return h.availability.Telegram
	case "vk":
		return h.availability.VK
	case "yandex_business":
		return h.availability.YandexBusiness
	case "google_business":
		return h.availability.GoogleBusiness
	}
	// Coming-soon platforms never reach this branch (early-returned by caller).
	// Unknown ids default to "configured" so a future registry addition does
	// not silently disappear if someone forgets to extend this switch.
	return true
}
