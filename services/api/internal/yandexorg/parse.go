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

// numericSegment matches one fully-numeric path segment of at least 4 digits (a
// whole /<digits>/ segment, not a digit run inside a slug). Real Yandex org ids
// are long; a shorter fully-numeric segment (e.g. a page number) is never the id.
var numericSegment = regexp.MustCompile(`^\d{4,}$`)

// ParsePermalink extracts the numeric Yandex organization permalink from a
// pasted value. It accepts either a bare numeric permalink or a full
// Maps/Sprav URL and returns the numeric id. Non-numeric input, or a URL with
// no recognizable numeric org id, is rejected — there is no best-effort
// fallback, because a wrong permalink is a cross-tenant hazard.
//
// A bare numeric value is returned as-is; otherwise the org id is pulled from
// the URL path, with a raw-path scan fallback for a pasted path fragment that
// has no scheme.
func ParsePermalink(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ErrEmpty
	}

	if numericPermalink.MatchString(trimmed) {
		return trimmed, nil
	}

	candidate := trimmed
	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		candidate = u.Path
	}
	if id := orgIDFromPath(candidate); id != "" {
		return id, nil
	}
	if id := orgIDFromPath(trimmed); id != "" {
		return id, nil
	}
	return "", ErrNoPermalink
}

// orgIDFromPath extracts the org id from a recognized Sprav/Maps org path,
// anchoring to the marker so a digit-bearing slug can never be mistaken for the
// id and an unrelated URL with a stray numeric segment is rejected.
//
//   - /sprav/<id>/...           → the id is the FIRST segment after /sprav/.
//   - /maps/org/<id>/...        → the id is the FIRST numeric segment after
//   - /maps/org/<slug>/<id>/...    /maps/org/ (the slug, which may itself
//     contain digits like "name-1234", is skipped because it is not a
//     fully-numeric segment).
//
// Returns "" when no marker is present or no ≥4-digit segment follows it.
func orgIDFromPath(path string) string {
	segments := strings.Split(path, "/")
	for i := 0; i < len(segments); i++ {
		switch {
		case segments[i] == "sprav" && i+1 < len(segments) && numericSegment.MatchString(segments[i+1]):
			return segments[i+1]
		case segments[i] == "maps" && i+1 < len(segments) && segments[i+1] == "org":
			if id := firstNumericSegment(segments[i+2:]); id != "" {
				return id
			}
		}
	}
	return ""
}

// firstNumericSegment returns the first fully-numeric (≥4-digit) segment in
// segments, or "". Used for the Maps org shape, where the id follows an optional
// slug and precedes any tab (/reviews, /gallery, ...).
func firstNumericSegment(segments []string) string {
	for _, seg := range segments {
		if numericSegment.MatchString(seg) {
			return seg
		}
	}
	return ""
}
