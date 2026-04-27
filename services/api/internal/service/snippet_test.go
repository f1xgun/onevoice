package service

import (
	"testing"
)

// TestBuildSnippet — Plan 19-03 / Task 3 / SEARCH-03 scaffold (Wave 0).
//
// Filled out in Task 3 once snippet.go lands. Table-driven cases per
// .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §10:
//
//   - "match in middle, both ellipses" — content with the matching token
//     centered, expects «…<context>…» with both ellipses.
//   - "match near start, only trailing ellipsis" — short content under
//     120 chars, expects no ellipses (full content returned).
//   - "no match returns empty string" — stems do not match any token,
//     expects "".
//
// HighlightRanges + QueryStems unit tests will live alongside.
func TestBuildSnippet(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with BuildSnippet + firstStemMatch")
}

// TestHighlightRanges — Plan 19-03 / Task 3 / SEARCH-03 + D-09 scaffold.
// Verifies that for input «запланировать пост» with stem set
// {«запланирова»} the function returns a [start, end] byte range covering
// the matched token.
func TestHighlightRanges(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with HighlightRanges")
}

// TestQueryStems — Plan 19-03 / Task 3 / D-09 scaffold. Verifies that the
// helper produces a deduplicated set of Russian stems for a multi-word
// query, lowercased and snowball-stemmed.
func TestQueryStems(t *testing.T) {
	t.Skip("scaffold — implemented in Task 3 with QueryStems")
}
