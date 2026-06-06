package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// businessContext extracts the per-request BusinessContext, writing a 500
// "internal_server_error" (and, when op is non-empty, the standard
// middleware-misconfiguration log line) and returning ok=false when absent.
func businessContext(w http.ResponseWriter, r *http.Request, op string) (authz.BusinessContext, bool) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		if op != "" {
			slog.ErrorContext(r.Context(), op+": no BusinessContext in ctx — middleware misconfiguration")
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return authz.BusinessContext{}, false
	}
	return bc, true
}

// requireBusiness folds the BusinessContext extraction and the adjacent
// permission check: 500 when no context, 403 "forbidden" when perm is absent.
func requireBusiness(w http.ResponseWriter, r *http.Request, op string, perm authz.Permission) (authz.BusinessContext, bool) {
	bc, ok := businessContext(w, r, op)
	if !ok {
		return authz.BusinessContext{}, false
	}
	if !authz.Can(r.Context(), perm) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return authz.BusinessContext{}, false
	}
	return bc, true
}

// requireUserID extracts the authenticated user ID, writing 401 "unauthorized"
// and returning ok=false when absent.
func requireUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return uuid.Nil, false
	}
	return userID, true
}

// decodeAndValidate decodes the JSON request body into T then runs the
// package validator, writing the given decodeErrCode (400) on a decode failure
// and a field-level validation error on a struct-validation failure.
func decodeAndValidate[T any](w http.ResponseWriter, r *http.Request, decodeErrCode string) (T, bool) {
	var req T
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, decodeErrCode)
		return req, false
	}
	if err := validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return req, false
	}
	return req, true
}

// parseLimitOffset reads ?limit and ?offset, clamping limit into
// (0, maxLimit] and offset into [0, ∞); malformed or absent values keep the
// supplied defaultLimit / offset 0.
//
//nolint:unparam // defaultLimit/maxLimit are per-resource constants that happen to share values today; folding them would couple unrelated endpoints.
func parseLimitOffset(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > maxLimit {
				limit = maxLimit
			}
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

// parseUUIDParam parses a chi URL param as a UUID, writing 400 errCode and
// returning ok=false on a malformed value.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, paramName, errCode string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, paramName))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, errCode)
		return uuid.Nil, false
	}
	return id, true
}
