package config

import (
	"strings"
	"testing"
)

func TestValidateEncryptionKey_DenyListLiteral(t *testing.T) {
	err := validateEncryptionKey("12345678901234567890123456789012")
	if err == nil {
		t.Fatal("expected error for known-weak deny-list literal, got nil")
	}
	if !strings.Contains(err.Error(), "deny-list") {
		t.Fatalf("expected error to mention deny-list, got %q", err.Error())
	}
}

func TestValidateEncryptionKey_DenyListZeros(t *testing.T) {
	if err := validateEncryptionKey("00000000000000000000000000000000"); err == nil {
		t.Fatal("expected error for all-zero key, got nil")
	}
}

func TestValidateEncryptionKey_DenyListFFs(t *testing.T) {
	if err := validateEncryptionKey("ffffffffffffffffffffffffffffffff"); err == nil {
		t.Fatal("expected error for all-FF key, got nil")
	}
	if err := validateEncryptionKey("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"); err == nil {
		t.Fatal("expected error for uppercase all-FF key, got nil")
	}
}

func TestValidateEncryptionKey_LowEntropyRepeated(t *testing.T) {
	err := validateEncryptionKey(strings.Repeat("a", 32))
	if err == nil {
		t.Fatal("expected error for 32x'a' key, got nil")
	}
	if !strings.Contains(err.Error(), "repeated") && !strings.Contains(err.Error(), "entropy") {
		t.Fatalf("expected error to mention repeated/entropy, got %q", err.Error())
	}
}

func TestValidateEncryptionKey_DictionaryWeak(t *testing.T) {
	weak := "password" + strings.Repeat("0", 24)
	err := validateEncryptionKey(weak)
	if err == nil {
		t.Fatal("expected error for dictionary-weak key, got nil")
	}
}

func TestValidateEncryptionKey_StrongKey(t *testing.T) {
	// Base64 of 24 random bytes — uniform distribution + 32 chars. The
	// literal here is the example given in the plan's must_haves.truth.
	if err := validateEncryptionKey("ZmFrZSByYW5kb20gMzIgYnl0ZSBzdHJpbmcga2V5c2FtcA=="); err == nil {
		// 47 chars — but encryption-key length check happens BEFORE
		// validateEncryptionKey is called by Load. Here we want the
		// validator alone to accept it because each gate (deny / repeat
		// / entropy / dictionary) clears.
		return
	} else {
		t.Fatalf("expected strong base64 random key to pass, got %v", err)
	}
}

func TestValidateEncryptionKey_StrongMixed32(t *testing.T) {
	// 32-byte high-entropy mix that mirrors the suggested production-style
	// output of `openssl rand -base64 24` (24 random bytes → 32 base64
	// chars). Must clear every gate.
	if err := validateEncryptionKey("uW4qX9pTzN3vM8yJ7sR2bL5kH1gD0fA6"); err != nil {
		t.Fatalf("expected strong 32-char key to pass, got %v", err)
	}
}

func TestValidateEncryptionKey_Empty(t *testing.T) {
	if err := validateEncryptionKey(""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestShannonEntropy_DigitString(t *testing.T) {
	// "12345678901234567890123456789012" — 10 unique digits in a 32-byte
	// string. Expected entropy ~ log2(10) = 3.32 bits/byte.
	got := shannonEntropy([]byte("12345678901234567890123456789012"))
	if got < 3.30 || got > 3.35 {
		t.Fatalf("expected entropy in [3.30, 3.35], got %.4f", got)
	}
}

func TestShannonEntropy_EmptyZero(t *testing.T) {
	if got := shannonEntropy(nil); got != 0 {
		t.Fatalf("expected 0 for nil input, got %v", got)
	}
	if got := shannonEntropy([]byte{}); got != 0 {
		t.Fatalf("expected 0 for empty input, got %v", got)
	}
}

func TestValidateINN_Sberbank10(t *testing.T) {
	// Public Sberbank INN — well-known fixture for ФНС checksum.
	if err := validateINN("7707083893"); err != nil {
		t.Fatalf("expected Sberbank INN to validate, got %v", err)
	}
}

func TestValidateINN_BadChecksum10(t *testing.T) {
	if err := validateINN("1234567890"); err == nil {
		t.Fatal("expected checksum failure for 1234567890")
	}
}

func TestValidateINN_WrongLength(t *testing.T) {
	if err := validateINN("770708389"); err == nil {
		t.Fatal("expected length failure for 9-digit input")
	}
	if err := validateINN("77070838933"); err == nil {
		t.Fatal("expected length failure for 11-digit input")
	}
}

func TestValidateINN_Valid12(t *testing.T) {
	// Public 12-digit example — passes both checksum stages.
	if err := validateINN("500100732259"); err != nil {
		t.Fatalf("expected 500100732259 to validate, got %v", err)
	}
}

func TestIsLegalPlaceholder_BracketedRussian(t *testing.T) {
	if !isLegalPlaceholder("[Юридическое лицо — будет обновлено]") {
		t.Fatal("expected bracketed Russian placeholder to match")
	}
}

func TestIsLegalPlaceholder_DashAndTBD(t *testing.T) {
	cases := []string{"—", "-", "TBD", "tbd", "N/A", "n/a", "будет позже"}
	for _, c := range cases {
		if !isLegalPlaceholder(c) {
			t.Fatalf("expected %q to be placeholder", c)
		}
	}
}

func TestIsLegalPlaceholder_Empty(t *testing.T) {
	if !isLegalPlaceholder("") {
		t.Fatal("expected empty string to be placeholder")
	}
	if !isLegalPlaceholder("   ") {
		t.Fatal("expected whitespace-only string to be placeholder")
	}
}

func TestIsLegalPlaceholder_Real(t *testing.T) {
	if isLegalPlaceholder("ООО Реальная компания") {
		t.Fatal("expected real Russian entity name to not match")
	}
}

func TestValidateLegalProduction_AggregatesAllProblems(t *testing.T) {
	cfg := &Config{
		LegalEntityName: "[placeholder]",
		LegalINN:        "",
		LegalAddress:    "—",
		LegalEmailPDN:   "TBD",
	}
	err := validateLegalProduction(cfg)
	if err == nil {
		t.Fatal("expected aggregated error for all-placeholder LEGAL_*")
	}
	msg := err.Error()
	for _, want := range []string{"LEGAL_ENTITY_NAME", "LEGAL_INN", "LEGAL_ADDRESS", "LEGAL_EMAIL_PDN"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected aggregated error to mention %s, got %q", want, msg)
		}
	}
}

func TestValidateLegalProduction_RejectsBadINNChecksum(t *testing.T) {
	cfg := &Config{
		LegalEntityName: "ООО Реальная компания",
		LegalINN:        "1234567890",
		LegalAddress:    "123456 Москва ул. Пушкина д. 1",
		LegalEmailPDN:   "dpo@example.com",
	}
	err := validateLegalProduction(cfg)
	if err == nil {
		t.Fatal("expected checksum failure")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum mention, got %q", err.Error())
	}
}

func TestValidateLegalProduction_RejectsShortName(t *testing.T) {
	cfg := &Config{
		LegalEntityName: "ОО Х",
		LegalINN:        "7707083893",
		LegalAddress:    "123456 Москва ул. Пушкина д. 1",
		LegalEmailPDN:   "dpo@example.com",
	}
	err := validateLegalProduction(cfg)
	if err == nil {
		t.Fatal("expected name length failure")
	}
}

func TestValidateLegalProduction_RejectsShortAddress(t *testing.T) {
	cfg := &Config{
		LegalEntityName: "ООО Реальная компания",
		LegalINN:        "7707083893",
		LegalAddress:    "Короткий адрес",
		LegalEmailPDN:   "dpo@example.com",
	}
	if err := validateLegalProduction(cfg); err == nil {
		t.Fatal("expected address length failure")
	}
}

func TestValidateLegalProduction_RejectsBadEmail(t *testing.T) {
	cfg := &Config{
		LegalEntityName: "ООО Реальная компания",
		LegalINN:        "7707083893",
		LegalAddress:    "123456 Москва ул. Пушкина д. 1",
		LegalEmailPDN:   "not-an-email",
	}
	if err := validateLegalProduction(cfg); err == nil {
		t.Fatal("expected email format failure")
	}
}

func TestValidateLegalProduction_AcceptsRealValues(t *testing.T) {
	cfg := &Config{
		LegalEntityName: "ООО Реальная компания",
		LegalINN:        "7707083893",
		LegalAddress:    "123456 Москва ул. Пушкина д. 1",
		LegalEmailPDN:   "dpo@example.com",
	}
	if err := validateLegalProduction(cfg); err != nil {
		t.Fatalf("expected real values to pass, got %v", err)
	}
}
