package config

import (
	"fmt"
	"math"
	"net/mail"
	"regexp"
	"strings"
	"unicode"

	"github.com/trustelem/zxcvbn"
)

// encryptionKeyDenyList holds literal ENCRYPTION_KEY values that have been
// shipped as dev defaults, demo strings, or all-zero / all-max-byte sentinels.
// They MUST never reach a production boot path.
var encryptionKeyDenyList = []string{
	"12345678901234567890123456789012",
	"00000000000000000000000000000000",
	"ffffffffffffffffffffffffffffffff",
	"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
}

// minRepeatedUnitRepetitions: if a 1-4 char unit covers the whole key with
// at least this many repetitions, the key collapses to brute-forceable
// effective entropy regardless of total length (e.g. "aaaaaaa…",
// "ababababab…", "abcabcabc…").
const minRepeatedUnitRepetitions = 7

const (
	// minShannonEntropy: bits/byte. Uniform random bytes approach 8.0; ASCII
	// hex of random bytes ≈ 3.94. 3.5 sits below hex but above pure-digit /
	// pure-letter strings (which clock ≈ 3.32).
	minShannonEntropy = 3.5
	// minZxcvbnScore: zxcvbn returns 0..4. 3 = "safely unguessable, moderate
	// protection from offline slow-hash scenario" per zxcvbn docs.
	minZxcvbnScore = 3
	// minCyrillicCharsInLegalEntityName: cheap heuristic that the value is a
	// real Russian legal-entity name (ООО / АО / etc), not an English fixture.
	minCyrillicCharsInLegalEntityName = 4
	// minLegalAddressRunes: short addresses ("Москва", "—") slip past the
	// placeholder regex; require enough content to be plausibly real.
	minLegalAddressRunes = 20
)

// validateEncryptionKey is a defense-in-depth gate for the AES-256 master
// key: deny-list → repeat pattern → Shannon entropy → zxcvbn score. Returns
// the first failure so the operator sees one specific reason at boot.
func validateEncryptionKey(key string) error {
	if key == "" {
		return fmt.Errorf("ENCRYPTION_KEY is empty")
	}
	for _, denied := range encryptionKeyDenyList {
		if key == denied {
			return fmt.Errorf("ENCRYPTION_KEY matches a known-weak deny-list literal")
		}
	}
	if isShortRepeatedPattern(key) {
		return fmt.Errorf("ENCRYPTION_KEY is a short repeated pattern")
	}
	if e := shannonEntropy([]byte(key)); e < minShannonEntropy {
		return fmt.Errorf("ENCRYPTION_KEY has insufficient entropy: %.2f bits/byte (need >=%.1f)", e, minShannonEntropy)
	}
	if s := zxcvbn.PasswordStrength(key, nil).Score; s < minZxcvbnScore {
		return fmt.Errorf("ENCRYPTION_KEY is dictionary-weak (zxcvbn score %d, need >=%d)", s, minZxcvbnScore)
	}
	return nil
}

// isShortRepeatedPattern reports whether s consists entirely of a 1-4 char
// unit repeated at least minRepeatedUnitRepetitions times. Implemented as a
// hand-rolled scan because Go's RE2 regex engine refuses backreferences.
func isShortRepeatedPattern(s string) bool {
	n := len(s)
	if n == 0 {
		return false
	}
	for unit := 1; unit <= 4; unit++ {
		if n%unit != 0 {
			continue
		}
		reps := n / unit
		if reps < minRepeatedUnitRepetitions {
			continue
		}
		head := s[:unit]
		ok := true
		for i := unit; i < n; i += unit {
			if s[i:i+unit] != head {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// shannonEntropy returns the Shannon entropy of b in bits per byte. Result is
// in [0, 8]. Empty input returns 0.
func shannonEntropy(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	var freq [256]int
	for _, c := range b {
		freq[c]++
	}
	n := float64(len(b))
	var h float64
	for _, f := range freq {
		if f == 0 {
			continue
		}
		p := float64(f) / n
		h -= p * math.Log2(p)
	}
	return h
}

// innDigitsRe accepts 10-digit (юр. лицо) or 12-digit (ИП / физ. лицо) INN.
var innDigitsRe = regexp.MustCompile(`^\d{10}$|^\d{12}$`)

// validateINN checks length + ФНС control-digit checksum per
// https://www.nalog.gov.ru/rn77/about_fts/about_nalog/3962425/
func validateINN(inn string) error {
	if !innDigitsRe.MatchString(inn) {
		return fmt.Errorf("INN must be 10 or 12 digits, got %q", inn)
	}
	d := make([]int, len(inn))
	for i, r := range inn {
		d[i] = int(r - '0')
	}
	if len(inn) == 10 {
		w := []int{2, 4, 10, 3, 5, 9, 4, 6, 8, 0}
		sum := 0
		for i := 0; i < 9; i++ {
			sum += d[i] * w[i]
		}
		if sum%11%10 != d[9] {
			return fmt.Errorf("INN checksum mismatch")
		}
		return nil
	}
	w1 := []int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8, 0}
	w2 := []int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8, 0}
	sum1, sum2 := 0, 0
	for i := 0; i < 11; i++ {
		sum1 += d[i] * w1[i]
	}
	for i := 0; i < 12; i++ {
		sum2 += d[i] * w2[i]
	}
	if sum1%11%10 != d[10] || sum2%11%10 != d[11] {
		return fmt.Errorf("INN checksum mismatch")
	}
	return nil
}

// placeholderRe matches placeholder strings shipped as LEGAL_* defaults:
// square-bracket templates, em-dash / hyphen sentinels, TBD / N/A markers,
// and Russian "будет ..." futures.
var placeholderRe = regexp.MustCompile(`(?i)^(\[.*\]|—|-|tbd|n/a|будет .*)$`)

// isLegalPlaceholder reports whether s is empty / whitespace / one of the
// placeholder templates above. Trims whitespace before matching.
func isLegalPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	return placeholderRe.MatchString(s)
}

// validateLegalProduction aggregates every LEGAL_* field problem into a
// single error so the operator fixes them in one boot cycle instead of
// chasing one-at-a-time.
func validateLegalProduction(cfg *Config) error {
	var problems []string

	if isLegalPlaceholder(cfg.LegalEntityName) {
		problems = append(problems, "LEGAL_ENTITY_NAME is empty or placeholder")
	} else {
		cyrillicCount := 0
		for _, r := range cfg.LegalEntityName {
			if unicode.Is(unicode.Cyrillic, r) {
				cyrillicCount++
			}
		}
		if cyrillicCount < minCyrillicCharsInLegalEntityName {
			problems = append(problems, "LEGAL_ENTITY_NAME must contain >=4 Cyrillic characters")
		}
	}

	if isLegalPlaceholder(cfg.LegalINN) {
		problems = append(problems, "LEGAL_INN is empty or placeholder")
	} else if err := validateINN(cfg.LegalINN); err != nil {
		problems = append(problems, "LEGAL_INN: "+err.Error())
	}

	if isLegalPlaceholder(cfg.LegalAddress) {
		problems = append(problems, "LEGAL_ADDRESS is empty or placeholder")
	} else if len([]rune(cfg.LegalAddress)) < minLegalAddressRunes {
		problems = append(problems, "LEGAL_ADDRESS must be at least 20 characters")
	}

	if isLegalPlaceholder(cfg.LegalEmailPDN) {
		problems = append(problems, "LEGAL_EMAIL_PDN is empty or placeholder")
	} else if _, err := mail.ParseAddress(cfg.LegalEmailPDN); err != nil {
		problems = append(problems, "LEGAL_EMAIL_PDN: "+err.Error())
	}

	if len(problems) > 0 {
		return fmt.Errorf("legal validation failed: %s", strings.Join(problems, "; "))
	}
	return nil
}
