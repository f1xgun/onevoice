package handler

import (
	"encoding/json"
	"net/http"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// InternalToolsHandler serves the cluster-internal registry snapshot consumed
// by the API service's POLICY-07 startup sweep.
//
// Access considerations (threat model T-16-03-04): this endpoint leaks tool
// NAMES and FLOORS only — no secrets, no schemas, no parameters. The
// information disclosure risk was accepted in planning because:
//
//  1. The endpoint binds to the orchestrator's regular port (8090) which is
//     not exposed publicly in docker-compose.yml (the frontend talks to the
//     API service on 8080; the orchestrator is cluster-internal).
//  2. An attacker with network access to orchestrator:8090 already has a
//     direct chat proxy surface — the tool list is not a meaningful additional
//     disclosure.
//
// Path convention: `/internal/*` is reserved for cluster-internal endpoints.
// External reverse proxies must drop requests matching this prefix.
type InternalToolsHandler struct {
	Registry *tools.Registry
}

// NewInternalToolsHandler constructs the handler. Caller must supply a
// non-nil Registry; the handler does not defend against nil because the
// orchestrator would already be non-functional in that state.
func NewInternalToolsHandler(reg *tools.Registry) *InternalToolsHandler {
	return &InternalToolsHandler{Registry: reg}
}

// Names responds with `{"names": ["tool1", "tool2", ...]}` — the canonical
// input to hitlvalidation.ValidateApprovalSettings at API boot.
//
// The response is JSON rather than newline-delimited so the API service can
// decode it with a single json.Decoder pass; it is small enough (<1KB per 100
// tools) that streaming is unnecessary. The endpoint is idempotent and safe
// to call repeatedly (e.g., a retry after a 5s backoff in the API service).
func (h *InternalToolsHandler) Names(w http.ResponseWriter, r *http.Request) {
	entries := h.Registry.AllEntries()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"names": names})
}
