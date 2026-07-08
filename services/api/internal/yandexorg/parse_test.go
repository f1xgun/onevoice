package yandexorg_test

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/services/api/internal/yandexorg"
)

func TestParsePermalink_Valid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare numeric", "114697172504", "114697172504"},
		{"bare numeric with whitespace", "  114697172504 ", "114697172504"},
		{"sprav url", "https://yandex.ru/sprav/114697172504/p/edit", "114697172504"},
		{"sprav url trailing slash", "https://yandex.ru/sprav/114697172504/", "114697172504"},
		{"maps org url slug then id", "https://yandex.ru/maps/org/kafe/114697172504/", "114697172504"},
		{"maps org url id only", "https://yandex.ru/maps/org/114697172504/", "114697172504"},
		{"maps org url with reviews tab", "https://yandex.ru/maps/org/coffee/114697172504/reviews/", "114697172504"},
		{"path fragment without scheme", "/sprav/114697172504/p/edit", "114697172504"},
		{"maps org digit-bearing slug then id", "https://yandex.ru/maps/org/name-1234/98765432/", "98765432"},
		{"maps org digit-slug id and tab", "https://yandex.ru/maps/org/coffee-2024/114697172504/reviews/", "114697172504"},
		{"maps org multi-digit-slug then id", "https://yandex.ru/maps/org/cafe-1-2-3/555555/reviews", "555555"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := yandexorg.ParsePermalink(tc.input)
			if err != nil {
				t.Fatalf("ParsePermalink(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParsePermalink(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParsePermalink_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty", "", yandexorg.ErrEmpty},
		{"whitespace only", "   ", yandexorg.ErrEmpty},
		{"non-numeric bare", "not-a-permalink", yandexorg.ErrNoPermalink},
		{"url with no org id", "https://yandex.ru/maps/", yandexorg.ErrNoPermalink},
		{"url with only short numbers", "https://yandex.ru/maps/org/12/", yandexorg.ErrNoPermalink},
		{"alpha id", "https://yandex.ru/sprav/abcdef/p/edit", yandexorg.ErrNoPermalink},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := yandexorg.ParsePermalink(tc.input)
			if err == nil {
				t.Fatalf("ParsePermalink(%q) expected error %v, got nil", tc.input, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParsePermalink(%q) error = %v, want %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

// TestParsePermalink_RejectsNonNumericTail guards the isolation property: a
// permalink that is not purely numeric must never be accepted, because a
// non-numeric external_id would break the /sprav/<permalink>/ assertion the
// agent relies on for tenant scoping.
func TestParsePermalink_RejectsNonNumericTail(t *testing.T) {
	if _, err := yandexorg.ParsePermalink("114697172504x"); err == nil {
		t.Fatal("a permalink with a non-numeric suffix must be rejected")
	}
}
