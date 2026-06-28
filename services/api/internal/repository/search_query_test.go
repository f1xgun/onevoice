package repository

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenizeQuery_DropsRegexSpecialChars — the tokenizer keeps only
// letters and digits; punctuation (incl. regex specials like ".", "+",
// "[", "$") becomes a token boundary. This is the first line of defense
// against regex injection: by the time wordPrefixRegex receives a token,
// it can never contain a metacharacter. (regexp.QuoteMeta is the second
// line, in case a future tokenizer change widens the alphabet.)
func TestTokenizeQuery_DropsRegexSpecialChars(t *testing.T) {
	got := tokenizeQuery("v1.3+test [OK] $price")
	assert.ElementsMatch(t, []string{"v1", "3", "test", "ok", "price"}, got,
		"punctuation must split tokens; uppercase must be lowercased")
}

// TestTokenizeQuery_DeduplicatesAndPreservesOrder — repeated lowercase
// tokens collapse to a single entry; first-seen order is stable so
// regex builders can rely on a deterministic AND order in $match.
func TestTokenizeQuery_DeduplicatesAndPreservesOrder(t *testing.T) {
	got := tokenizeQuery("Отзыв ОТЗЫВ отзыв проверь Отзыв")
	assert.Equal(t, []string{"отзыв", "проверь"}, got,
		"dedup must keep first-seen order, all tokens lowercased")
}

// TestTokenizeQuery_HandlesEdges — empty / whitespace-only / punctuation-
// only inputs produce no tokens (callers short-circuit to empty results).
func TestTokenizeQuery_HandlesEdges(t *testing.T) {
	for _, q := range []string{"", "   ", ".,!?"} {
		assert.Empty(t, tokenizeQuery(q), "input %q must produce no tokens", q)
	}
}

// TestTokenizeQuery_CapsTokenCount — each distinct token becomes one
// unanchored regex clause in a Mongo $and, so an unbounded token count is a
// DoS amplification vector. The tokenizer must cap the result at
// maxQueryTokens distinct tokens regardless of how many the query contains.
func TestTokenizeQuery_CapsTokenCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxQueryTokens*5; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "t%d", i)
	}

	got := tokenizeQuery(b.String())
	assert.Len(t, got, maxQueryTokens,
		"token count must be capped at maxQueryTokens")
	assert.Equal(t, "t0", got[0], "cap must keep first-seen tokens, in order")
}

// TestWordPrefixRegex_LiteralEscaping — wordPrefixRegex must escape its
// input via regexp.QuoteMeta. Even though tokenizeQuery already strips
// regex metacharacters, the escape is the contract: a hostile or
// inflated future input must NEVER produce a pattern that backreferences,
// alternates, or quantifies on attacker-controlled bytes.
func TestWordPrefixRegex_LiteralEscaping(t *testing.T) {
	pat := wordPrefixRegex("a.b+c[")
	re, err := regexp.Compile(pat)
	require.NoError(t, err, "escaped pattern must always compile")

	assert.True(t, re.MatchString("xx a.b+c[ yy"), "literal substring must match")
	assert.False(t, re.MatchString("xx aXbXcX yy"),
		"meta-as-meta would have matched aXbXcX; escape failed")
}

// TestWordPrefixRegex_WordBoundary — the pattern matches only at
// (start | non-word) boundaries. "отзыв" must match a standalone
// "отзыв" or any inflected form ("отзывы", "отзывов") — but NEVER
// the inside of a compound word like "переотзыв". This is the load-
// bearing property that distinguishes prefix matching from naive
// substring matching.
func TestWordPrefixRegex_WordBoundary(t *testing.T) {
	pat := wordPrefixRegex("отзыв")
	re, err := regexp.Compile("(?i)" + pat)
	require.NoError(t, err)

	for _, s := range []string{
		"отзыв",
		"проверь отзыв",
		"проверь отзывы",
		"проверь отзывов",
		"(отзыв)",
		"Отзыв в начале строки",
	} {
		assert.True(t, re.MatchString(s), "expected match for %q", s)
	}

	for _, s := range []string{
		"переотзыв",
		"антиотзыв",
		"foo-переотзыв-bar",
	} {
		assert.False(t, re.MatchString(s), "must NOT match %q (within-word)", s)
	}
}
