package repository

import (
	"regexp"
	"strings"
	"unicode"
)

// tokenizeQuery splits a search query into deduplicated lowercase tokens
// on letter/digit boundaries. Punctuation and whitespace become
// separators. Returns an empty slice for empty / whitespace-only input.
//
// Mirrors service.QueryPrefixes (the snippet/highlight side); both must
// agree on token boundaries so backend hits and frontend marks line up.
func tokenizeQuery(query string) []string {
	seen := make(map[string]struct{})
	var out []string
	runes := []rune(query)
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
		t := strings.ToLower(string(runes[ts:pos]))
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// wordPrefixRegex builds a Mongo-friendly regex pattern that matches
// any word in the target string whose lowercased form starts with
// `token`. Word boundary = the rune just before the match must NOT be
// a letter or digit (or it must be the start of the string).
//
// We avoid PCRE lookbehind for portability across Mongo / driver
// versions: we use a non-capturing group `(?:^|[non-word])` and rely
// on the regex engine to anchor at any qualifying position.
//
// `token` is escaped via regexp.QuoteMeta so a query like "v1.3" or
// "[OK]" can never inject metacharacters into the pattern.
//
// Case-insensitivity is supplied by the caller via the `i` regex
// option in the BSON query (NOT inline in the pattern); keeping the
// flag external lets the Mongo regex driver short-circuit to its
// case-fold pipeline.
func wordPrefixRegex(token string) string {
	if token == "" {
		// Pattern that matches nothing (empty token shouldn't reach here,
		// but stay defensive — never emit a pattern that matches every
		// document).
		return `\b\B`
	}
	return `(?:^|[^\p{L}\p{N}_])` + regexp.QuoteMeta(token)
}
