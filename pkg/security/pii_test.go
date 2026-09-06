package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContainsPII verifies the named-class detector against:
//
//   - The full true-positive corpus (≥9 cases across all 6 classes).
//   - The Russian false-positive corpus (≥10 cases) that legitimate numeric
//     titles MUST NOT trigger. Without prefix anchors
//     on passport / INN and Luhn on cc, the auto-titler would terminal-fail
//     on every order/invoice title.
func TestContainsPII(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantHit   bool
		wantClass string
	}{
		{"valid CC (luhn-valid 4111...)", "Спросил про 4111111111111111", true, "cc"},
		{"valid CC dashed (luhn-valid)", "Платёж 4111-1111-1111-1111", true, "cc"},
		{"RU phone +7 fmt", "Связь +7 (495) 123-45-67", true, "phone"},
		{"RU phone 8 fmt", "Звонить 8 495 123 45 67", true, "phone"},
		{"email", "Письмо на user@example.com", true, "email"},
		{"IBAN UK test vector", "Банк GB82WEST12345698765432", true, "iban"},
		{"INN 10 with prefix", "Контрагент ИНН 7707083893", true, "inn"},
		{"INN 12 with prefix", "ИНН: 770708388912", true, "inn"},
		{"passport with Cyrillic prefix", "паспорт 1234 567890 РФ", true, "passport"},
		{"passport strict 4+6 whitespace form", "Серия: 1234 567890", true, "passport"},

		{"Заказ 12345", "Заказ 12345 от вторника", false, ""},
		{"Чек 9876543", "Чек 9876543", false, ""},
		{"Звонок with date", "Звонок 2026-04-15 10:30", false, ""},
		{"Заявка 10 digits no prefix", "Заявка 7654321098", false, ""},
		{"Артикул 9 digits", "Артикул 123456789", false, ""},
		{"Счёт 13 digits no prefix", "Счёт 1234567890123", false, ""},
		{"Доход за 2025", "Доход за 2025 квартал 1", false, ""},
		{"Платёж 100500", "Платёж 100500", false, ""},
		{"random short num", "Стол 5", false, ""},
		{"4-digit year alone", "Отчёт 2025", false, ""},
		{"non-luhn 16 digit", "Идентификатор 1234567890123456", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			class, hit := ContainsPIIClass(c.input)
			if hit != c.wantHit {
				t.Fatalf("ContainsPIIClass(%q) hit=%v want=%v (class=%q)", c.input, hit, c.wantHit, class)
			}
			if hit && class != c.wantClass {
				t.Fatalf("ContainsPIIClass(%q) class=%q want=%q", c.input, class, c.wantClass)
			}
			if got := ContainsPII(c.input); got != c.wantHit {
				t.Fatalf("ContainsPII(%q) = %v want %v", c.input, got, c.wantHit)
			}
		})
	}
}

// TestRedactPII verifies that matches are replaced with "[Скрыто]" verbatim
// and that non-PII inputs (including Luhn-failing CC candidates and legitimate
// Russian numeric titles) survive unchanged.
func TestRedactPII(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"phone", "Перезвонить +7 (495) 123-45-67 утром", "Перезвонить [Скрыто] утром"},
		{"email", "user@x.ru — на почту", "[Скрыто] — на почту"},
		{"valid CC", "карта 4111-1111-1111-1111", "карта [Скрыто]"},
		{"non-luhn passes through", "id 1234-5678-9012-3456", "id 1234-5678-9012-3456"},
		{"Заказ 12345 untouched", "Заказ 12345", "Заказ 12345"},
		{"mixed email + phone", "user@x.ru и +7 495 1234567", "[Скрыто] и [Скрыто]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RedactPII(c.input)
			if got != c.want {
				t.Fatalf("RedactPII(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

// TestRedactPII_LogShape is a negative-assertion regression test:
// "MUST NOT log X" rule — positive field-presence assertions can't catch
// a future "I added a debug field" regression. For each PII input we assert:
//
//   - The original PII substring does NOT appear in the redacted output.
//   - The placeholder "[Скрыто]" DOES appear.
//
// This proves token replacement actually wipes the bytes, not just decorates
// them with a sibling field.
func TestRedactPII_LogShape(t *testing.T) {
	piiInputs := []string{
		"+7 (495) 123-45-67",
		"user@example.com",
		"4111111111111111",
		"GB82WEST12345698765432",
		"ИНН 7707083893",
		"паспорт 1234 567890",
	}
	for _, input := range piiInputs {
		t.Run(input, func(t *testing.T) {
			redacted := RedactPII(input)
			if strings.Contains(redacted, input) {
				t.Fatalf("RedactPII(%q) leaked the original PII substring: %q", input, redacted)
			}
			if !strings.Contains(redacted, "[Скрыто]") {
				t.Fatalf("RedactPII(%q) did not contain placeholder, got %q", input, redacted)
			}
		})
	}
}

// TestRedactPIIExcept covers the allowlist carve-out used for a business's own
// registered contact details. Phone matching is format-insensitive:
// the +7 / 8 / punctuated spellings of one Russian number are one value, and
// e-mail comparison is case-insensitive.
func TestRedactPIIExcept(t *testing.T) {
	cases := []struct {
		name  string
		input string
		allow []string
		want  string
	}{
		{name: "website cannot allow email", input: "customer a@b.com", allow: []string{"ab.com"}, want: "customer [Скрыто]"},
		{name: "local part dots remain meaningful", input: "first.last@example.com", allow: []string{"firstlast@example.com"}, want: "[Скрыто]"},
		{name: "local part dots remain meaningful in reverse", input: "firstlast@example.com", allow: []string{"first.last@example.com"}, want: "[Скрыто]"},
		{name: "domain dots remain meaningful", input: "a@b.com", allow: []string{"a@bc.om"}, want: "[Скрыто]"},
		{name: "plus tag remains meaningful", input: "first+last@example.com", allow: []string{"firstlast@example.com"}, want: "[Скрыто]"},
		{name: "email prefix cannot grant exemption", input: "a@b.com", allow: []string{"contact: a@b.com"}, want: "[Скрыто]"},
		{name: "phone prefix cannot grant exemption", input: "+78435551234", allow: []string{"phone: +78435551234"}, want: "[Скрыто]"},
		{name: "email allowlist requires whole value", input: "a@b.com", allow: []string{"a@b.com/path"}, want: "[Скрыто]"},
		{name: "email whitespace is not ignored", input: "a@b.com", allow: []string{" a@b.com "}, want: "[Скрыто]"},
		{name: "numeric website cannot allow phone", input: "+78435551234", allow: []string{"7843.5551234"}, want: "[Скрыто]"},
		{name: "phone extension is not ignored", input: "+78435551234", allow: []string{"+78435551234/"}, want: "[Скрыто]"},
		{name: "national allowlist matches international phone", input: "+78435551234", allow: []string{"8 (843) 555-12-34"}, want: "+78435551234"},
		{name: "other PII classes cannot be allowlisted", input: "4111111111111111", allow: []string{"4111111111111111"}, want: "[Скрыто]"},
		{
			name:  "nil allowlist behaves exactly like RedactPII",
			input: "тел. +7 (843) 555-12-34",
			allow: nil,
			want:  "тел. [Скрыто]",
		},
		{
			name:  "exact spelling is preserved",
			input: "тел. +7 (843) 555-12-34",
			allow: []string{"+7 (843) 555-12-34"},
			want:  "тел. +7 (843) 555-12-34",
		},
		{
			name:  "E.164 spelling matches the punctuated allowlist entry",
			input: "тел. +78435551234",
			allow: []string{"+7 (843) 555-12-34"},
			want:  "тел. +78435551234",
		},
		{
			name:  "8-prefixed national spelling matches the +7 allowlist entry",
			input: "тел. 8 843 555-12-34",
			allow: []string{"+78435551234"},
			want:  "тел. 8 843 555-12-34",
		},
		{
			name:  "a different number is still redacted",
			input: "наш +78435551234, клиента +78120000000",
			allow: []string{"+78435551234"},
			want:  "наш +78435551234, клиента [Скрыто]",
		},
		{
			name:  "e-mail comparison ignores case",
			input: "пишите Info@Utro.ru",
			allow: []string{"info@utro.ru"},
			want:  "пишите Info@Utro.ru",
		},
		{
			name:  "an unrelated e-mail beside the allowed one is still redacted",
			input: "info@utro.ru и a@b.com",
			allow: []string{"info@utro.ru"},
			want:  "info@utro.ru и [Скрыто]",
		},
		{
			name:  "blank allowlist entries are ignored",
			input: "тел. +78435551234",
			allow: []string{"", "   ", "-"},
			want:  "тел. [Скрыто]",
		},
		{
			name:  "a website entry never accidentally allows a phone",
			input: "тел. +78435551234",
			allow: []string{"https://utro.ru"},
			want:  "тел. [Скрыто]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RedactPIIExcept(tc.input, tc.allow))
		})
	}
}

// TestRedactPIIExcept_Idempotent asserts a second pass over already-redacted
// text is a no-op and still preserves the allowlisted value.
func TestRedactPIIExcept_Idempotent(t *testing.T) {
	allow := []string{"+78435551234"}
	once := RedactPIIExcept("наш +78435551234, клиента a@b.com", allow)
	twice := RedactPIIExcept(once, allow)
	assert.Equal(t, once, twice)
	assert.Contains(t, twice, "+78435551234")
}
