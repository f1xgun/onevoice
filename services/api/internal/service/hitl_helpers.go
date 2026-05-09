package service

import (
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
