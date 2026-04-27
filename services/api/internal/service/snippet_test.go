package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildSnippet — Plan 19-03 / Task 3 / SEARCH-03. Table-driven cases
// from RESEARCH §10. The snowball stem of «запланировать» / «запланируем»
// / «Запланировать» is «запланирова» (3rd-conjugation root) — used as
// the queryStems fixture.
func TestBuildSnippet(t *testing.T) {
	stem := map[string]struct{}{"запланирова": {}}

	cases := []struct {
		name        string
		content     string
		stems       map[string]struct{}
		wantContain string // assert containsAll of these substrings
		wantEqual   string // when non-empty, assert exact equality
	}{
		{
			name:        "match in middle, both ellipses",
			content:     "Доброе утро. Я хочу запланировать пост в Telegram на пятницу вечером, чтобы охватить аудиторию выходных.",
			stems:       stem,
			wantContain: "запланировать пост",
		},
		{
			name:      "match near start, only trailing ellipsis when content is short",
			content:   "Запланировать пост на завтра.",
			stems:     stem,
			wantEqual: "Запланировать пост на завтра.",
		},
		{
			name:      "no match returns empty string",
			content:   "Совсем другой текст без ключевых слов.",
			stems:     stem,
			wantEqual: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildSnippet(tc.content, tc.stems)
			if tc.wantEqual != "" || tc.name == "no match returns empty string" {
				assert.Equal(t, tc.wantEqual, got)
				return
			}
			require.NotEmpty(t, got)
			assert.Contains(t, got, tc.wantContain)
		})
	}
}

// TestBuildSnippet_AddsLeadingEllipsisWhenWindowStartsAfterContentStart
// — when the matched token sits well past the start of the content, the
// snippet must begin with «…».
func TestBuildSnippet_AddsLeadingEllipsisWhenWindowStartsAfterContentStart(t *testing.T) {
	content := "Это длинное вступление к разговору, по итогу которого пришли к мысли запланировать пост на завтра вечером для нашей аудитории."
	stems := map[string]struct{}{"запланирова": {}}
	got := BuildSnippet(content, stems)
	require.NotEmpty(t, got)
	assert.True(t, strings.HasPrefix(got, "…"), "expected leading ellipsis, got %q", got)
}

// TestHighlightRanges_FindsAllMatchingTokens — Plan 19-03 / Task 3 / D-09.
// For input «запланировать пост в Telegram» with stems
// {«запланирова», «пост»}, two byte ranges must come back covering each
// matching token. The byte offsets must be valid slices of the input.
func TestHighlightRanges_FindsAllMatchingTokens(t *testing.T) {
	stems := map[string]struct{}{
		"запланирова": {},
		"пост":        {},
	}
	snippet := "запланировать пост в Telegram"

	marks := HighlightRanges(snippet, stems)
	require.GreaterOrEqual(t, len(marks), 2,
		"expected at least 2 marks (запланировать + пост), got %d: %v", len(marks), marks)

	// Validate every range slices the original snippet cleanly.
	for _, m := range marks {
		require.GreaterOrEqual(t, m[0], 0)
		require.LessOrEqual(t, m[1], len(snippet))
		require.Less(t, m[0], m[1])
	}

	// First mark must cover «запланировать» (starts at byte 0).
	assert.Equal(t, 0, marks[0][0], "first mark should cover the leading word")
	assert.Equal(t, "запланировать", snippet[marks[0][0]:marks[0][1]])
}

// TestHighlightRanges_EmptyOnNoMatch — no token's stem is in the set →
// no marks.
func TestHighlightRanges_EmptyOnNoMatch(t *testing.T) {
	stems := map[string]struct{}{"запланирова": {}}
	marks := HighlightRanges("ничего не совпадает здесь", stems)
	assert.Empty(t, marks)
}

// TestQueryStems_DeduplicatesIdenticalStems — Plan 19-03 / Task 3 / D-09.
// Repeating the SAME word twice must return a single-element set
// (set semantics: dedup is by stem identity).
//
// We deliberately do NOT assert that two different inflections collapse
// to the same stem — kljensen/snowball produces different stems for
// some inflected forms (RESEARCH §1 caveat). The Mongo $text stemmer
// drives retrieval; QueryStems only powers the cosmetic <mark>
// highlight via HighlightRanges, so divergence is acceptable.
func TestQueryStems_DeduplicatesIdenticalStems(t *testing.T) {
	stems := QueryStems("инвойс инвойс инвойс")
	assert.Len(t, stems, 1, "the same word repeated stems to the same single root")
	for s := range stems {
		assert.NotEmpty(t, s)
	}
}

// TestQueryStems_HandlesPunctuationAndCase — punctuation, mixed case,
// trailing whitespace must not produce extra stems or empty entries.
func TestQueryStems_HandlesPunctuationAndCase(t *testing.T) {
	stems := QueryStems("ЗАПЛАНИРОВАТЬ! пост, в Telegram?")
	assert.GreaterOrEqual(t, len(stems), 2)
	for s := range stems {
		assert.NotEmpty(t, s, "stems map must never contain an empty key")
	}
}
