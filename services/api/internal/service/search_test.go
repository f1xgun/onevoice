package service

import (
	"testing"
)

// TestSearcher — Plan 19-03 / Task 3 / SEARCH-02 + SEARCH-06 + SEARCH-07
// scaffold (Wave 0).
//
// Filled out in Task 3 once Searcher + ErrInvalidScope/ErrSearchIndexNotReady
// guards land. Subtests:
//
//   - empty businessID returns domain.ErrInvalidScope (no repo calls)
//   - empty userID returns domain.ErrInvalidScope (no repo calls)
//   - indexReady = false returns domain.ErrSearchIndexNotReady
//   - successful path calls SearchTitles + ScopedConversationIDs +
//     SearchByConversationIDs and merges via mergeAndRank with weights
//     (titleW=20, contentW=10)
//   - log capture (titler_test.go captureLogs pattern): every log line
//     carries `query_length` and never the literal query bytes — SEARCH-07
//     T-19-LOG-LEAK mitigation
func TestSearcher_RejectsEmptyScope(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with Searcher + ErrInvalidScope guard")
}

func TestSearcher_RejectsBeforeIndexReady(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with Searcher.indexReady atomic.Bool")
}

func TestSearcher_HappyPath(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with Searcher.Search orchestration")
}

func TestSearcher_LogShape_NoQueryText(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with captureLogs + SEARCH-07 assertion")
}
