package templates

import (
	"strings"
	"testing"
	"time"
)

// maliciousName is an organization name a non-owner admin (PermBusinessUpdate)
// could set, carrying a phishing link, a tracking pixel, and a script tag.
const maliciousName = `<a href="https://evil.example/login">Restore now</a><img src="https://evil/track.gif"><script>alert(1)</script>`

// rawMarkup are the raw HTML fragments from the injected payload that must NOT
// appear in a rendered HTML body — their presence means the name was
// interpolated unescaped. These are unique to the attacker payload so they
// don't collide with the template's own legitimate static <a> button.
var rawMarkup = []string{`<a href="https://evil`, `<img src="https://evil`, `<script>alert`}

func htmlBuilders() map[string]func(locale, name string, deletionAt time.Time) string {
	return map[string]func(locale, name string, deletionAt time.Time) string{
		"BusinessDeletionConfirmationHTML": BusinessDeletionConfirmationHTML,
		"BusinessDeletionT7WarningHTML":    BusinessDeletionT7WarningHTML,
	}
}

func textBuilders() map[string]func(locale, name string, deletionAt time.Time) string {
	return map[string]func(locale, name string, deletionAt time.Time) string{
		"BusinessDeletionConfirmationText": BusinessDeletionConfirmationText,
		"BusinessDeletionT7WarningText":    BusinessDeletionT7WarningText,
	}
}

func TestBusinessDeletionHTMLEscapesName(t *testing.T) {
	deletionAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, locale := range []string{"ru", "en"} {
		for name, build := range htmlBuilders() {
			body := build(locale, maliciousName, deletionAt)

			if !strings.Contains(body, "&lt;a href=") {
				t.Errorf("%s[%s]: expected escaped link (&lt;a href=) in body, got:\n%s", name, locale, body)
			}
			if !strings.Contains(body, "&lt;script&gt;") {
				t.Errorf("%s[%s]: expected escaped <script> (&lt;script&gt;) in body, got:\n%s", name, locale, body)
			}
			for _, raw := range rawMarkup {
				if strings.Contains(body, raw) {
					t.Errorf("%s[%s]: raw markup %q present in HTML body — name was not escaped:\n%s", name, locale, raw, body)
				}
			}
		}
	}
}

func TestBusinessDeletionTextPassesNameThrough(t *testing.T) {
	deletionAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, locale := range []string{"ru", "en"} {
		for name, build := range textBuilders() {
			body := build(locale, maliciousName, deletionAt)
			if !strings.Contains(body, maliciousName) {
				t.Errorf("%s[%s]: plaintext body should carry the name verbatim, got:\n%s", name, locale, body)
			}
			if strings.Contains(body, "&lt;") {
				t.Errorf("%s[%s]: plaintext body must not HTML-escape the name, got:\n%s", name, locale, body)
			}
		}
	}
}

func TestBusinessDeletionHTMLBenignNameUnchanged(t *testing.T) {
	deletionAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	const benign = "Coffee House"
	for _, locale := range []string{"ru", "en"} {
		for name, build := range htmlBuilders() {
			body := build(locale, benign, deletionAt)
			if !strings.Contains(body, benign) {
				t.Errorf("%s[%s]: escaping a benign name must be a no-op, %q missing from:\n%s", name, locale, benign, body)
			}
		}
	}
}
