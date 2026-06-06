package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// SearchHandler implements
// GET /api/v1/search?q=&project_id=&limit=20.
//
// Maps the three search sentinel errors:
//
//   - domain.ErrSearchIndexNotReady → 503 + Retry-After: 5 header.
//   - domain.ErrInvalidScope        → 500 (defense-in-depth; should not
//     be reachable because we resolve scope server-side, but the repo
//     guards anyway).
//   - other errors                  → 500 with metadata-only log line.
//
// Logging contract: every log line carries
// {user_id, business_id, query_length}. NEVER the literal query text.
// NO `"query"` slog field key.
type SearchHandler struct {
	searcher *service.Searcher
}

// NewSearchHandler — searcher dep is mandatory. Pattern parallels
// NewConversationHandler (returns *Handler, error). Nil dep returns a
// nil handler + descriptive error so cmd/main.go fails at boot.
func NewSearchHandler(searcher *service.Searcher) (*SearchHandler, error) {
	if searcher == nil {
		return nil, fmt.Errorf("NewSearchHandler: searcher cannot be nil")
	}
	return &SearchHandler{searcher: searcher}, nil
}

// Search handles GET /api/v1/search.
//
// Query params:
//   - q (required, length ≥ 2)  — search query
//   - project_id (optional)     — UUID-shaped scope filter
//   - limit (optional, ≤ 50)    — max rows (default 20)
//
// Response:
//   - 200 OK + JSON [SearchResult ...] on success
//   - 400 on missing/short q
//   - 403 on missing permission
//   - 500 on missing BusinessContext (middleware misconfiguration)
//   - 503 + Retry-After: 5 on cold-boot before indexes are ready
//   - 500 on unexpected error or scope-guard surfacing (server-side bug)
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSONError(w, http.StatusBadRequest, "query too short")
		return
	}

	bc, ok := requireBusiness(w, r, "Search", authz.PermContentRead)
	if !ok {
		return
	}

	var projectID *string
	if p := strings.TrimSpace(r.URL.Query().Get("project_id")); p != "" {
		projectID = &p
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, parseErr := strconv.Atoi(l); parseErr == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	results, err := h.searcher.Search(r.Context(), bc.BusinessID.String(), bc.UserID.String(), q, projectID, limit)
	if errors.Is(err, domain.ErrSearchIndexNotReady) {
		w.Header().Set("Retry-After", "5")
		writeJSONError(w, http.StatusServiceUnavailable, "search index initializing")
		return
	}
	if errors.Is(err, domain.ErrInvalidScope) {
		slog.ErrorContext(r.Context(), "search: invalid scope reached handler",
			"user_id", bc.UserID.String(),
			"business_id", bc.BusinessID.String(),
			"query_length", len(q))
		writeJSONError(w, http.StatusInternalServerError, "scope error")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "search failed",
			"user_id", bc.UserID.String(),
			"business_id", bc.BusinessID.String(),
			"query_length", len(q),
			"error", err)
		writeJSONError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, results)
}
