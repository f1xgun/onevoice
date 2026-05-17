package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"

	"github.com/f1xgun/onevoice/pkg/i18n"
)

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// extractVoiceTone reads the voiceTone tag list out of business.Settings.
// Tags persist as []string under settings.voiceTone (see UpdateVoiceTone).
// JSON round-trips via Postgres come back as []interface{}, so handle both.
// Returns nil when nothing is configured — callers should treat nil/empty as
// "no tone preference set".
func extractVoiceTone(settings map[string]interface{}) []string {
	if settings == nil {
		return nil
	}
	raw, ok := settings["voiceTone"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// ErrorResponse represents a JSON error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// ValidationErrorResponse represents a validation error response with field details
type ValidationErrorResponse struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields"`
}

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
// Use this instead of writeJSONError for any user-visible string that was
// previously a Russian literal in the handler.
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
