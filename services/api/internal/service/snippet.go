// Package service — Phase 19 / Plan 19-03 / D-09 snippet + highlight helpers.
//
// Pure functions only: no DB, no HTTP, no slog. Token boundaries are
// rune-aware (Russian is multi-byte UTF-8) but byte offsets are returned
// for the JSON frontend. Stemming is performed by
// github.com/kljensen/snowball/russian (Russian Snowball, MIT, pure Go).
//
// Caveat (RESEARCH §1): kljensen/snowball stems «злейший» → «зл», not
// «злейш» as the original Russian Snowball algorithm dictates. Mongo's
// libstemmer drives the $text retrieval, so any divergence is at most a
// missed <mark> highlight (cosmetic), never a missed result.
package service

import (
	"strings"
	"unicode"

	"github.com/kljensen/snowball/russian"
)

// halfWindow is the snippet half-window in bytes (RESEARCH §10:
// SEARCH-03 locks ±40-120 chars; we pick 50 as the sweet spot).
const halfWindow = 50

// BuildSnippet returns a snippet of `content` centered on the first
// token whose stem matches any of `queryStems`, clamped to roughly
// [80,120] chars and aligned to word boundaries (so we don't cut a
// word in half). Returns the empty string if no token matches.
//
// Algorithm (RESEARCH §10):
//  1. Find the byte range of the first stem-match token.
//  2. Compute the desired window:
//     [matchStart - halfWindow, matchEnd + halfWindow], clamped to
//     [0, len(content)].
//  3. Expand the window outward to the nearest preceding/following
//     whitespace.
//  4. Prepend "…" if window starts > 0; append "…" if window ends
//     before len(content).
func BuildSnippet(content string, queryStems map[string]struct{}) string {
	matchStart, matchEnd := firstStemMatch(content, queryStems)
	if matchStart < 0 {
		return ""
	}
	desired := matchStart - halfWindow
	if desired < 0 {
		desired = 0
	} else {
		desired = expandLeftToBoundary(content, desired)
	}
	end := matchEnd + halfWindow
	if end > len(content) {
		end = len(content)
	} else {
		end = expandRightToBoundary(content, end)
	}
	snippet := content[desired:end]
	if desired > 0 {
		snippet = "…" + snippet
	}
	if end < len(content) {
		snippet = snippet + "…"
	}
	return snippet
}

// firstStemMatch scans content token-by-token and returns the byte
// range of the first token whose stem hits queryStems. Returns
// (-1, -1) on no match.
func firstStemMatch(content string, queryStems map[string]struct{}) (int, int) {
	runes := []rune(content)
	pos := 0
	for pos < len(runes) {
		for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
			pos++
		}
		start := pos
		for pos < len(runes) && unicode.IsLetter(runes[pos]) {
			pos++
		}
		if start == pos {
			continue
		}
		token := strings.ToLower(string(runes[start:pos]))
		if _, hit := queryStems[russian.Stem(token, false)]; hit {
			byteStart := len(string(runes[:start]))
			byteEnd := len(string(runes[:pos]))
			return byteStart, byteEnd
		}
	}
	return -1, -1
}

// expandLeftToBoundary moves `pos` leftward until the previous byte is
// a whitespace rune (so the snippet starts on a word boundary). pos==0
// is a fixed point.
func expandLeftToBoundary(s string, pos int) int {
	for pos > 0 && !unicode.IsSpace(rune(s[pos-1])) {
		pos--
	}
	return pos
}

// expandRightToBoundary moves `pos` rightward until s[pos] is whitespace
// (so the snippet ends on a word boundary). pos==len(s) is a fixed point.
func expandRightToBoundary(s string, pos int) int {
	for pos < len(s) && !unicode.IsSpace(rune(s[pos])) {
		pos++
	}
	return pos
}

// HighlightRanges returns byte ranges in `snippet` where any token's
// stem matches any query stem. Stable order, non-overlapping. Used by
// the search service to build the `marks` array sent to the frontend
// (D-09); the frontend wraps each [start, end) range in <mark>.
//
// Byte offsets — NOT rune offsets — because the frontend slices by byte
// in the response payload. Rune-vs-byte conversion uses
// `len(string(runes[:i]))` per RESEARCH §1 lines 122-125.
func HighlightRanges(snippet string, queryStems map[string]struct{}) [][2]int {
	var marks [][2]int
	runes := []rune(snippet)
	pos := 0
	for pos < len(runes) {
		for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
			pos++
		}
		start := pos
		for pos < len(runes) && unicode.IsLetter(runes[pos]) {
			pos++
		}
		if start == pos {
			continue
		}
		token := strings.ToLower(string(runes[start:pos]))
		if _, hit := queryStems[russian.Stem(token, false)]; hit {
			byteStart := len(string(runes[:start]))
			byteEnd := len(string(runes[:pos]))
			marks = append(marks, [2]int{byteStart, byteEnd})
		}
	}
	return marks
}

// QueryStems builds the deduplicated set of Russian stems used by
// BuildSnippet and HighlightRanges. Tokenizes `query` on letter / non-
// letter boundaries, lowercases each token, runs russian.Stem(_, false)
// (stemStopWords=false leaves Russian stop-words alone — Mongo's stemmer
// already filters them, so we never produce a highlight on a stop-word).
func QueryStems(query string) map[string]struct{} {
	result := make(map[string]struct{})
	runes := []rune(query)
	pos := 0
	for pos < len(runes) {
		for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
			pos++
		}
		start := pos
		for pos < len(runes) && unicode.IsLetter(runes[pos]) {
			pos++
		}
		if start == pos {
			continue
		}
		token := strings.ToLower(string(runes[start:pos]))
		result[russian.Stem(token, false)] = struct{}{}
	}
	return result
}
