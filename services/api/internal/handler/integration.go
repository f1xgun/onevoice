package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// IntegrationService defines the interface for integration operations
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID, actorID uuid.UUID) error
	MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) error
}

// driftLister exposes the proactive-sync reconciliation state to the drift +
// verify endpoints. *service.ReconciliationService satisfies it. Optional — nil
// when SYNC_RECONCILE is not wired, in which case the drift endpoint returns an
// empty list and verify skips the re-check reschedule.
type driftLister interface {
	ListDrift(ctx context.Context, businessID uuid.UUID) ([]domain.SyncState, error)
	ScheduleImmediate(ctx context.Context, businessID uuid.UUID) error
}

// businessSyncTrigger re-pushes the stored business profile to every connected
// platform. *platform.Syncer satisfies it. It is the SAME re-push the reactive
// path uses — the verify endpoint performs manual repair, not a new sync path.
type businessSyncTrigger interface {
	SyncBusiness(business *domain.Business)
}

// IntegrationHandler handles integration endpoints
type IntegrationHandler struct {
	integrationService IntegrationService
	businessService    businessGetter
	audit              audit.Logger

	// reconciler + syncTrigger back the drift + verify endpoints. Both optional
	// (nil-safe) so the handler works with reconciliation unwired.
	reconciler  driftLister
	syncTrigger businessSyncTrigger
}

// NewIntegrationHandler creates a new integration handler instance.
//
// businessService lets DeleteIntegration reject disconnects against a
// soft-deleted (erasure-pending) organization, which the RequireBusinessAccess
// middleware does not filter.
//
// auditLogger receives the integration.disconnected
// event emitted from DeleteIntegration. Connected + token_rotated are emitted
// from the service layer (one call site per action — ). nil-safe via
// audit.Nop so existing handler tests that pass nil still work.
func NewIntegrationHandler(integrationService IntegrationService, businessService BusinessService, auditLogger audit.Logger) (*IntegrationHandler, error) {
	if integrationService == nil {
		return nil, fmt.Errorf("NewIntegrationHandler: integrationService cannot be nil")
	}
	if businessService == nil {
		return nil, fmt.Errorf("NewIntegrationHandler: businessService cannot be nil")
	}
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &IntegrationHandler{
		integrationService: integrationService,
		businessService:    businessService,
		audit:              auditLogger,
	}, nil
}

// SetReconciler injects the proactive-sync collaborators that back the drift +
// verify endpoints. Both may be nil (reconciliation unwired); the endpoints
// degrade gracefully. Called from wire/handlers.go after construction so the
// existing constructor signature (and its many test call sites) is unchanged.
func (h *IntegrationHandler) SetReconciler(reconciler driftLister, syncTrigger businessSyncTrigger) {
	h.reconciler = reconciler
	h.syncTrigger = syncTrigger
}

// ListIntegrations returns all integrations for the business from the request context.
func (h *IntegrationHandler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListIntegrations", authz.PermIntegrationsRead)
	if !ok {
		return
	}

	integrations, err := h.integrationService.ListByBusinessID(r.Context(), bc.BusinessID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, integrations)
}

// DeleteIntegration deletes an integration by ID
func (h *IntegrationHandler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "DeleteIntegration", authz.PermIntegrationsDisconnect)
	if !ok {
		return
	}

	if _, err := h.businessService.GetByID(r.Context(), bc.BusinessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "delete integration: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	idStr := chi.URLParam(r, "integrationId")
	integrationID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid integration ID")
		return
	}

	integrations, err := h.integrationService.ListByBusinessID(r.Context(), bc.BusinessID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var target *domain.Integration
	for i := range integrations {
		if integrations[i].ID == integrationID {
			target = &integrations[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, http.StatusNotFound, "integration not found")
		return
	}

	err = h.integrationService.Delete(r.Context(), integrationID, bc.UserID)
	if err != nil {
		slog.Error("failed to delete integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	audit.LogIntegrationDisconnected(r.Context(), h.audit, bc.BusinessID, bc.UserID, integrationID, target.Platform)

	writeJSON(w, http.StatusNoContent, nil)
}

// driftView is the per-channel drift status surfaced to the frontend. It never
// includes the raw remote snapshot (which may hold transient PII) — only the
// drifted field names and the check schedule.
type driftView struct {
	Platform      string     `json:"platform"`
	ExternalID    string     `json:"externalId"`
	DriftDetected bool       `json:"driftDetected"`
	DriftFields   []string   `json:"driftFields"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	NextCheckAt   time.Time  `json:"nextCheckAt"`
}

// GetIntegrationsDrift returns the proactive-sync drift status for every
// connected channel of the business. Read-gated on PermIntegrationsRead like
// ListIntegrations. Returns an empty list when reconciliation is unwired.
func (h *IntegrationHandler) GetIntegrationsDrift(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetIntegrationsDrift", authz.PermIntegrationsRead)
	if !ok {
		return
	}

	if h.reconciler == nil {
		writeJSON(w, http.StatusOK, []driftView{})
		return
	}

	states, err := h.reconciler.ListDrift(r.Context(), bc.BusinessID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list drift state", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	views := make([]driftView, 0, len(states))
	for _, s := range states {
		fields := s.DriftFields
		if fields == nil {
			fields = []string{}
		}
		views = append(views, driftView{
			Platform:      s.Platform,
			ExternalID:    s.ExternalID,
			DriftDetected: s.DriftDetected,
			DriftFields:   fields,
			LastCheckedAt: s.LastCheckedAt,
			NextCheckAt:   s.NextCheckAt,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

// VerifyIntegrations performs manual repair: it re-pushes the stored business
// profile to every connected platform (the SAME re-push the reactive path uses)
// and reschedules the channels for an immediate re-check. Write-gated on
// PermBusinessUpdate because it mutates the remote platforms.
func (h *IntegrationHandler) VerifyIntegrations(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "VerifyIntegrations", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "verify integrations: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncTrigger != nil {
		go h.syncTrigger.SyncBusiness(business)
	}
	if h.reconciler != nil {
		if err := h.reconciler.ScheduleImmediate(r.Context(), bc.BusinessID); err != nil {
			slog.WarnContext(r.Context(), "verify integrations: failed to reschedule drift check", "error", err)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "repair_started"})
}
