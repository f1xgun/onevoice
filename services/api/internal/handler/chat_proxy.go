package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/go-chi/chi/v5"
)

// ChatProxyHandler enriches chat requests with business context and proxies
// them to the orchestrator service.
type ChatProxyHandler struct {
	businessService    BusinessService
	integrationService IntegrationService
	orchestratorURL    string
	httpClient         *http.Client
}

// NewChatProxyHandler creates a new ChatProxyHandler. If httpClient is nil,
// http.DefaultClient is used.
func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	orchestratorURL string,
	httpClient *http.Client,
) *ChatProxyHandler {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ChatProxyHandler{
		businessService:    businessService,
		integrationService: integrationService,
		orchestratorURL:    orchestratorURL,
		httpClient:         httpClient,
	}
}

type chatProxyRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

// Chat enriches the incoming request with business context and streams the
// orchestrator's SSE response back to the client.
func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "conversationID")

	var req chatProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, "message is required")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	integrations, err := h.integrationService.ListByBusinessID(r.Context(), business.ID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	activeIntegrations := make([]string, 0)
	seen := make(map[string]bool)
	for _, integ := range integrations {
		if integ.Status == "active" && !seen[integ.Platform] {
			activeIntegrations = append(activeIntegrations, integ.Platform)
			seen[integ.Platform] = true
		}
	}

	orchReq := map[string]interface{}{
		"model":                req.Model,
		"message":              req.Message,
		"business_id":          business.ID.String(),
		"business_name":        business.Name,
		"business_category":    business.Category,
		"business_address":     business.Address,
		"business_phone":       business.Phone,
		"business_description": business.Description,
		"active_integrations":  activeIntegrations,
	}

	orchURL := fmt.Sprintf("%s/chat/%s", h.orchestratorURL, conversationID)
	body, _ := json.Marshal(orchReq)
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, orchURL, bytes.NewReader(body))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		slog.Error("orchestrator request failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	defer resp.Body.Close()

	// Stream SSE response back to client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			flusher.Flush()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			break
		}
	}
}
