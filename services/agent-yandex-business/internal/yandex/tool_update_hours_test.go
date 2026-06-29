package yandex

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestFormatHoursForYandex(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// The format that triggered "could not parse hours from" in prod:
			// each day maps to an array containing one range string.
			name: "array of range strings, all days equal",
			in:   `{"monday":["00:00-22:00"],"tuesday":["00:00-22:00"],"wednesday":["00:00-22:00"],"thursday":["00:00-22:00"],"friday":["00:00-22:00"],"saturday":["00:00-22:00"],"sunday":["00:00-22:00"]}`,
			want: "Пн-Вс 00:00-22:00",
		},
		{
			name: "array of range strings, split shift",
			in:   `{"monday":["09:00-13:00","14:00-18:00"]}`,
			want: "Пн 09:00-13:00, 14:00-18:00",
		},
		{
			name: "plain range strings per day with weekend variation",
			in:   `{"monday":"09:00-18:00","tuesday":"09:00-18:00","saturday":"10:00-15:00"}`,
			want: "Пн-Вт 09:00-18:00, Сб 10:00-15:00",
		},
		{
			name: "open/close objects",
			in:   `{"monday":{"open":"09:00","close":"22:00"}}`,
			want: "Пн 09:00-22:00",
		},
		{
			name: "array of open/close objects (unchanged behavior)",
			in:   `{"monday":[{"open":"09:00","close":"22:00"}]}`,
			want: "Пн 09:00-22:00",
		},
		{
			name: "closed string is skipped",
			in:   `{"monday":"09:00-18:00","sunday":"closed"}`,
			want: "Пн 09:00-18:00",
		},
		{
			name: "empty array yields no entry",
			in:   `{"monday":[]}`,
			want: "",
		},
		{
			// Already-Yandex-formatted text (the edit-page placeholder shape) is
			// forwarded verbatim — this is the partly-intentional raw path.
			name: "pre-formatted yandex text passes through",
			in:   "Пн-Пт 9:00-18:00, Сб 10:00-16:00",
			want: "Пн-Пт 9:00-18:00, Сб 10:00-16:00",
		},
		{
			name: "pre-formatted single day passes through",
			in:   "Пн 09:00-22:00",
			want: "Пн 09:00-22:00",
		},
		{
			name: "pre-formatted split shift passes through",
			in:   "Пн 09:00-13:00, 14:00-18:00",
			want: "Пн 09:00-13:00, 14:00-18:00",
		},
		{
			name: "pre-formatted with surrounding whitespace is trimmed",
			in:   "  Пн-Вс 00:00-22:00  ",
			want: "Пн-Вс 00:00-22:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatHoursForYandex(tt.in)
			if err != nil {
				t.Fatalf("formatHoursForYandex(%s) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("formatHoursForYandex(%s) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFormatHoursForYandexRejectsGarbage is the fail-on-revert guard: genuinely
// malformed input (not JSON and not the Yandex hours shape) must return an
// error rather than being forwarded verbatim into the operating-hours field.
// Before the fix, formatHoursForYandex returned (rawInput, nil) on json.Unmarshal
// failure, so UpdateHours would type the garbage and report success.
func TestFormatHoursForYandexRejectsGarbage(t *testing.T) {
	garbage := []struct {
		name string
		in   string
	}{
		{name: "truncated json missing closing brace", in: `{"monday": "9:00-18:00"`},
		{name: "json with trailing prose", in: `{"monday":"9:00-18:00"} call us anytime`},
		{name: "random non-hours string", in: "open whenever we feel like it"},
		{name: "english day names as free text", in: "Mon-Fri 9 to 5"},
		{name: "stray punctuation only", in: ";;; ???"},
		{name: "day token without a time range", in: "Пн-Пт"},
	}
	for _, g := range garbage {
		t.Run(g.name, func(t *testing.T) {
			got, err := formatHoursForYandex(g.in)
			if err == nil {
				t.Fatalf("formatHoursForYandex(%q) = %q, nil; want error for malformed input "+
					"(raw fallthrough would have typed this verbatim into the hours field)", g.in, got)
			}
			if got != "" {
				t.Errorf("formatHoursForYandex(%q) returned non-empty %q alongside error", g.in, got)
			}
		})
	}
}

// TestFormatHoursForYandexRejectsInvalidStructuredHours is the fail-on-revert
// guard for the structured-JSON branches: a day with one endpoint missing must
// NOT be silently dropped, and inverted ranges, impossible clocks, and free-text
// values must be rejected rather than typed into the hours field. Before the fix
// these inputs returned (text, nil) — the partial day vanished and the bad
// ranges/clocks passed straight through.
func TestFormatHoursForYandexRejectsInvalidStructuredHours(t *testing.T) {
	invalid := []struct {
		name string
		in   string
	}{
		{name: "partial day (open without close) is not silently dropped", in: `{"monday":{"open":"09:00","close":"18:00"},"tuesday":{"open":"10:00"}}`},
		{name: "partial day (close without open)", in: `{"monday":{"close":"18:00"}}`},
		{name: "inverted range", in: `{"monday":{"open":"18:00","close":"09:00"}}`},
		{name: "zero-length range", in: `{"monday":{"open":"09:00","close":"09:00"}}`},
		{name: "impossible clock", in: `{"monday":{"open":"25:00","close":"99:99"}}`},
		{name: "free-text string value", in: `{"monday":"banana"}`},
		{name: "inverted string-range value", in: `{"monday":"18:00-09:00"}`},
		{name: "impossible clock in array of range strings", in: `{"monday":["25:00-26:00"]}`},
		{name: "inverted range in array of objects", in: `{"monday":[{"open":"18:00","close":"09:00"}]}`},
		{name: "partial day in array of objects", in: `{"monday":[{"open":"09:00"}]}`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatHoursForYandex(tt.in)
			if err == nil {
				t.Fatalf("formatHoursForYandex(%q) = %q, nil; want error "+
					"(invalid hours must be rejected, not silently dropped or typed verbatim)", tt.in, got)
			}
			if got != "" {
				t.Errorf("formatHoursForYandex(%q) returned non-empty %q alongside error", tt.in, got)
			}
		})
	}
}

// TestUpdateHoursRejectsMalformedBeforeRPA verifies UpdateHours surfaces the
// validation error as a non-retryable A2A error without ever reaching the
// Playwright step. A nil *BusinessBrowser is safe here because the guard
// returns before any method on it is dereferenced.
func TestUpdateHoursRejectsMalformedBeforeRPA(t *testing.T) {
	var bb *BusinessBrowser
	err := bb.UpdateHours(t.Context(), `{"monday": "9:00-18:00"`)
	if err == nil {
		t.Fatal("UpdateHours(malformed) = nil; want non-retryable error (write must be rejected, not silently committed)")
	}
	var nre *a2a.NonRetryableError
	if !errors.As(err, &nre) {
		t.Errorf("UpdateHours(malformed) error = %T (%v); want *a2a.NonRetryableError", err, err)
	}
}
