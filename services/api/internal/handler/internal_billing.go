package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// maxBillingPayloadBytes caps the request body size on the internal billing
// endpoint. A populated llm.UsageLog round-trips to well under 1 KB; 64 KB is
// a generous DoS guard (T-25a-24).
const maxBillingPayloadBytes = 64 * 1024

// BillingService is the narrow write-only surface the internal billing
// handler depends on. Matches pkg/llm.Writer; declared locally so the handler
// package does not import pkg/llm just for the interface alias.
type BillingService interface {
	LogUsage(ctx context.Context, log *llm.UsageLog) error
}

// InternalBillingHandler serves POST /internal/v1/billing/usage_logs on the
// mTLS-protected :8443 listener. It is reached only via the orchestrator's
// pkg/billingclient (plan 25a-03), gated by router-level
// middleware.RequireServiceIdentity.
type InternalBillingHandler struct {
	billing BillingService
	log     *slog.Logger
}

// NewInternalBillingHandler wires the handler with a backing BillingService
// (production: services/api/internal/repository.billingRepository from plan
// 25a-02). A nil logger falls back to slog.Default().
func NewInternalBillingHandler(b BillingService, log *slog.Logger) *InternalBillingHandler {
	if log == nil {
		log = slog.Default()
	}
	return &InternalBillingHandler{billing: b, log: log}
}

// billingErrorBody is the JSON envelope returned on 4xx and 5xx. The shape
// matches pkg/billingclient's error-classification matrix: `error` of
// "invalid_payload" → ErrInvalidPayload; "transient" → ErrTransient.
type billingErrorBody struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

// LogUsage handles POST /internal/v1/billing/usage_logs.
//
// Status codes:
//   - 204 No Content — success.
//   - 400 Bad Request — invalid JSON, missing required fields, or negative
//     token counts. Body is {"error":"invalid_payload","detail":"…"}.
//   - 405 Method Not Allowed — non-POST verb.
//   - 500 Internal Server Error — repository failure. Body is
//     {"error":"transient"} (no internal details leaked; T-25a-25).
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

// writeBillingError emits the {"error":..,"detail":..} envelope.
func writeBillingError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(billingErrorBody{Error: code, Detail: detail})
}
