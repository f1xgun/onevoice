// Package service — search snippet + highlight helpers.
//
// Pure functions only: no DB, no HTTP, no slog. Token boundaries are
// rune-aware (Cyrillic is multi-byte UTF-8) but byte offsets are
// returned for the JSON frontend.
//
// Matching model (v20): word-prefix. A query token "отзыв" matches any
// word in the content that starts with "отзыв" (case-insensitive),
// covering "отзыв", "отзывы", "отзыва", "отзывов", … in one rule.
// We deliberately do NOT use Russian Snowball — its asymmetric stem
// behavior ("отзыв" vs "отзывы") was the bug that motivated v20.
// Prefix matching gives morphological recall over inflectional suffixes
// (which is most of Russian noun/verb morphology) without the asymmetry.
package service

import (
	"strings"
	"unicode"
)

// halfWindow is the snippet half-window in bytes.
const halfWindow = 50

// BuildSnippet returns a snippet of `content` centered on the first
// token whose lowercased form starts with any of `queryPrefixes`,
// clamped to roughly [80,120] chars and aligned to word boundaries
// (so we don't cut a word in half). Returns the empty string if no
// token matches.
func BuildSnippet(content string, queryPrefixes map[string]struct{}) string {
	matchStart, matchEnd := firstPrefixMatch(content, queryPrefixes)
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
		snippet += "…"
	}
	return snippet
}

// forEachToken scans s on letter/digit boundaries and invokes fn with each
// lowercased token and its byte range. fn returning true stops the scan.
func forEachToken(s string, fn func(token string, byteStart, byteEnd int) bool) {
	runes := []rune(s)
	pos := 0
	for pos < len(runes) {
		for pos < len(runes) && !unicode.IsLetter(runes[pos]) && !unicode.IsDigit(runes[pos]) {
			pos++
		}
		ts := pos
		for pos < len(runes) && (unicode.IsLetter(runes[pos]) || unicode.IsDigit(runes[pos])) {
			pos++
		}
		if ts == pos {
			continue
		}
		token := strings.ToLower(string(runes[ts:pos]))
		if fn(token, len(string(runes[:ts])), len(string(runes[:pos]))) {
			return
		}
	}
}

// firstPrefixMatch scans content token-by-token and returns the byte
// range of the first token whose lowercased form starts with any
// prefix in queryPrefixes. Returns (-1, -1) on no match.
func firstPrefixMatch(content string, queryPrefixes map[string]struct{}) (start, end int) {
	start, end = -1, -1
	forEachToken(content, func(token string, byteStart, byteEnd int) bool {
		if anyPrefixMatches(token, queryPrefixes) {
			start, end = byteStart, byteEnd
			return true
		}
		return false
	})
	return start, end
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
// lowercased form starts with one of `queryPrefixes`. Stable order,
// non-overlapping. The search service builds the `marks` array from these
// ranges; the frontend wraps each [start, end) range in <mark>.
//
// Byte offsets — NOT rune offsets — because the frontend slices by byte
// in the response payload.
func HighlightRanges(snippet string, queryPrefixes map[string]struct{}) [][2]int {
	var marks [][2]int
	forEachToken(snippet, func(token string, byteStart, byteEnd int) bool {
		if anyPrefixMatches(token, queryPrefixes) {
			marks = append(marks, [2]int{byteStart, byteEnd})
		}
		return false
	})
	return marks
}

// anyPrefixMatches reports whether token starts with any prefix in the
// set. Both token and prefixes are expected to be lowercased.
func anyPrefixMatches(token string, prefixes map[string]struct{}) bool {
	for p := range prefixes {
		if p != "" && strings.HasPrefix(token, p) {
			return true
		}
	}
	return false
}

// QueryPrefixes builds the deduplicated set of lowercased query tokens
// shared by BuildSnippet, HighlightRanges, and the repository regex
// builders. Tokenizes `query` on letter/digit boundaries; punctuation
// and whitespace become separators.
func QueryPrefixes(query string) map[string]struct{} {
	result := make(map[string]struct{})
	forEachToken(query, func(token string, _, _ int) bool {
		result[token] = struct{}{}
		return false
	})
	return result
}
