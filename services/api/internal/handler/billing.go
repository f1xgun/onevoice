// Package handler — billing.go
//
// BillingHandler serves the read-only usage-transparency endpoint
// GET /businesses/{id}/billing/summary. It exposes plan + credits + usage; no
// payment surface (that is Track-B). Gated by authz.PermBillingRead.
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// BillingSummarizer is the narrow service surface the handler depends on.
type BillingSummarizer interface {
	Summary(ctx context.Context, businessID uuid.UUID) (service.BillingSummary, error)
}

// BillingHandler implements GET /businesses/{id}/billing/summary.
type BillingHandler struct {
	svc BillingSummarizer
}

// NewBillingHandler constructs a BillingHandler.
func NewBillingHandler(svc BillingSummarizer) *BillingHandler {
	return &BillingHandler{svc: svc}
}

// Summary handles GET /api/v1/businesses/{id}/billing/summary.
func (h *BillingHandler) Summary(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "billing.Summary", authz.PermBillingRead)
	if !ok {
		return
	}

	summary, err := h.svc.Summary(r.Context(), bc.BusinessID)
	if err != nil {
		slog.ErrorContext(r.Context(), "billing: summary failed",
			"error", err,
			"business_id", bc.BusinessID,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}
