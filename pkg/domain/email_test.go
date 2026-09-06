package domain

import "testing"

func TestNormalizeEmail(t *testing.T) {
	for _, input := range []string{"owner@example.com", "OWNER@EXAMPLE.COM", "  Owner@Example.COM\t"} {
		if got := NormalizeEmail(input); got != "owner@example.com" {
			t.Errorf("NormalizeEmail(%q) = %q", input, got)
		}
	}
}
