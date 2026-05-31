// TokenHash is sha256(rawToken) — the raw token is returned to the inviter
// ONCE at creation time and never stored. Lookup uses crypto/subtle.ConstantTimeCompare.
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
