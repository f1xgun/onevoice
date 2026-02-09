package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
)

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
		json.NewEncoder(w).Encode(data)
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
	}

	writeJSON(w, http.StatusBadRequest, ValidationErrorResponse{
		Error:  "validation failed",
		Fields: fields,
	})
}
