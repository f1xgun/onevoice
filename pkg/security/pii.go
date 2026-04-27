package security

import (
	"regexp"
	"unicode"
)

// redactionToken is the literal Russian placeholder that replaces every PII
// match in RedactPII output. D-14 locked this exact token; downstream prompt
// builders rely on it verbatim.
//
//nolint:gosec // G101 false positive: this is a placeholder string, not a credential.
const redactionToken = "[Скрыто]"

// Compiled regex classes. Each class is responsible for one trust-boundary PII
// shape; the named-class lookup feeds D-16's regex_class log field.
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
	// prefix (e.g. "Заявка 7654321098") MUST NOT match — Landmine 2.
	//
	// NOTE: Go's RE2 \b is ASCII-only — a word boundary is not detected at the
	// transition between a space and a Cyrillic letter, so a literal `\bИНН`
	// never matches in practice. We split the alternation: Latin "INN" keeps
	// `\b` (where ASCII boundary detection works), Cyrillic "ИНН" drops it
	// (the prefix is unique enough that mid-word collision is not a realistic
	// false-positive in Russian text). Deviation from research draft regex.
	reINN = regexp.MustCompile(`(?i)(?:\bINN|ИНН)[\s:№]*\d{10}(?:\d{2})?\b`)
)

// piiClass binds a name (used as the D-16 regex_class log field) to a compiled
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
//
// Used by Phase 18 (titler) on the user message + assistant message BEFORE
// they reach the cheap LLM endpoint (D-14 defense-in-depth).
func RedactPII(s string) string {
	out := s
	for _, c := range piiClasses {
		out = c.pattern.ReplaceAllStringFunc(out, func(match string) string {
			if c.extra != nil && !c.extra(match) {
				return match // not a real PII match — leave intact
			}
			return redactionToken
		})
	}
	return out
}

// ContainsPII reports whether s contains any PII pattern. Convenience wrapper
// over ContainsPIIClass.
func ContainsPII(s string) bool {
	_, hit := ContainsPIIClass(s)
	return hit
}

// ContainsPIIClass reports the first matching class name (or "", false). The
// class name is one of: "email", "phone", "iban", "passport", "inn", "cc".
//
// Phase 18 logs this as the `regex_class` field per D-16 — never the matched
// substring.
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

// luhnValid implements the Luhn checksum used by major card schemes
// (ISO/IEC 7812-1). It accepts the raw matched candidate (which may contain
// space/dash separators), strips non-digit runes, requires 13–19 digits, and
// returns true only when the checksum is divisible by 10.
//
// Phase 18 uses Luhn to reject 16-digit IDs, order numbers, and other numeric
// titles that the cc regex would otherwise over-match.
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
		// Double every second digit from the right.
		if (len(digits)-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
