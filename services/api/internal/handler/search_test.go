package handler

import (
	"testing"
)

// TestSearchHandler — Plan 19-03 / Task 4 / SEARCH-02 + SEARCH-06 +
// SEARCH-07 scaffold (Wave 0).
//
// Filled out in Task 4 once SearchHandler lands. Subtests:
//
//   - q="" → 400 (empty query rejected by validator path)
//   - q="a" (len < 2) → 400 (D-13 min-length 2 chars)
//   - missing bearer → 401 (middleware.GetUserID returns error)
//   - searchReady=false → 503 + Retry-After: 5 header (T-19-INDEX-503)
//   - successful search → 200 + JSON []SearchResult body
//   - logs captured during a successful search MUST contain `query_length`
//     and MUST NOT contain the literal query bytes (T-19-LOG-LEAK)
func TestSearchHandler_400OnShortQuery(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with SearchHandler.Search 400 path")
}

func TestSearchHandler_401OnMissingBearer(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with SearchHandler.Search 401 path")
}

func TestSearchHandler_503BeforeReady(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with Retry-After: 5 mapping")
}

func TestSearchHandler_LogShape(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with captureLogs + SEARCH-07 metadata-only")
}
