package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/sseevent"
)

// Resumer is the narrow interface consumed by ResumeHandler. Implemented by
// *orchestrator.Orchestrator in production; tests inject a stub.
type Resumer interface {
	Resume(ctx context.Context, req orchestrator.ResumeRequest) (<-chan orchestrator.Event, error)
}

// ResumeHandler handles POST /chat/{conversationID}/resume?batch_id=X.
//
// The API service (services/api) sits in front of this endpoint — external
// clients reach it only via chat_proxy's streamResume path. The body carries
// the FRESH business/project approval maps the API re-fetched from Postgres
// at resolve time so the orchestrator's dispatchApprovedCalls can re-run
// hitl.Resolve against them (TOCTOU safety).
//
// The response is text/event-stream — same wire shape as POST /chat/{id}
// (text / tool_call / tool_result / tool_rejected / done / error events).
type ResumeHandler struct {
	resumer Resumer
}

// NewResumeHandler constructs a ResumeHandler. resumer must be non-nil.
func NewResumeHandler(resumer Resumer) *ResumeHandler {
	if resumer == nil {
		panic("NewResumeHandler: resumer cannot be nil")
	}
	return &ResumeHandler{resumer: resumer}
}

// resumeRequest is the JSON shape chat_proxy sends in the request body.
// All fields are optional from the orchestrator's perspective — an empty body
// is acceptable and produces a Resume with zero-value maps (safe defaults:
// nothing flips to forbidden, nothing upgrades from auto).
//
// chat_proxy currently sends an empty body (http.NoBody); the
// resolve-endpoint follow-up path sends the full body with fresh
// approval maps. Both paths are supported simultaneously.
type resumeRequest struct {
	BusinessApprovals        map[string]domain.ToolFloor `json:"business_approvals"`
	ProjectApprovalOverrides map[string]domain.ToolFloor `json:"project_approval_overrides"`
	ActiveIntegrations       []string                    `json:"active_integrations"`
	WhitelistMode            string                      `json:"whitelist_mode"`
	AllowedTools             []string                    `json:"allowed_tools"`
	Model                    string                      `json:"model"`
	Tier                     string                      `json:"tier"`
}

// Resume handles POST /chat/{conversationID}/resume?batch_id=X.
func (h *ResumeHandler) Resume(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		http.Error(w, `{"error":"batch_id query param is required"}`, http.StatusBadRequest)
		return
	}

	var req resumeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.WarnContext(r.Context(), "resume: body decode failed, using zero-value request",
				"batch_id", batchID, "error", err,
			)
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	if corrID := r.Header.Get("X-Correlation-ID"); corrID != "" {
		ctx = logger.WithCorrelationID(ctx, corrID)
	}

	resumeReq := orchestrator.ResumeRequest{
		BatchID:                  batchID,
		BusinessApprovals:        req.BusinessApprovals,
		ProjectApprovalOverrides: req.ProjectApprovalOverrides,
		ActiveIntegrations:       req.ActiveIntegrations,
		WhitelistMode:            domain.WhitelistMode(req.WhitelistMode),
		AllowedTools:             req.AllowedTools,
		Model:                    req.Model,
		Tier:                     req.Tier,
	}

	events, err := h.resumer.Resume(ctx, resumeReq)
	if err != nil {
		writeSSE(ctx, w, flusher, sse.Event{Type: "error", Content: err.Error()})
		return
	}

	for event := range events {
		writeSSE(ctx, w, flusher, sseevent.FromEvent(event))
	}
}

// RegisterResumeRoute wires POST /chat/{conversationID}/resume onto the given
// chi router. Called from services/orchestrator/cmd/main.go alongside the
// existing POST /chat/{conversationID} route.
func RegisterResumeRoute(r chi.Router, h *ResumeHandler) {
	r.Post("/chat/{conversationID}/resume", h.Resume)
}
