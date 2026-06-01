package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"

	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// Re-export spec-owned error envelopes under the historic handler.* names so
// the existing test suite continues to compile against named handler types.
// Wire shape (keys + nesting) is identical to the legacy local definitions —
// both `error` and `fields` are required, matching writeJSONError /
// writeValidationError output.
type (
	ErrorResponse           = openapi.ErrorResponse
	ValidationErrorResponse = openapi.ValidationErrorResponse
)

// writeJSON writes a JSON response with the given status code and data
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data != nil && status != http.StatusNoContent {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			slog.Error("failed to encode JSON response", "error", err)
		}
	}
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

// writeJSONErrorKey writes a JSON error response whose message is resolved
// from pkg/i18n using the locale stored on the request context by the
// LocaleResolver middleware. The catalog key MUST exist in pkg/i18n/catalog_ru.go;
// fmt-style args are forwarded to i18n.Tr (so e.g. "connect.vk.invalid_token"
// accepts a single %s arg with the VK-provided detail).
//
// Use this instead of writeJSONError for any user-visible localized string.
func writeJSONErrorKey(w http.ResponseWriter, r *http.Request, status int, key string, args ...any) {
	writeJSON(w, status, ErrorResponse{Error: i18n.Tr(r.Context(), key, args...)})
}

// writeValidationError writes a validation error response with field-level
// details. Field-level messages are localized via pkg/i18n keyed by
// validation tag ("required" → "validation.required" etc.); the envelope
// "error" field uses the "validation.failed" key.
func writeValidationError(w http.ResponseWriter, r *http.Request, err error) {
	fields := make(map[string]string)
	ctx := r.Context()

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldErr := range validationErrors {
			field := fieldErr.Field()
			tag := fieldErr.Tag()

			// Map validator/v10 tag → i18n catalog key. Unknown tags fall
			// through to a generic "validation failed" message so a newly
			// introduced struct tag never crashes the handler.
			var key string
			switch tag {
			case "required":
				key = "validation.required"
			case "email":
				key = "validation.invalid_email"
			case "min":
				key = "validation.too_short"
			case "max":
				key = "validation.too_long"
			default:
				key = "validation.generic"
			}

			fields[field] = i18n.Tr(ctx, key)
		}
	} else {
		slog.Warn("validation error is not of type validator.ValidationErrors", "error", err)
	}

	writeJSON(w, http.StatusBadRequest, ValidationErrorResponse{
		Error:  i18n.Tr(ctx, "validation.failed"),
		Fields: fields,
	})
}
