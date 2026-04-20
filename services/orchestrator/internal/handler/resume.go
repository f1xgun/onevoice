package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
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
// hitl.Resolve against them (HITL-06 TOCTOU safety).
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
// Plan 16-06's chat_proxy currently sends an empty body (http.NoBody); Plan
// 16-07's resolve-endpoint follow-up path sends the full body with fresh
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

	// Body is optional: http.NoBody from chat_proxy's implicit-resume path is
	// perfectly acceptable. Only attempt to decode if there's a content body.
	var req resumeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Tolerant: ignore decode errors and proceed with zero-value request.
			// The orchestrator will re-resolve with empty maps which means no
			// business/project overrides apply — safe default.
			slog.WarnContext(r.Context(), "resume: body decode failed, using zero-value request",
				"batch_id", batchID, "error", err,
			)
		}
	}

	// SSE headers (same shape as chat_proxy expects).
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
		writeSSE(ctx, w, flusher, sseEvent{Type: "error", Content: err.Error()})
		return
	}

	for event := range events {
		sse := sseEvent{Type: string(event.Type), Content: event.Content}
		switch event.Type {
		case orchestrator.EventToolCall:
			sse.ToolCallID = event.ToolCallID
			sse.ToolName = event.ToolName
			sse.ToolArgs = event.ToolArgs
		case orchestrator.EventToolResult:
			sse.ToolCallID = event.ToolCallID
			sse.ToolName = event.ToolName
			sse.ToolResult = event.ToolResult
			sse.ToolError = event.ToolError
		case orchestrator.EventToolRejected:
			sse.ToolCallID = event.ToolCallID
			sse.ToolName = event.ToolName
		case orchestrator.EventToolApprovalRequired:
			// Resume typically does NOT re-emit approval events (the batch is
			// already resolved by the time Resume is called). Surface it
			// anyway for defense-in-depth — if a resumed turn hits ANOTHER
			// manual-floor tool on the next iteration, we want the client to
			// see the new pause event.
			sse.BatchID = event.BatchID
			sse.Calls = event.Calls
		case orchestrator.EventText, orchestrator.EventError, orchestrator.EventDone:
			// No additional fields beyond Type + Content.
		}
		writeSSE(ctx, w, flusher, sse)
	}
}

// RegisterResumeRoute wires POST /chat/{conversationID}/resume onto the given
// chi router. Called from services/orchestrator/cmd/main.go alongside the
// existing POST /chat/{conversationID} route.
func RegisterResumeRoute(r chi.Router, h *ResumeHandler) {
	r.Post("/chat/{conversationID}/resume", h.Resume)
}
