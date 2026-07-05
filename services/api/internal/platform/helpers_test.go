package platform

import (
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// scheduleSettings returns a settings blob carrying the schedule shape the
// default formatter and the {hours} placeholder both read.
func scheduleSettings() map[string]interface{} {
	return map[string]interface{}{
		"schedule": []map[string]interface{}{
			{"day": "mon", "open": "09:00", "close": "21:00", "closed": false},
			{"day": "tue", "open": "09:00", "close": "21:00", "closed": false},
			{"day": "wed", "open": "09:00", "close": "21:00", "closed": false},
			{"day": "thu", "open": "09:00", "close": "21:00", "closed": false},
			{"day": "fri", "open": "09:00", "close": "21:00", "closed": false},
			{"day": "sat", "open": "10:00", "close": "18:00", "closed": false},
			{"day": "sun", "open": "00:00", "close": "00:00", "closed": true},
		},
	}
}

func fullBusiness() *domain.Business {
	site := "example.com"
	return &domain.Business{
		Name:        "Кофейня",
		Category:    "Кафе",
		Address:     "ул. Ленина, 1",
		Phone:       "+7 900 000-00-00",
		Website:     &site,
		Description: "Лучший кофе в городе",
		Settings:    scheduleSettings(),
	}
}

// TestFormatTelegramDescription_Golden pins the default formatter's output for a
// fully-populated business. It is the fail-on-revert tripwire: any change to the
// default composition (emoji, ordering, schedule string, joins) turns this red.
func TestFormatTelegramDescription_Golden(t *testing.T) {
	got := formatTelegramDescription(fullBusiness())
	want := "Лучший кофе в городе\n\n" +
		"📞 +7 900 000-00-00\n" +
		"🌐 example.com\n" +
		"📍 ул. Ленина, 1\n\n" +
		"⏰ Пн-Пт 09:00-21:00, Сб 10:00-18:00"
	if got != want {
		t.Fatalf("default Telegram description drifted\n got: %q\nwant: %q", got, want)
	}
}

// TestRenderBusinessDescription_ZeroDiff_NoTemplate is the load-bearing zero-diff
// guarantee: with no descriptionTemplate override the render helper must equal
// the default formatter byte-for-byte, for both fully-populated and
// partially-populated businesses.
func TestRenderBusinessDescription_ZeroDiff_NoTemplate(t *testing.T) {
	empty := ""
	cases := map[string]*domain.Business{
		"all fields": fullBusiness(),
		"some empty fields": {
			Name:        "Бар",
			Description: "Только описание",
			Settings:    map[string]interface{}{},
		},
		"nil website + no schedule": {
			Name:    "Салон",
			Phone:   "+7 111",
			Website: &empty,
		},
		"nil settings": {
			Name:    "Магазин",
			Address: "Проспект Мира, 5",
		},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if got, want := renderBusinessDescription(b, maxTelegramDescription), formatTelegramDescription(b); got != want {
				t.Fatalf("render diverged from default\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// TestRenderBusinessDescription_ZeroDiff_EmptyTemplate confirms a present-but-empty
// override is treated as unset and still falls back to the default formatter.
func TestRenderBusinessDescription_ZeroDiff_EmptyTemplate(t *testing.T) {
	b := fullBusiness()
	b.Settings[DescriptionTemplateSettingsKey] = ""
	if got, want := renderBusinessDescription(b, maxTelegramDescription), formatTelegramDescription(fullBusiness()); got != want {
		t.Fatalf("empty template should fall back to default\n got: %q\nwant: %q", got, want)
	}
}

// TestRenderBusinessDescription_Substitution checks each placeholder maps to the
// right field and that the template is a full replacement (no auto contact block
// appended).
func TestRenderBusinessDescription_Substitution(t *testing.T) {
	b := fullBusiness()
	b.Settings[DescriptionTemplateSettingsKey] = "{name} — {category}\n{phone} · {website}\n{address}\n{hours}\n{description}"

	got := renderBusinessDescription(b, maxTelegramDescription)
	want := "Кофейня — Кафе\n" +
		"+7 900 000-00-00 · example.com\n" +
		"ул. Ленина, 1\n" +
		"Пн-Пт 09:00-21:00, Сб 10:00-18:00\n" +
		"Лучший кофе в городе"
	if got != want {
		t.Fatalf("substitution mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "📞") || strings.Contains(got, "⏰") {
		t.Fatalf("full-replace template must not carry default emoji/contact block: %q", got)
	}
}

// TestRenderBusinessDescription_MissingFieldsEmpty checks that empty and nil
// source fields render as empty strings rather than placeholder text.
func TestRenderBusinessDescription_MissingFieldsEmpty(t *testing.T) {
	b := &domain.Business{
		Name:     "X",
		Settings: map[string]interface{}{DescriptionTemplateSettingsKey: "[{phone}][{website}][{hours}]"},
	}
	if got, want := renderBusinessDescription(b, maxTelegramDescription), "[][][]"; got != want {
		t.Fatalf("missing fields should render empty\n got: %q\nwant: %q", got, want)
	}

	site := "site.ru"
	b2 := &domain.Business{
		Name:     "Y",
		Website:  &site,
		Settings: map[string]interface{}{DescriptionTemplateSettingsKey: "<{website}>"},
	}
	if got, want := renderBusinessDescription(b2, maxTelegramDescription), "<site.ru>"; got != want {
		t.Fatalf("website should render\n got: %q\nwant: %q", got, want)
	}
}

// TestRenderBusinessDescription_RuneCap verifies the render truncates to the
// platform cap (254 runes + ellipsis) and stays within the rune budget for
// multi-byte content.
func TestRenderBusinessDescription_RuneCap(t *testing.T) {
	long := strings.Repeat("Я", 400)
	b := &domain.Business{
		Name:     long,
		Settings: map[string]interface{}{DescriptionTemplateSettingsKey: "{name}"},
	}
	got := renderBusinessDescription(b, maxTelegramDescription)
	if n := len([]rune(got)); n != maxTelegramDescription {
		t.Fatalf("expected %d runes after truncation, got %d", maxTelegramDescription, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated output should end with ellipsis: %q", got)
	}
}

// TestRenderBusinessDescription_UnknownTokenLiteral confirms an unknown token
// that somehow reaches render is left literal (defense-in-depth) and never
// panics.
func TestRenderBusinessDescription_UnknownTokenLiteral(t *testing.T) {
	b := &domain.Business{
		Name:     "Z",
		Settings: map[string]interface{}{DescriptionTemplateSettingsKey: "{name}-{bogus}"},
	}
	if got, want := renderBusinessDescription(b, maxTelegramDescription), "Z-{bogus}"; got != want {
		t.Fatalf("unknown token should stay literal\n got: %q\nwant: %q", got, want)
	}
}

func TestUnknownDescriptionPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
		want []string
	}{
		{"all known", "{name} {category} {address} {phone} {website} {hours} {description}", nil},
		{"no tokens", "plain text, no braces", nil},
		{"single unknown", "{name} {foo}", []string{"{foo}"}},
		{"dedup + order", "{bar} {foo} {bar}", []string{"{bar}", "{foo}"}},
		{"wrong case", "{Name}", []string{"{Name}"}},
		{"empty braces", "{}", []string{"{}"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UnknownDescriptionPlaceholders(tc.tmpl)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestDescriptionPlaceholderValues_CoversAllowedSet guards against drift between
// AllowedDescriptionPlaceholders and the field resolver: every allowed name must
// resolve to a value pair the substituter can emit.
func TestDescriptionPlaceholderValues_CoversAllowedSet(t *testing.T) {
	values := descriptionPlaceholderValues(fullBusiness())
	if len(values) != len(AllowedDescriptionPlaceholders) {
		t.Fatalf("value map has %d keys, allow-set has %d", len(values), len(AllowedDescriptionPlaceholders))
	}
	for _, name := range AllowedDescriptionPlaceholders {
		if _, ok := values[name]; !ok {
			t.Fatalf("allowed placeholder %q has no field mapping", name)
		}
	}
}
