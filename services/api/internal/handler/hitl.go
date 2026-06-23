// Package handler — HITL resolve + resume + GET /tools endpoints.
// See docs/api/handlers/hitl.md.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/chatturn"
)

// hitlBatchResolvingRetryAfterMs is the 409 retry hint (ms). 500ms balances
// "don't hammer" vs "feels responsive". See docs/api/handlers/hitl.md.
const hitlBatchResolvingRetryAfterMs = 500

// chatResumer is the narrow slice of the chat-turn lifecycle that Resume needs:
// run the orchestrator resume stream AND persist the finalized assistant
// message. *chatturn.Turn satisfies it. Kept as an interface so HITL tests can
// inject a fake without wiring a full Turn.
type chatResumer interface {
	ResumeApproved(ctx context.Context, w http.ResponseWriter, conversationID, batchID string, body []byte) (chatturn.TurnOutcome, error)
}

// HITLHandler serves the three HITL HTTP endpoints. See docs/api/handlers/hitl.md.
type HITLHandler struct {
	hitlService      *service.HITLService
	businessService  BusinessService
	conversationRepo domain.ConversationRepository
	resumer          chatResumer
}

// NewHITLHandler constructs a HITLHandler; all four deps are required. resumer
// is the shared chat-turn lifecycle — Resume delegates to it so the approved-tool
// result is persisted (the bare SSE proxy this replaces never wrote it back).
func NewHITLHandler(
	hitlService *service.HITLService,
	businessService BusinessService,
	conversationRepo domain.ConversationRepository,
	resumer chatResumer,
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
	if resumer == nil {
		return nil, fmt.Errorf("NewHITLHandler: resumer cannot be nil")
	}
	return &HITLHandler{
		hitlService:      hitlService,
		businessService:  businessService,
		conversationRepo: conversationRepo,
		resumer:          resumer,
	}, nil
}

// resolveRequest is the spec-owned wire shape for POST /resolve. The
// handler accepts openapi.HITLResolveRequest (decisions: HITLDecisionInput[])
// and the local toServiceDecisions mapper translates each row into the
// internal service.DecisionInput (string-action + value-typed maps).
type resolveRequest = openapi.HITLResolveRequest

// toServiceDecisions converts the spec-side decision rows to the internal
// service.DecisionInput slice. EditedArgs and RejectReason are pointer-typed
// in the spec (omitempty); the service layer expects value-typed fields with
// nil maps / empty strings standing in for "absent".
func toServiceDecisions(rows []openapi.HITLDecisionInput) []service.DecisionInput {
	out := make([]service.DecisionInput, 0, len(rows))
	for _, d := range rows {
		di := service.DecisionInput{
			ID:     d.Id,
			Action: string(d.Action),
		}
		if d.EditedArgs != nil {
			di.EditedArgs = *d.EditedArgs
		}
		if d.RejectReason != nil {
			di.RejectReason = *d.RejectReason
		}
		out = append(out, di)
	}
	return out
}

// ResolvePendingToolCalls handles POST /conversations/{id}/pending-tool-calls/{batch_id}/resolve.
// See docs/api/handlers/hitl.md.
func (h *HITLHandler) ResolvePendingToolCalls(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ResolvePendingToolCalls", authz.PermContentUpdate)
	if !ok {
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
		ActorUserID:     bc.UserID.String(),
		ActorBusinessID: bc.BusinessID.String(),
		Decisions:       toServiceDecisions(req.Decisions),
	}

	result, err := h.hitlService.Resolve(r.Context(), in)
	if err != nil {
		h.mapResolveError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// mapResolveError translates HITLService errors to HTTP status codes + bodies.
// See docs/api/handlers/hitl.md for the full mapping table.
func (h *HITLHandler) mapResolveError(w http.ResponseWriter, r *http.Request, err error) {
	var errEdit *tools.ErrFieldNotEditable
	if errors.As(err, &errEdit) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":    fmt.Sprintf("field %q not editable for tool %q", errEdit.Field, errEdit.Tool),
			"editable": nilToEmptyStringArr(errEdit.Editable),
		})
		return
	}
	var errScalar *tools.ErrNonScalarValue
	if errors.As(err, &errScalar) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": fmt.Sprintf("field %q must be string/number/bool", errScalar.Field),
			"tool":  errScalar.Tool,
		})
		return
	}

	var errShape *service.ErrHITLDecisionsShape
	if errors.As(err, &errShape) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "shape mismatch",
			"missing": errShape.Missing,
		})
		return
	}

	var errReason *service.ErrHITLRejectReasonTooLong
	if errors.As(err, &errReason) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "reject_reason too long",
			"max":   errReason.Max,
		})
		return
	}

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
			"retry_after_ms": hitlBatchResolvingRetryAfterMs,
			"reason":         "concurrent resolve in progress",
		})
	default:
		slog.ErrorContext(r.Context(), "resolve: unmapped error", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

// nilToEmptyStringArr coerces nil → [] so the JSON body emits `"editable":[]` not `null`.
func nilToEmptyStringArr(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Resume handles POST /chat/{id}/resume?batch_id=X. See docs/api/handlers/hitl.md.
func (h *HITLHandler) Resume(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "Resume", authz.PermContentCreate)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		writeJSONError(w, http.StatusBadRequest, "batch_id query param required")
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "resume: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

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
	if batch.Status == "resolved" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":  "batch already resolved",
			"reason": "already_resolved",
		})
		return
	}

	bizApprovals := business.ToolApprovals()
	var projectOverrides map[string]domain.ToolFloor
	if batch.ProjectID != "" {
		if projUUID, perr := uuid.Parse(batch.ProjectID); perr == nil {
			if proj, perr := h.hitlService.ProjectRepo().GetByID(r.Context(), projUUID); perr == nil && proj != nil {
				projectOverrides = proj.ApprovalOverrides
			}
		}
	}

	body := map[string]interface{}{
		"business_approvals":         bizApprovals,
		"project_approval_overrides": projectOverrides,
	}
	raw, _ := json.Marshal(body)

	// Delegate to the shared chat-turn lifecycle so the approved-tool result is
	// accumulated and the assistant message is finalized (complete + results).
	// The bare SSE proxy this replaces streamed the result to the browser but
	// never wrote it back, stranding the message at pending_approval and bricking
	// the conversation with turn_already_in_progress on the next message.
	outcome, streamErr := h.resumer.ResumeApproved(r.Context(), w, conversationID, batchID, raw)
	metrics.IncChatTurn(outcome.String())
	if outcome == chatturn.OutcomeOrchestratorUnavailable {
		slog.ErrorContext(r.Context(), "resume: orchestrator request failed", "error", streamErr)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	if streamErr != nil {
		slog.WarnContext(r.Context(), "resume: stream ended with error",
			"error", streamErr, "conversation_id", conversationID)
	}
}

// GetTools handles GET /tools — registry projection via 5-min cache.
// See docs/api/handlers/hitl.md §"GetTools projection".
func (h *HITLHandler) GetTools(w http.ResponseWriter, r *http.Request) {
	entries := h.hitlService.ToolsCache().List(r.Context())
	for i := range entries {
		if entries[i].EditableFields == nil {
			entries[i].EditableFields = []string{}
		}
	}
	writeJSON(w, http.StatusOK, entries)
}
