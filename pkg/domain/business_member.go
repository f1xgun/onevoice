// Package domain — business_member.go
//
// BusinessMember is one row of the business_members table created in
// migration 000006/000005. The PK is composite (business_id, user_id) — the
// table has no synthetic id column. Single-role-per-membership is enforced
// by the PK; multi-role-per-membership is explicitly out of scope (AF-6 in
// REQUIREMENTS.md).
//
// RoleChangedAt / RoleChangedBy are the DATA-08 audit-hook columns —
// nullable in Phase 1; population by handlers happens in Phases 2/5.
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
