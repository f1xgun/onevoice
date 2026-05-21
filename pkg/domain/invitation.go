// Package domain — invitation.go
//
// Invitation is one row of the invitations table created in migration
// 000007/000005. TokenHash is sha256(rawToken) — the raw token is returned
// to the inviter ONCE at creation time (Phase 3) and never stored. Lookup
// uses crypto/subtle.ConstantTimeCompare (also Phase 3).
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Invitation struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	BusinessID uuid.UUID  `json:"businessId" db:"business_id"`
	RoleID     uuid.UUID  `json:"roleId" db:"role_id"`
	TokenHash  string     `json:"-" db:"token_hash"` // never expose hash via JSON
	ExpiresAt  time.Time  `json:"expiresAt" db:"expires_at"`
	AcceptedAt *time.Time `json:"acceptedAt,omitempty" db:"accepted_at"`
	AcceptedBy *uuid.UUID `json:"acceptedBy,omitempty" db:"accepted_by"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty" db:"revoked_at"`
	CreatedBy  uuid.UUID  `json:"createdBy" db:"created_by"`
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
}
