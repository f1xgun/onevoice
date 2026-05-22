package i18n_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/i18n"
)

func TestLocaleFromContext_BareContextReturnsDefault(t *testing.T) {
	got := i18n.LocaleFromContext(context.Background())
	assert.Equal(t, i18n.DefaultTag, got, "bare context must yield DefaultTag")
}

func TestLocaleFromContext_NilContextReturnsDefault(t *testing.T) {
	// Defensive: production code should never pass a nil ctx, but adversarial
	// callers must not panic the resolver.
	//nolint:staticcheck // intentional nil-context smoke test
	got := i18n.LocaleFromContext(nil)
	assert.Equal(t, i18n.DefaultTag, got)
}

func TestWithLocale_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		tag  language.Tag
	}{
		{name: "russian", tag: language.Russian},
		{name: "english", tag: language.English},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := i18n.WithLocale(context.Background(), tt.tag)
			assert.Equal(t, tt.tag, i18n.LocaleFromContext(ctx))
		})
	}
}

func TestTr_RussianContext(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), language.Russian)
	got := i18n.Tr(ctx, "test.hello", "Мир")
	assert.Equal(t, "Привет, Мир", got)
}

func TestTr_EnglishContext(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), language.English)
	got := i18n.Tr(ctx, "test.hello", "World")
	assert.Equal(t, "Hello, World", got)
}

func TestTr_DefaultsToRussianWithoutLocale(t *testing.T) {
	got := i18n.Tr(context.Background(), "test.hello", "Мир")
	assert.Equal(t, "Привет, Мир", got, "bare ctx must resolve via DefaultTag (ru)")
}

func TestTrTag_FallsBackToRussianForUnsupportedLocale(t *testing.T) {
	// French isn't in Supported — lookup() routes unknown bases through ru.
	got := i18n.TrTag(language.French, "test.hello", "Monde")
	assert.Equal(t, "Привет, Monde", got)
}

func TestTrTag_ReturnsKeyWhenMissingFromBothCatalogs(t *testing.T) {
	tests := []struct {
		name string
		tag  language.Tag
	}{
		{name: "russian", tag: language.Russian},
		{name: "english", tag: language.English},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "test.nonexistent.key"
			got := i18n.TrTag(tt.tag, key)
			assert.Equal(t, key, got, "missing key must surface literally so the bug is visible")
		})
	}
}

func TestTrTag_NoArgsSkipsSprintf(t *testing.T) {
	// Sanity: when no args are supplied the template must be returned verbatim
	// (so catalog entries containing literal `%` characters aren't mangled).
	got := i18n.TrTag(language.Russian, "test.hello")
	assert.Equal(t, "Привет, %s", got)
}

func TestMatchAcceptLanguage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   language.Tag
	}{
		{name: "simple english", header: "en", want: language.English},
		{name: "russian region", header: "ru-RU", want: language.Russian},
		{name: "english region", header: "en-US", want: language.English},
		{name: "unsupported language returns default", header: "fr", want: i18n.DefaultTag},
		{name: "weighted prefers higher q", header: "en-US;q=0.9, ru;q=0.5", want: language.English},
		{name: "weighted prefers ru when higher", header: "en-US;q=0.3, ru;q=0.9", want: language.Russian},
		{name: "empty header returns default", header: "", want: i18n.DefaultTag},
		{name: "garbage header returns default", header: "not-a-valid-lang-tag!!!", want: i18n.DefaultTag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := i18n.MatchAcceptLanguage(tt.header)
			// Compare on Base because Matcher may return a regional variant
			// (e.g. en-US) for some inputs even when we asked for `en`.
			wantBase, _ := tt.want.Base()
			gotBase, _ := got.Base()
			require.Equal(t, wantBase.String(), gotBase.String(),
				"header=%q want=%s got=%s", tt.header, tt.want, got)
		})
	}
}

func TestMatchAcceptLanguage_NeverPanics(t *testing.T) {
	// Adversarial inputs must not panic — this is the worst-case smoke test.
	inputs := []string{
		"",
		"!!!",
		"\x00\x01\x02",
		string(make([]byte, 1024)), // long zero-filled buffer
		"en;q=invalid",
		"de;q=0.9, fr;q=0.8, it;q=0.7",
	}
	for _, in := range inputs {
		assert.NotPanics(t, func() {
			_ = i18n.MatchAcceptLanguage(in)
		}, "input=%q must not panic", in)
	}
}
