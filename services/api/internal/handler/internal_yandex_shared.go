package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/yandexcookies"
)

// sharedSessionSetter is the narrow slice of the integration service the
// bootstrap handler needs. *service.integrationService (via IntegrationService)
// satisfies it.
type sharedSessionSetter interface {
	SetSharedSession(ctx context.Context, params service.SharedSessionParams) (*domain.Integration, error)
}

// InternalYandexSharedHandler sets/rotates the single shared representative
// session used by delegated-representative access. It is served ONLY on the
// mTLS-protected internal listener behind a service-identity allowlist — the
// ops/admin bootstrap path. It never touches customer credentials.
type InternalYandexSharedHandler struct {
	setter           sharedSessionSetter
	sharedBusinessID string
}

// NewInternalYandexSharedHandler constructs the bootstrap handler. sharedBusinessID
// is the config sentinel (YANDEX_SHARED_BUSINESS_ID); an empty value makes the
// endpoint fail closed ("delegated access not configured").
func NewInternalYandexSharedHandler(setter sharedSessionSetter, sharedBusinessID string) *InternalYandexSharedHandler {
	return &InternalYandexSharedHandler{setter: setter, sharedBusinessID: sharedBusinessID}
}

// setSharedSessionRequest is the internal bootstrap payload. cookies is the
// shared representative session cookie JSON (same paste format the cookie probe
// accepts).
type setSharedSessionRequest struct {
	Cookies string `json:"cookies"`
}

// SetSharedSession handles POST /internal/v1/yandex/shared-session. It validates
// the pasted cookies, then stores/rotates them as the KMS-wrapped shared
// singleton under the sentinel business.
func (h *InternalYandexSharedHandler) SetSharedSession(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.sharedBusinessID) == "" {
		writeJSONError(w, http.StatusServiceUnavailable, "delegated access not configured")
		return
	}
	sharedBusinessID, err := uuid.Parse(strings.TrimSpace(h.sharedBusinessID))
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "delegated access misconfigured: shared business id is not a uuid")
		return
	}

	var req setSharedSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parsed, err := yandexcookies.Parse(req.Cookies)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid cookies: "+err.Error())
		return
	}

	integration, err := h.setter.SetSharedSession(r.Context(), service.SharedSessionParams{
		SharedBusinessID: sharedBusinessID,
		Platform:         a2a.AgentYandexBusiness,
		Credential:       parsed.JSON(),
		ActorIP:          r.RemoteAddr,
		UserAgent:        r.Header.Get("User-Agent"),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to set shared session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "integration_id": integration.ID.String()})
}
