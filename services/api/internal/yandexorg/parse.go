// Package yandexorg parses a Yandex organization permalink out of a pasted
// Yandex Maps / Sprav URL. The permalink is the numeric organization id that
// scopes every delegated-representative RPA action to one tenant, so parsing is
// strict: only a numeric id is accepted, and it is resolved exclusively from the
// owner-supplied URL (never from LLM or task args).
package yandexorg

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrEmpty is returned when the input is blank.
var ErrEmpty = errors.New("yandexorg: empty input")

// ErrNoPermalink is returned when no numeric permalink can be extracted.
var ErrNoPermalink = errors.New("yandexorg: no numeric permalink found in input")

// numericPermalink matches a run of digits — a bare permalink or the numeric id
// embedded in a Maps/Sprav path segment.
var numericPermalink = regexp.MustCompile(`^\d+$`)

// orgIDInPath extracts the numeric id from the known Yandex URL shapes:
//   - /sprav/<id>/...            (Sprav business console)
//   - /maps/org/<slug>/<id>/...  (Maps org card, slug then id)
//   - /maps/org/<id>/...         (Maps org card, id only)
//
// The last all-numeric path segment is the org id in every observed shape.
var orgIDInPath = regexp.MustCompile(`(?:/sprav/|/maps/org/[^/]*?/?)(\d{4,})`)

// ParsePermalink extracts the numeric Yandex organization permalink from a
// pasted value. It accepts either a bare numeric permalink or a full
// Maps/Sprav URL and returns the numeric id. Non-numeric input, or a URL with
// no recognizable numeric org id, is rejected — there is no best-effort
// fallback, because a wrong permalink is a cross-tenant hazard.
func ParsePermalink(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ErrEmpty
	}

	// Bare numeric permalink.
	if numericPermalink.MatchString(trimmed) {
		return trimmed, nil
	}

	// Otherwise treat it as a URL and pull the org id from the path (falling
	// back to a raw scan when it does not parse as a URL, e.g. a pasted path
	// fragment without a scheme).
	candidate := trimmed
	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		candidate = u.Path
	}
	if m := orgIDInPath.FindStringSubmatch(candidate); m != nil {
		return m[1], nil
	}
	if m := orgIDInPath.FindStringSubmatch(trimmed); m != nil {
		return m[1], nil
	}
	return "", ErrNoPermalink
}
