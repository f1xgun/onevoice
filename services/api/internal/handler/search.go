package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// searchBusinessLookup is the narrow interface SearchHandler needs from
// the business service. Lives here (not in business.go's wider
// BusinessService) so the search handler is decoupled from POLICY-05's
// tool-approvals path. Implemented by *service.BusinessService through
// Go's structural typing.
type searchBusinessLookup interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
}

// SearchHandler — Phase 19 / Plan 19-03 / Task 4. Implements
// GET /api/v1/search?q=&project_id=&limit=20.
//
// Maps the three search sentinel errors:
//
//   - domain.ErrSearchIndexNotReady → 503 + Retry-After: 5 header
//     (T-19-INDEX-503 mitigation; also satisfies SEARCH-06).
//   - domain.ErrInvalidScope        → 500 (defense-in-depth; should not
//     be reachable because we resolve scope server-side, but the repo
//     guards anyway).
//   - other errors                  → 500 with metadata-only log line.
//
// Logging contract (SEARCH-07 / T-19-LOG-LEAK): every log line carries
// {user_id, business_id, query_length}. NEVER the literal query text.
// NO `"query"` slog field key.
type SearchHandler struct {
	searcher       *service.Searcher
	businessLookup searchBusinessLookup
}

// NewSearchHandler — both deps are mandatory. Pattern parallels
// NewConversationHandler (returns *Handler, error). Nil deps return a
// nil handler + descriptive error so cmd/main.go fails at boot.
func NewSearchHandler(searcher *service.Searcher, biz searchBusinessLookup) (*SearchHandler, error) {
	if searcher == nil {
		return nil, fmt.Errorf("NewSearchHandler: searcher cannot be nil")
	}
	if biz == nil {
		return nil, fmt.Errorf("NewSearchHandler: businessLookup cannot be nil")
	}
	return &SearchHandler{searcher: searcher, businessLookup: biz}, nil
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
//   - 401 on missing/invalid bearer
//   - 503 + Retry-After: 5 on cold-boot before indexes are ready
//   - 500 on unexpected error or scope-guard surfacing (server-side bug)
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSONError(w, http.StatusBadRequest, "query too short")
		return
	}

	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	biz, err := h.businessLookup.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusUnauthorized, "no business")
			return
		}
		slog.ErrorContext(r.Context(), "search: failed to resolve business",
			"user_id", userID.String(),
			"query_length", len(q),
			"error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
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

	results, err := h.searcher.Search(r.Context(), biz.ID.String(), userID.String(), q, projectID, limit)
	if errors.Is(err, domain.ErrSearchIndexNotReady) {
		// SEARCH-06 / T-19-INDEX-503 mitigation. The atomic.Bool flag
		// in service.Searcher flips to true after EnsureSearchIndexes
		// returns nil at startup; until then we surface a retryable
		// 503 with Retry-After: 5.
		w.Header().Set("Retry-After", "5")
		writeJSONError(w, http.StatusServiceUnavailable, "search index initializing")
		return
	}
	if errors.Is(err, domain.ErrInvalidScope) {
		// Should not be reachable: handler resolves businessID/userID
		// server-side. If we surface ErrInvalidScope, a future caller
		// must have introduced an empty-scope path — log loudly with
		// metadata-only fields (SEARCH-07).
		slog.ErrorContext(r.Context(), "search: invalid scope reached handler",
			"user_id", userID.String(),
			"business_id", biz.ID.String(),
			"query_length", len(q))
		writeJSONError(w, http.StatusInternalServerError, "scope error")
		return
	}
	if err != nil {
		// Metadata-only log — NEVER the query text (SEARCH-07 /
		// T-19-LOG-LEAK).
		slog.ErrorContext(r.Context(), "search failed",
			"user_id", userID.String(),
			"business_id", biz.ID.String(),
			"query_length", len(q),
			"error", err)
		writeJSONError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, results)
}
