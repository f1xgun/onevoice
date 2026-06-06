package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedNow is used as the reference "now" for all tests to ensure determinism.
var fixedNow = time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)

func writeAllowlist(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".rbac-migration-allowlist")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeAllowlist: %v", err)
	}
	return path
}

// Test 1: ParseAllowlist on empty [] returns ([], nil).
func TestParseAllowlist_Empty(t *testing.T) {
	dir := t.TempDir()
	path := writeAllowlist(t, dir, "[]")
	entries, err := ParseAllowlist(path, fixedNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// Test 2: ParseAllowlist on a single non-expired entry returns 1-element slice.
func TestParseAllowlist_SingleNonExpired(t *testing.T) {
	dir := t.TempDir()
	path := writeAllowlist(t, dir, `[{"route":"/businesses/{id}/legacy","reason":"test","expires":"2027-01-01"}]`)
	entries, err := ParseAllowlist(path, fixedNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Route != "/businesses/{id}/legacy" {
		t.Errorf("unexpected route %q", entries[0].Route)
	}
}

// Test 3: ParseAllowlist on a past-expires entry returns an error.
func TestParseAllowlist_ExpiredEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeAllowlist(t, dir, `[{"route":"/businesses/{id}/old","reason":"old","expires":"2020-01-01"}]`)
	_, err := ParseAllowlist(path, fixedNow)
	if err == nil {
		t.Fatal("expected error for expired entry, got nil")
	}
	errStr := err.Error()
	if len(errStr) == 0 {
		t.Fatal("error message is empty")
	}
}

// Test 4: IsAllowed returns true when route matches an entry.
func TestIsAllowed_Match(t *testing.T) {
	entries := []AllowlistEntry{
		{Route: "/businesses/{id}/foo", Reason: "test", Expires: "2027-01-01"},
	}
	if !IsAllowed("/businesses/{id}/foo", entries) {
		t.Error("expected IsAllowed to return true")
	}
}

// Test 5: IsAllowed returns false when route does not match.
func TestIsAllowed_NoMatch(t *testing.T) {
	entries := []AllowlistEntry{
		{Route: "/businesses/{id}/foo", Reason: "test", Expires: "2027-01-01"},
	}
	if IsAllowed("/businesses/{id}/bar", entries) {
		t.Error("expected IsAllowed to return false")
	}
}

// Test 6: IsAllowed uses exact-string matching (no chi-pattern wildcard expansion).
func TestIsAllowed_ExactMatch(t *testing.T) {
	entries := []AllowlistEntry{
		{Route: "/businesses/{id}/foo/{userId}", Reason: "test", Expires: "2027-01-01"},
	}
	if !IsAllowed("/businesses/{id}/foo/{userId}", entries) {
		t.Error("expected exact match to return true")
	}
	if IsAllowed("/businesses/123/foo/456", entries) {
		t.Error("real URL should not match placeholder pattern in v2.0")
	}
}

// Test 7: ParseAllowlist on missing file returns empty slice + nil error.
func TestParseAllowlist_MissingFile(t *testing.T) {
	entries, err := ParseAllowlist("/nonexistent/path/.rbac-migration-allowlist", fixedNow)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if entries != nil && len(entries) != 0 {
		t.Fatalf("expected nil/empty slice for missing file, got %v", entries)
	}
}

// Test 8: ParseAllowlist on malformed JSON returns wrapped error.
func TestParseAllowlist_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeAllowlist(t, dir, `[not valid json`)
	_, err := ParseAllowlist(path, fixedNow)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}
