// Package i18n provides runtime locale resolution and a minimal flat-catalog
// translation primitive shared by every Go service in the OneVoice backend.
//
// The package intentionally avoids heavier libraries (e.g. nicksnyder/go-i18n)
// because the catalog is small (~60 keys at maturity), all formatting is
// handled by fmt.Sprintf, and ICU plural rules belong on the frontend where
// next-intl already covers them. See `.planning/i18n-readiness/PLAN.md` AD-2.
//
// Two consumers exist:
//
//  1. HTTP middleware (`services/{api,orchestrator}/internal/middleware/locale.go`)
//     parses Accept-Language and stores the resolved tag in the request context
//     via WithLocale. Handlers then call Tr(ctx, key) without threading the
//     locale explicitly.
//  2. LLM prompt builders (Phase D) read the tag off the chat-scoped context
//     and pick the matching template / tool description.
//
// Catalog entries live in catalog_ru.go and catalog_en.go. RU is the default
// and the fallback — keys missing from EN resolve to RU; keys missing from
// both resolve to the literal key string (matches the next-intl behavior on
// the frontend, makes missing-key bugs visible without crashing).
package i18n

import (
	"context"
	"fmt"

	"golang.org/x/text/language"
)

// Supported lists the locales the backend can resolve via Accept-Language.
// Order matters for language.NewMatcher: the first tag is preferred when
// the client's preferences are ambiguous (e.g. empty Accept-Language).
var Supported = []language.Tag{
	language.Russian,
	language.English,
}

// DefaultTag is the locale used when no usable Accept-Language header is
// present, when parsing fails, or when the resolved tag has no catalog.
// Kept as `ru` for backwards compatibility with the pre-i18n era.
var DefaultTag = language.Russian

// Matcher resolves a client Accept-Language list against Supported.
// Declared at package scope because NewMatcher is moderately expensive and
// the result is safe for concurrent use (immutable after construction).
var Matcher = language.NewMatcher(Supported)

// ctxKey is unexported to prevent foreign packages from colliding with our
// context entry; the canonical Go idiom for context keys.
type ctxKey int

const localeKey ctxKey = iota

// WithLocale returns a derived context carrying the given language tag.
// Used by the LocaleResolver HTTP middleware and by the chat plumbing that
// propagates the per-conversation locale to the orchestrator.
func WithLocale(ctx context.Context, tag language.Tag) context.Context {
	return context.WithValue(ctx, localeKey, tag)
}

// LocaleFromContext returns the tag previously stored by WithLocale, or
// DefaultTag if the context carries none. Never returns the zero Tag so
// callers can use the result unconditionally.
func LocaleFromContext(ctx context.Context) language.Tag {
	if ctx == nil {
		return DefaultTag
	}
	if tag, ok := ctx.Value(localeKey).(language.Tag); ok {
		return tag
	}
	return DefaultTag
}

// Tr looks up `key` in the catalog selected by the locale on `ctx` and
// returns the formatted string. When `args` are supplied the template is
// passed through fmt.Sprintf, so catalog entries use %s / %d / etc.
// Convenience wrapper around TrTag for handler code.
func Tr(ctx context.Context, key string, args ...any) string {
	return TrTag(LocaleFromContext(ctx), key, args...)
}

// TrTag is the locale-explicit form of Tr. Resolution order:
//
//  1. Look up `key` in the catalog whose base matches `tag` (en → en, ru → ru,
//     anything else → ru fallback).
//  2. If the key is missing and the chosen catalog is EN, retry against RU
//     (RU is canonical — every key SHOULD exist there).
//  3. If still missing, return the literal `key` so the caller sees the
//     bug instead of an empty string.
//
// fmt.Sprintf is only invoked when len(args) > 0 to avoid touching templates
// that contain a literal `%` but no placeholders.
func TrTag(tag language.Tag, key string, args ...any) string {
	template, ok := lookup(tag, key)
	if !ok {
		// EN miss → RU fallback. If RU also misses, surface the key.
		if template, ok = ru[key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return template
	}
	return fmt.Sprintf(template, args...)
}

// lookup picks the catalog that matches the tag's base language and returns
// (template, found). Unsupported bases fall through to RU.
func lookup(tag language.Tag, key string) (string, bool) {
	base, _ := tag.Base()
	switch base.String() {
	case "en":
		v, ok := en[key]
		return v, ok
	default:
		v, ok := ru[key]
		return v, ok
	}
}

// NormalizeToSupported collapses an arbitrary language.Tag down to one of
// Supported (currently Russian or English) — the locale-tag equivalent of
// lookup()'s catalog selection. Use it when downstream code needs a Tag
// (not a catalog string) and the input could be a regional variant
// ("en-US"), an unsupported language ("fr"), or the zero Tag from test
// fixtures or unset BusinessContext.Locale fields.
//
// Resolution rules:
//   - zero Tag             → DefaultTag (Russian). The zero Tag's base
//     ("und") reports as "en" via golang.org/x/text, so treating it as
//     English would silently flip the LLM output language for any caller
//     that forgets to populate Locale. The zero Tag is interpreted as
//     "unset" instead.
//   - base.String() == "en" → English
//   - everything else      → DefaultTag (Russian)
//
// Sibling of MatchAcceptLanguage: that one normalizes a string header,
// this one normalizes a Tag value. Together they cover every entry point
// where untrusted locale data crosses into the backend.
func NormalizeToSupported(tag language.Tag) language.Tag {
	if tag == (language.Tag{}) {
		return DefaultTag
	}
	base, _ := tag.Base()
	if base.String() == "en" {
		return language.English
	}
	return DefaultTag
}

// MatchAcceptLanguage parses an HTTP Accept-Language header and returns the
// best Supported tag. Returns DefaultTag on:
//   - empty header (nothing to match)
//   - malformed header (ParseAcceptLanguage error — protects callers from
//     having to handle the error themselves and prevents a panic from
//     adversarial input)
//
// Caller contract: the returned tag is always one of Supported, never the
// zero Tag.
func MatchAcceptLanguage(header string) language.Tag {
	if header == "" {
		return DefaultTag
	}
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil || len(tags) == 0 {
		return DefaultTag
	}
	matched, _, conf := Matcher.Match(tags...)
	// `language.NewMatcher` falls back to English for any unmatched input
	// regardless of Supported order (its "neutral" default), so the
	// confidence value is what actually tells us if a real match happened.
	// Anything below Low is effectively a guess — treat it as "no match" and
	// honor our own DefaultTag instead.
	if conf == language.No {
		return DefaultTag
	}
	if matched == (language.Tag{}) {
		return DefaultTag
	}
	return matched
}
