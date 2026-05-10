package authz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// UserIDExtractor is the seam the middleware uses to retrieve the
// authenticated userID from ctx. The default impl reads
// services/api/internal/middleware UserIDKey ctx-value, but pkg/authz
// cannot import services/api — so we accept the extractor as a parameter
// and let services/api/cmd/main.go inject the production extractor.
type UserIDExtractor func(ctx context.Context) (uuid.UUID, error)

// RequireBusinessAccess returns chi-style middleware that:
//  1. Parses chi.URLParam(r, "id") as a UUID -> 400 on invalid.
//  2. Reads userID via the extractor -> 401 on missing.
//  3. Looks up membership via cache (loader fallthrough) -> 404 on
//     not-found (AUTHZ-05 — never 403, never leak business existence).
//  4. 403 on status == "suspended".
//  5. Loads role permissions via cache.
//  6. Injects BusinessContext into ctx and calls next.ServeHTTP.
//
// 401 from upstream auth.Auth fires BEFORE this middleware in the chi
// chain. Inputs to this middleware always carry an authenticated userID.
func RequireBusinessAccess(cache *Cache, extractUserID UserIDExtractor) func(http.Handler) http.Handler {
	if cache == nil {
		panic("authz.RequireBusinessAccess: cache cannot be nil")
	}
	if extractUserID == nil {
		panic("authz.RequireBusinessAccess: extractUserID cannot be nil")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawID := chi.URLParam(r, "id")
			businessID, err := uuid.Parse(rawID)
			if err != nil {
				writeAuthzError(w, http.StatusBadRequest, "invalid_business_id")
				return
			}

			userID, err := extractUserID(r.Context())
			if err != nil {
				writeAuthzError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			member, err := cache.GetMembership(r.Context(), businessID, userID)
			switch {
			case err == nil:
				// ok — proceed
			case errors.Is(err, domain.ErrMembershipNotFound):
				// 404, NOT 403 — AUTHZ-05 contract.
				writeAuthzError(w, http.StatusNotFound, "not_found")
				return
			default:
				slog.ErrorContext(r.Context(), "authz: load membership failed",
					"error", err,
					"business_id", businessID,
					"user_id", userID,
				)
				writeAuthzError(w, http.StatusInternalServerError, "internal_server_error")
				return
			}

			if member.Status == "suspended" {
				writeAuthzError(w, http.StatusForbidden, "forbidden_suspended")
				return
			}

			role, err := cache.GetRole(r.Context(), member.RoleID)
			if err != nil {
				slog.ErrorContext(r.Context(), "authz: load role failed",
					"error", err,
					"business_id", businessID,
					"user_id", userID,
					"role_id", member.RoleID,
				)
				writeAuthzError(w, http.StatusInternalServerError, "internal_server_error")
				return
			}

			// WR-01: defensive copy. role.Permissions points at the slice
			// stored inside the cache entry; if any future code path mutates
			// bc.Permissions (append, sort, in-place edit) it would corrupt
			// the shared cache entry — and the bug would only fire on cache
			// HITS, making it easy to miss in unit tests. One alloc per
			// request (~100 bytes) is a negligible cost for the safety.
			perms := make([]Permission, len(role.Permissions))
			copy(perms, role.Permissions)
			bc := BusinessContext{
				BusinessID:  businessID,
				UserID:      userID,
				RoleID:      member.RoleID,
				Permissions: perms,
			}
			next.ServeHTTP(w, r.WithContext(WithBusinessContext(r.Context(), bc)))
		})
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeAuthzError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: code})
}
