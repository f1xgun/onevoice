package security

import (
	"strings"
	"testing"
)

// TestContainsPII verifies the named-class detector against:
//
//   - The full true-positive corpus (≥9 cases across all 6 classes).
//   - The Russian false-positive corpus (≥10 cases) that legitimate numeric
//     titles MUST NOT trigger — Landmine 2 / Pitfall 3. Without prefix anchors
//     on passport / INN and Luhn on cc, the auto-titler would terminal-fail
//     on every order/invoice title.
func TestContainsPII(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantHit   bool
		wantClass string
	}{
		// --- True positives (must hit, with the named class). ---
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

		// --- False positives — Russian numeric titles MUST NOT match. ---
		{"Заказ 12345", "Заказ 12345 от вторника", false, ""},
		{"Чек 9876543", "Чек 9876543", false, ""},
		{"Звонок with date", "Звонок 2026-04-15 10:30", false, ""},
		{"Заявка 10 digits no prefix", "Заявка 7654321098", false, ""}, // 10 digits but no INN/passport prefix
		{"Артикул 9 digits", "Артикул 123456789", false, ""},
		{"Счёт 13 digits no prefix", "Счёт 1234567890123", false, ""},
		{"Доход за 2025", "Доход за 2025 квартал 1", false, ""},
		{"Платёж 100500", "Платёж 100500", false, ""},
		{"random short num", "Стол 5", false, ""},
		{"4-digit year alone", "Отчёт 2025", false, ""},
		{"non-luhn 16 digit", "Идентификатор 1234567890123456", false, ""}, // 16 digits but fails Luhn
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			class, hit := ContainsPIIClass(c.input)
			if hit != c.wantHit {
				t.Fatalf("ContainsPIIClass(%q) hit=%v want=%v (class=%q)", c.input, hit, c.wantHit, class)
			}
			if hit && class != c.wantClass {
				t.Fatalf("ContainsPIIClass(%q) class=%q want=%q", c.input, class, c.wantClass)
			}
			// ContainsPII must agree with hit.
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
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := RedactPII(c.input)
			if got != c.want {
				t.Fatalf("RedactPII(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

// TestRedactPII_LogShape is the negative-assertion regression test required by
// Landmine 6 / Pitfall 8: D-16 is a "MUST NOT log X" rule and positive
// field-presence assertions can't catch a future "I added a debug field"
// regression. For each PII input we assert:
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
		input := input
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
