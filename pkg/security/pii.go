package security

import (
	"regexp"
	"strings"
	"unicode"
)

// redactionToken is the literal Russian placeholder that replaces every PII
// match in RedactPII output. Downstream prompt builders rely on it verbatim.
//
//nolint:gosec // G101 false positive: this is a placeholder string, not a credential.
const redactionToken = "[Скрыто]"

// Compiled regex classes. Each class is responsible for one trust-boundary PII
// shape; the named-class lookup feeds the regex_class log field.
var (
	// Email — RFC 5322 simplified; covers >99% of practical inputs.
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

	// Credit card — 13–19 digits with optional space/dash separators. Pure
	// regex over-matches numeric titles; luhnValid enforces a real card.
	reCreditCard = regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`)

	// RU phone — three forms (E.164 +7XXXXXXXXXX, +7 (XXX) XXX-XX-XX,
	// 8 XXX XXX-XX-XX). Allows optional spacing inside the body but requires
	// all 11 digits.
	rePhoneRU = regexp.MustCompile(`(?:\+7|8)[\s\-(]*\d{3}[\s\-)]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}\b`)

	// IBAN — country (2 letters, uppercase per ISO 13616) + 2 check digits +
	// 11–30 alphanumeric.
	reIBAN = regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`)

	// RU passport — 4-digit series + 6-digit number, but ONLY when prefixed
	// by "паспорт" / "серия и номер" (case-insensitive) OR appearing as the
	// strict "DDDD DDDDDD" whitespace form. Bare 10-digit numbers without
	// either anchor (e.g. "Заказ 1234567890") MUST NOT match.
	rePassportRU = regexp.MustCompile(`(?i)(?:паспорт|серия\s+и\s+номер)[\s:№]*\d{4}\s*\d{6}\b|\b\d{4}\s\d{6}\b`)

	// INN — 10 digits (legal entity) or 12 digits (individual), but ONLY when
	// prefixed by "ИНН" / "INN" (case-insensitive). Bare numbers without the
	// prefix (e.g. "Заявка 7654321098") MUST NOT match.
	//
	// NOTE: Go's RE2 \b is ASCII-only — a word boundary is not detected at the
	// transition between a space and a Cyrillic letter, so a literal `\bИНН`
	// never matches in practice. We split the alternation: Latin "INN" keeps
	// `\b` (where ASCII boundary detection works), Cyrillic "ИНН" drops it
	// (the prefix is unique enough that mid-word collision is not a realistic
	// false-positive in Russian text).
	reINN = regexp.MustCompile(`(?i)(?:\bINN|ИНН)[\s:№]*\d{10}(?:\d{2})?\b`)
)

// piiClass binds a name (used as the regex_class log field) to a compiled
// pattern and an optional extra validator (e.g. Luhn for cc).
type piiClass struct {
	name    string
	pattern *regexp.Regexp
	extra   func(string) bool
}

// piiClasses is the canonical lookup order. The order matters for
// ContainsPIIClass (first hit wins) and for RedactPII (each class runs in
// turn). cc is last because its regex is the broadest and Luhn-gated.
var piiClasses = []piiClass{
	{name: "email", pattern: reEmail},
	{name: "phone", pattern: rePhoneRU},
	{name: "iban", pattern: reIBAN},
	{name: "passport", pattern: rePassportRU},
	{name: "inn", pattern: reINN},
	{name: "cc", pattern: reCreditCard, extra: luhnValid},
}

// RedactPII replaces every PII match in s with the placeholder "[Скрыто]".
// Idempotent. Safe on empty string. UTF-8 / Cyrillic preserved.
func RedactPII(s string) string {
	return RedactPIIExcept(s, nil)
}

// RedactPIIExcept behaves like RedactPII but preserves every match whose value
// is listed in allow. It exists for values that are PII-shaped yet are not
// third-party personal data — a business's OWN registered contact phone or
// e-mail, which it publishes on its public profile and which the assistant
// legitimately has to reproduce.
//
// Comparison is format-insensitive: both sides are folded by normalizePIIValue,
// so "+7 (843) 555-12-34", "8 843 555 12 34" and "+78435551234" are one value.
// A nil or empty allow makes this exactly RedactPII.
func RedactPIIExcept(s string, allow []string) string {
	allowed := normalizedAllowSet(allow)
	out := s
	for _, c := range piiClasses {
		out = c.pattern.ReplaceAllStringFunc(out, func(match string) string {
			if c.extra != nil && !c.extra(match) {
				return match
			}
			if allowed[normalizePIIValue(match)] {
				return match
			}
			return redactionToken
		})
	}
	return out
}

// normalizedAllowSet folds allow into a lookup set, dropping entries that
// normalize to the empty string. Returns nil for an empty input — lookups on a
// nil map are legal and always miss, so the caller needs no branch.
func normalizedAllowSet(allow []string) map[string]bool {
	if len(allow) == 0 {
		return nil
	}
	set := make(map[string]bool, len(allow))
	for _, v := range allow {
		if key := normalizePIIValue(v); key != "" {
			set[key] = true
		}
	}
	return set
}

// normalizePIIValue folds a PII-shaped value to a comparison key: lowercased,
// with every non-alphanumeric rune dropped. An 11-digit all-numeric result that
// starts with the Russian trunk prefix 8 is rewritten to the 7 country-code form
// so both national and E.164 spellings of one phone number collapse together.
func normalizePIIValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	digitsOnly := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsLetter(r):
			digitsOnly = false
			b.WriteRune(r)
		}
	}
	key := b.String()
	if digitsOnly && len(key) == ruPhoneDigits && key[0] == '8' {
		return "7" + key[1:]
	}
	return key
}

// ruPhoneDigits is the digit count of a full Russian phone number (country code
// or trunk prefix + 10 subscriber digits).
const ruPhoneDigits = 11

// ContainsPII reports whether s contains any PII pattern. Convenience wrapper
// over ContainsPIIClass.
func ContainsPII(s string) bool {
	_, hit := ContainsPIIClass(s)
	return hit
}

// ContainsPIIClass reports the first matching class name (or "", false). The
// class name is one of: "email", "phone", "iban", "passport", "inn", "cc".
//
// Logs this as the `regex_class` field — never the matched substring.
func ContainsPIIClass(s string) (string, bool) {
	for _, c := range piiClasses {
		loc := c.pattern.FindStringIndex(s)
		if loc == nil {
			continue
		}
		match := s[loc[0]:loc[1]]
		if c.extra != nil && !c.extra(match) {
			continue
		}
		return c.name, true
	}
	return "", false
}

// luhnValid implements the Luhn checksum (ISO/IEC 7812-1). It accepts the
// raw matched candidate (which may contain space/dash separators), strips
// non-digit runes, requires 13–19 digits, and returns true only when the
// checksum is divisible by 10. Rejects 16-digit IDs, order numbers, and
// other numeric titles that the cc regex would otherwise over-match.
func luhnValid(card string) bool {
	digits := make([]int, 0, 19)
	for _, r := range card {
		if unicode.IsDigit(r) {
			digits = append(digits, int(r-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	var sum int
	for i, d := range digits {
		if (len(digits)-i)%2 == 0 {
			d *= 2
			if d > 9 { //nolint:mnd // Luhn check digit overflow boundary
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
