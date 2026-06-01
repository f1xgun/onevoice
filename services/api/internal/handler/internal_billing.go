package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// maxBillingPayloadBytes caps the request body size on the internal billing
// endpoint. A populated llm.UsageLog round-trips to well under 1 KB; 64 KB is
// a generous DoS guard.
const maxBillingPayloadBytes = 64 * 1024

// BillingService is the narrow write-only surface the internal billing
// handler depends on. LogUsage backs the POST usage_logs endpoint;
// GetDailySpend backs the GET daily_spend endpoint that the orchestrator's
// rate-limiter consults before each chat turn.
type BillingService interface {
	LogUsage(ctx context.Context, log *llm.UsageLog) error
	GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error)
}

// InternalBillingHandler serves POST /internal/v1/billing/usage_logs on the
// mTLS-protected :8443 listener. It is reached only via the orchestrator's
// pkg/billingclient, gated by router-level middleware.RequireServiceIdentity.
type InternalBillingHandler struct {
	billing BillingService
	log     *slog.Logger
}

// NewInternalBillingHandler wires the handler with a backing BillingService
// (production: services/api/internal/repository.billingRepository).
// A nil logger falls back to slog.Default().
func NewInternalBillingHandler(b BillingService, log *slog.Logger) *InternalBillingHandler {
	if log == nil {
		log = slog.Default()
	}
	return &InternalBillingHandler{billing: b, log: log}
}

// LogUsage handles POST /internal/v1/billing/usage_logs.
//
// Status codes:
//   - 204 No Content — success.
//   - 400 Bad Request — invalid JSON, missing required fields, or negative
//     token counts. Body is {"error":"invalid_payload","detail":"…"}.
//   - 405 Method Not Allowed — non-POST verb.
//   - 500 Internal Server Error — repository failure. Body is
//     {"error":"transient"} (no internal details leaked).
func (h *InternalBillingHandler) LogUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBillingError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBillingPayloadBytes)

	var log llm.UsageLog
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&log); err != nil {
		h.log.WarnContext(r.Context(), "internal_billing: decode failed",
			"error", err)
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "could not decode body")
		return
	}

	if log.BusinessID == uuid.Nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "business_id required")
		return
	}
	if log.Model == "" {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "model required")
		return
	}
	if log.InputTokens < 0 || log.OutputTokens < 0 ||
		log.CacheReadTokens < 0 || log.CacheCreationTokens < 0 {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "token counts must be >= 0")
		return
	}

	if err := h.billing.LogUsage(r.Context(), &log); err != nil {
		h.log.ErrorContext(r.Context(), "internal_billing: repo failed",
			"error", err,
			"business_id", log.BusinessID,
		)
		writeBillingError(w, http.StatusInternalServerError, "transient", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeBillingError emits the {"error":..,"detail":..} envelope. `detail` is
// rendered via omitempty so an empty string disappears from the wire.
func writeBillingError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := openapi.BillingErrorResponse{Error: code}
	if detail != "" {
		body.Detail = &detail
	}
	_ = json.NewEncoder(w).Encode(body)
}

// dailySpendDateFormat is the wire format for the date query parameter on
// GET daily_spend. time.Parse against this layout anchors the result in UTC
// so callers in non-UTC time zones still land on the UTC calendar day they
// asked for.
const dailySpendDateFormat = "2006-01-02"

// GetDailySpend handles GET /internal/v1/billing/daily_spend.
//
// Query parameters (both required):
//   - business_id — UUID of the business whose spend to look up.
//   - date        — UTC calendar day in YYYY-MM-DD form.
//
// Status codes:
//   - 200 OK — body is {"daily_spend_usd": <float>}.
//   - 400 Bad Request — missing/invalid business_id or date; body is
//     {"error":"invalid_payload","detail":"…"}.
//   - 405 Method Not Allowed — non-GET verb.
//   - 500 Internal Server Error — repository failure;
//     body is {"error":"transient"} (no internal details leaked).
func (h *InternalBillingHandler) GetDailySpend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBillingError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	q := r.URL.Query()
	businessIDStr := q.Get("business_id")
	if businessIDStr == "" {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "business_id required")
		return
	}
	bizID, err := uuid.Parse(businessIDStr)
	if err != nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "business_id must be a UUID")
		return
	}
	if bizID == uuid.Nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "business_id required")
		return
	}

	dateStr := q.Get("date")
	if dateStr == "" {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "date required")
		return
	}
	day, err := time.Parse(dailySpendDateFormat, dateStr)
	if err != nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_payload", "date must be YYYY-MM-DD")
		return
	}

	spend, err := h.billing.GetDailySpend(r.Context(), bizID, day)
	if err != nil {
		h.log.ErrorContext(r.Context(), "internal_billing: GetDailySpend repo failed",
			"error", err,
			"business_id", bizID,
		)
		writeBillingError(w, http.StatusInternalServerError, "transient", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(openapi.DailySpendResponse{DailySpendUsd: spend})
}
