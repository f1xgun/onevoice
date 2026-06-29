package yandex

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestFieldSavedMatches(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{
			name:     "exact match",
			expected: "Пн-Пт 9:00-18:00",
			actual:   "Пн-Пт 9:00-18:00",
			want:     true,
		},
		{
			name:     "read-back wraps typed value in layout whitespace",
			expected: "+79991234567",
			actual:   "  +79991234567  ",
			want:     true,
		},
		{
			name:     "case-insensitive match",
			expected: "Кофейня на углу",
			actual:   "кофейня на углу",
			want:     true,
		},
		{
			name:     "collapsed inner whitespace still matches",
			expected: "Пн-Пт 9:00-18:00",
			actual:   "Пн-Пт   9:00-18:00",
			want:     true,
		},
		{
			name:     "reformatted read-back containing typed value matches",
			expected: "+79991234567",
			actual:   "+7 999 123-45-67 +79991234567",
			want:     true,
		},
		{
			name:     "field reverted to a different value does not match",
			expected: "Пн-Пт 9:00-18:00",
			actual:   "Пн-Вс 10:00-20:00",
			want:     false,
		},
		{
			name:     "empty read-back (input cleared / nothing persisted) does not match",
			expected: "Пн-Пт 9:00-18:00",
			actual:   "",
			want:     false,
		},
		{
			name:     "empty expected never matches",
			expected: "",
			actual:   "Пн-Пт 9:00-18:00",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fieldSavedMatches(tt.expected, tt.actual); got != tt.want {
				t.Fatalf("fieldSavedMatches(%q, %q) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

// TestConfirmFieldSavedMismatch is the fail-on-revert guard: when the value
// read back after clickSave does not reflect what we typed (server rejected the
// save / the input reverted), confirmFieldSaved must return a non-retryable
// error so UpdateInfo / UpdateHours surface the failed write instead of
// reporting success. Reverting the read-back call in those tools makes the
// success path fire unconditionally; this asserts the helper they depend on
// rejects a mismatch.
func TestConfirmFieldSavedMismatch(t *testing.T) {
	err := confirmFieldSaved("Пн-Пт 9:00-18:00", "Пн-Вс 10:00-20:00")
	if err == nil {
		t.Fatal("confirmFieldSaved(mismatch) = nil; want non-retryable error " +
			"(a save the server rejected must not be reported as success)")
	}
	var nre *a2a.NonRetryableError
	if !errors.As(err, &nre) {
		t.Fatalf("confirmFieldSaved(mismatch) error = %T (%v); want *a2a.NonRetryableError", err, err)
	}
}

// TestConfirmFieldSavedEmptyReadback covers the "input cleared / nothing
// persisted" case explicitly: an empty read-back means the value did not land
// and must be rejected.
func TestConfirmFieldSavedEmptyReadback(t *testing.T) {
	if err := confirmFieldSaved("+79991234567", ""); err == nil {
		t.Fatal("confirmFieldSaved(_, \"\") = nil; want error when the input reads back empty")
	}
}

// TestConfirmFieldSavedMatch confirms the success path: an exact or
// whitespace/case-normalized read-back returns nil so a genuine save is not
// false-flagged as failed.
func TestConfirmFieldSavedMatch(t *testing.T) {
	if err := confirmFieldSaved("Пн-Пт 9:00-18:00", "  пн-пт   9:00-18:00  "); err != nil {
		t.Fatalf("confirmFieldSaved(match) = %v; want nil", err)
	}
}
