// Package handler — HITL resolve + resume + GET /tools endpoints.
//
// This file ships every HITL HTTP surface on the API service:
//
//	POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve
//	POST /api/v1/chat/{id}/resume?batch_id=X
//	GET  /api/v1/tools
//
// See service/hitl.go for the business-logic layer (atomic transition, TOCTOU
// re-check, edit validation). This file handles HTTP parsing, error mapping,
// and SSE proxying.
package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/toolvalidation"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// HITLHandler serves the three new HITL endpoints. Takes the HITLService
// (business logic), the businessService (for actor → business resolution on
// the resume path), and the conversationRepo (ownership check on resume).
type HITLHandler struct {
	hitlService      *service.HITLService
	businessService  BusinessService
	conversationRepo domain.ConversationRepository
}

// NewHITLHandler constructs a HITLHandler. All three deps are required; a nil
// anywhere indicates a wiring error.
func NewHITLHandler(
	hitlService *service.HITLService,
	businessService BusinessService,
	conversationRepo domain.ConversationRepository,
) (*HITLHandler, error) {
	if hitlService == nil {
		return nil, fmt.Errorf("NewHITLHandler: hitlService cannot be nil")
	}
	if businessService == nil {
		return nil, fmt.Errorf("NewHITLHandler: businessService cannot be nil")
	}
	if conversationRepo == nil {
		return nil, fmt.Errorf("NewHITLHandler: conversationRepo cannot be nil")
	}
	return &HITLHandler{
		hitlService:      hitlService,
		businessService:  businessService,
		conversationRepo: conversationRepo,
	}, nil
}

// resolveRequest is the JSON body shape for POST /resolve.
// See 16-RESEARCH §Atomic Resolve Contract for the canonical shape.
type resolveRequest struct {
	Decisions []service.DecisionInput `json:"decisions"`
}

// ResolvePendingToolCalls handles
// POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve.
//
// Error mapping (service layer → HTTP):
//
//	ErrHITLBatchNotFound       → 404 {"error":"batch not found"}
//	ErrHITLForbidden           → 403 {"error":"forbidden"}
//	ErrHITLBatchExpired        → 410 {"error":"approval_expired"}
//	ErrHITLDecisionsShape      → 400 {"error":"shape mismatch","missing":[...]}
//	*toolvalidation.ErrFieldNotEditable → 400 {"error":"...","editable":[...]}
//	*toolvalidation.ErrNonScalarValue   → 400 {"error":"...","tool":"..."}
//	ErrHITLRejectReasonTooLong → 400
//	ErrHITLBatchAlreadyResolving → 409 {"error":"batch resolving","retry_after_ms":500,"reason":"concurrent resolve in progress"}
//	default                    → 500
//
// On success: 200 with the ResolveResult JSON body.
//
// HITL-07 pinning: the handler NEVER reads `tool_name` from the request body.
// The service layer always uses c.ToolName from the persisted PendingCall row.
// Any edit on a "tool_name" field is rejected by ValidateEditArgs because
// "tool_name" is not in any tool's EditableFields allowlist.
func (h *HITLHandler) ResolvePendingToolCalls(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Resolve actor's business so we can compare against batch.BusinessID.
	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "resolve: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	conversationID := chi.URLParam(r, "id")
	batchID := chi.URLParam(r, "batch_id")
	if conversationID == "" || batchID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing path params")
		return
	}

	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	in := service.ResolveInput{
		ConversationID:  conversationID,
		BatchID:         batchID,
		ActorUserID:     userID.String(),
		ActorBusinessID: business.ID.String(),
		Decisions:       req.Decisions,
	}

	result, err := h.hitlService.Resolve(r.Context(), in)
	if err != nil {
		h.mapResolveError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// mapResolveError translates HITLService errors to HTTP status codes + bodies.
// Centralized so the happy-path handler stays easy to read.
func (h *HITLHandler) mapResolveError(w http.ResponseWriter, r *http.Request, err error) {
	// Edit validation — HITL-07 / D-12: never silently ignore.
	var errEdit *toolvalidation.ErrFieldNotEditable
	if errors.As(err, &errEdit) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":    fmt.Sprintf("field %q not editable for tool %q", errEdit.Field, errEdit.Tool),
			"editable": nilToEmptyStringArr(errEdit.Editable),
		})
		return
	}
	var errScalar *toolvalidation.ErrNonScalarValue
	if errors.As(err, &errScalar) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": fmt.Sprintf("field %q must be string/number/bool", errScalar.Field),
			"tool":  errScalar.Tool,
		})
		return
	}

	// Shape mismatch — 400 with missing call_ids.
	var errShape *service.ErrHITLDecisionsShape
	if errors.As(err, &errShape) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "shape mismatch",
			"missing": errShape.Missing,
		})
		return
	}

	// Reject reason too long.
	var errReason *service.ErrHITLRejectReasonTooLong
	if errors.As(err, &errReason) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "reject_reason too long",
			"max":   errReason.Max,
		})
		return
	}

	// Sentinel path mapping.
	switch {
	case errors.Is(err, service.ErrHITLBatchNotFound):
		writeJSONError(w, http.StatusNotFound, "batch not found")
	case errors.Is(err, service.ErrHITLForbidden):
		writeJSONError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrHITLBatchExpired):
		writeJSON(w, http.StatusGone, map[string]string{"error": "approval_expired"})
	case errors.Is(err, service.ErrHITLBatchAlreadyResolving):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":          "batch resolving",
			"retry_after_ms": 500,
			"reason":         "concurrent resolve in progress",
		})
	default:
		slog.ErrorContext(r.Context(), "resolve: unmapped error", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

// nilToEmptyStringArr guarantees a non-null JSON array in the 400 body.
// When a tool has no EditableFields, we want `"editable":[]` not `"editable":null`.
func nilToEmptyStringArr(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Resume handles POST /api/v1/chat/{id}/resume?batch_id=X.
//
// The endpoint:
//  1. Validates ownership (actor's business owns the batch's conversation).
//  2. Rejects resume while the batch is in status=resolving (409) — this
//     protects against the client racing its own resume call against an
//     in-flight resolve.
//  3. Rejects resume on expired batches (410).
//  4. Re-fetches FRESH business approvals + project overrides so the
//     orchestrator's TOCTOU re-check uses current state (HITL-06).
//  5. Forwards to the orchestrator's POST /chat/{id}/resume?batch_id=X with
//     the fresh maps in the JSON body and streams the SSE response back.
func (h *HITLHandler) Resume(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "id")
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		writeJSONError(w, http.StatusBadRequest, "batch_id query param required")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "resume: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Load batch for ownership + status check.
	batch, err := h.hitlService.PendingRepo().GetByBatchID(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, domain.ErrBatchNotFound) {
			writeJSONError(w, http.StatusNotFound, "batch not found")
			return
		}
		slog.ErrorContext(r.Context(), "resume: failed to load batch", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if batch.BusinessID != business.ID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if batch.ConversationID != conversationID {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if batch.Status == "expired" {
		writeJSON(w, http.StatusGone, map[string]string{"error": "approval_expired"})
		return
	}
	if batch.Status == "resolving" {
		// The client may be racing its own resolve. Tell them to retry shortly.
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":          "batch resolving",
			"retry_after_ms": 500,
			"reason":         "resolve in flight",
		})
		return
	}

	// Fresh business + project state for TOCTOU re-check on the orchestrator side.
	bizApprovals := business.ToolApprovals()
	var projectOverrides map[string]domain.ToolFloor
	if batch.ProjectID != "" {
		if projUUID, perr := uuid.Parse(batch.ProjectID); perr == nil {
			if proj, perr := h.hitlService.ProjectRepo().GetByID(r.Context(), projUUID); perr == nil && proj != nil {
				projectOverrides = proj.ApprovalOverrides
			}
		}
	}

	// Forward to the orchestrator's /chat/{id}/resume endpoint with the fresh
	// maps. The orchestrator's ResumeHandler decodes this body and passes the
	// fresh maps into orchestrator.Resume → dispatchApprovedCalls →
	// hitl.Resolve.
	body := map[string]interface{}{
		"business_approvals":         bizApprovals,
		"project_approval_overrides": projectOverrides,
	}
	raw, _ := json.Marshal(body)

	orchURL := fmt.Sprintf("%s/chat/%s/resume?batch_id=%s",
		strings.TrimRight(h.hitlService.OrchestratorURL(), "/"),
		conversationID, batchID)
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, orchURL, strings.NewReader(string(raw)))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	if corrID := logger.CorrelationIDFromContext(r.Context()); corrID != "" {
		proxyReq.Header.Set("X-Correlation-ID", corrID)
	}

	resp, err := h.hitlService.HTTPClient().Do(proxyReq)
	if err != nil {
		slog.ErrorContext(r.Context(), "resume: orchestrator request failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// 1 MB scanner buffer matching chat_proxy (HITL-13: large tool results must flow through).
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		_, _ = fmt.Fprintf(w, "%s\n", scanner.Text())
		flusher.Flush()
	}
}

// GetTools handles GET /api/v1/tools. Returns the live orchestrator registry
// projection (name, platform, floor, editable_fields, description) via the
// cached ToolsRegistryCache (5-min TTL).
//
// The frontend's React Query cache further caches the response client-side
// so settings pages + the project edit page share a single in-browser copy.
func (h *HITLHandler) GetTools(w http.ResponseWriter, r *http.Request) {
	// Auth is enforced by middleware; no per-user scoping on this endpoint —
	// the tool registry is the same for every business.
	entries := h.hitlService.ToolsCache().List(r.Context())
	// Normalize nil EditableFields to [] so downstream consumers never see null.
	for i := range entries {
		if entries[i].EditableFields == nil {
			entries[i].EditableFields = []string{}
		}
	}
	writeJSON(w, http.StatusOK, entries)
}
