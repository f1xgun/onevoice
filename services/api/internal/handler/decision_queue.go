package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type DecisionQueueReader interface {
	List(context.Context, uuid.UUID, uuid.UUID, service.DecisionAccess) (service.DecisionQueue, error)
}

type DecisionQueueHandler struct{ service DecisionQueueReader }

func NewDecisionQueueHandler(svc DecisionQueueReader) *DecisionQueueHandler {
	return &DecisionQueueHandler{service: svc}
}

func (h *DecisionQueueHandler) List(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "decision-queue", authz.PermBusinessRead)
	if !ok {
		return
	}
	queue, err := h.service.List(r.Context(), bc.BusinessID, bc.UserID, service.DecisionAccess{
		Content:      authz.Can(r.Context(), authz.PermContentRead),
		Integrations: authz.Can(r.Context(), authz.PermIntegrationsRead),
		Audit:        authz.Can(r.Context(), authz.PermAuditRead),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "decision queue failed", "business_id", bc.BusinessID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "decision_queue_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, queue)
}
