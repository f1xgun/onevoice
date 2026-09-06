package domain

import "strings"

// NormalizeEmail returns the canonical account email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
