// Package repository — helpers.go
//
// Tiny encoder helpers shared across repositories that need to translate Go
// zero values into SQL NULLs. The pgx driver encodes uuid.Nil as the literal
// '00000000-0000-0000-0000-000000000000', NOT as NULL — direct passing of
// uuid.Nil would defeat the usage_logs.user_id ON DELETE SET NULL contract
// (the FK target row would be the zero-UUID, not a missing row). Returning
// `any` lets squirrel/pgx see an untyped nil and emit a real NULL.

package repository

import (
	"github.com/google/uuid"
)

// nullableUUID returns nil when u is uuid.Nil so pgx encodes the column as SQL
// NULL. Otherwise returns the UUID by value. Used by billing.go LogUsage for
// usage_logs.user_id (system-level callers pass uuid.Nil).
func nullableUUID(u uuid.UUID) any {
	if u == uuid.Nil {
		return nil
	}
	return u
}

// nullableString returns nil when s is "" so pgx encodes the column as SQL
// NULL. Otherwise returns the string. Used for usage_logs.conversation_id and
// usage_logs.request_id, where the empty string is semantically "absent".
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
