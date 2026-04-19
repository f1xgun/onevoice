package service

import (
	"encoding/json"
	"io"

	"github.com/google/uuid"
)

// parseUUIDSafe parses a UUID string and returns uuid.Nil on failure. Used
// when a malformed UUID must not short-circuit the caller — the fallback
// UUID.Nil triggers a predictable "not found" path downstream.
func parseUUIDSafe(s string) uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return u
}

// parseUUIDStrict returns an error if the string isn't a valid UUID. Used
// when the caller wants to distinguish "missing" from "malformed" at the
// decision layer.
func parseUUIDStrict(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

// decodeJSON is a thin wrapper so tests can override if needed. Keeps
// hitl.go from importing encoding/json directly in the hot path.
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
