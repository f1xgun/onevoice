package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"
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

// writeValidationError writes a validation error response with field-level details
func writeValidationError(w http.ResponseWriter, err error) {
	fields := make(map[string]string)

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldErr := range validationErrors {
			field := fieldErr.Field()
			tag := fieldErr.Tag()

			// Create human-readable error message based on validation tag
			var message string
			switch tag {
			case "required":
				message = "field is required"
			case "email":
				message = "invalid email format"
			case "min":
				message = "value is too short"
			case "max":
				message = "value is too long"
			default:
				message = "validation failed"
			}

			fields[field] = message
		}
	} else {
		slog.Warn("validation error is not of type validator.ValidationErrors", "error", err)
	}

	writeJSON(w, http.StatusBadRequest, ValidationErrorResponse{
		Error:  "validation failed",
		Fields: fields,
	})
}
