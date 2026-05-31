// PK is composite (business_id, user_id); no synthetic id column. The composite PK
// enforces single-role-per-membership; multi-role-per-membership is out of scope.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type BusinessMember struct {
	BusinessID    uuid.UUID  `json:"businessId" db:"business_id"`
	UserID        uuid.UUID  `json:"userId" db:"user_id"`
	RoleID        uuid.UUID  `json:"roleId" db:"role_id"`
	Status        string     `json:"status" db:"status"` // 'active' | 'suspended'
	InvitedBy     *uuid.UUID `json:"invitedBy,omitempty" db:"invited_by"`
	InvitedAt     *time.Time `json:"invitedAt,omitempty" db:"invited_at"`
	JoinedAt      time.Time  `json:"joinedAt" db:"joined_at"`
	RoleChangedAt *time.Time `json:"roleChangedAt,omitempty" db:"role_changed_at"`
	RoleChangedBy *uuid.UUID `json:"roleChangedBy,omitempty" db:"role_changed_by"`
}
