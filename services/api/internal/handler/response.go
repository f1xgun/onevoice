package handler

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code and data
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data != nil && status != http.StatusNoContent {
		json.NewEncoder(w).Encode(data)
	}
}
