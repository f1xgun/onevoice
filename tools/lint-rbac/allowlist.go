package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// AllowlistEntry mirrors the CONTEXT D-13 schema.
type AllowlistEntry struct {
	Route   string `json:"route"`
	Reason  string `json:"reason"`
	Expires string `json:"expires"` // YYYY-MM-DD
}

// ParseAllowlist reads .rbac-migration-allowlist (or returns [] on missing file) and
// fails on any past-date entry per CONTEXT D-13.
func ParseAllowlist(path string, now time.Time) ([]AllowlistEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open allowlist %s: %w", path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read allowlist: %w", err)
	}

	if len(raw) == 0 {
		return nil, nil
	}

	var entries []AllowlistEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse allowlist json: %w", err)
	}

	for i, e := range entries {
		if e.Route == "" {
			return nil, fmt.Errorf("allowlist entry %d: route is empty", i)
		}
		exp, err := time.Parse("2006-01-02", e.Expires)
		if err != nil {
			return nil, fmt.Errorf("allowlist entry %s: invalid expires %q: %w", e.Route, e.Expires, err)
		}
		if exp.Before(now) {
			return nil, fmt.Errorf("allowlist entry %s expired on %s — remove or extend", e.Route, e.Expires)
		}
	}

	return entries, nil
}

// IsAllowed reports whether route matches any allowlist entry. Phase 2
// ships exact-string matching (no chi pattern compilation); the
// allowlist is empty so the matcher's behaviour is moot until v2.1.
func IsAllowed(route string, entries []AllowlistEntry) bool {
	for _, e := range entries {
		if e.Route == route {
			return true
		}
	}
	return false
}
