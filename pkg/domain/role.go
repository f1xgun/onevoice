// Package domain — role.go
//
// Role is one row of the roles table created in migration 000007/000005.
// BusinessID is nullable (NULL = system preset; non-NULL = business-custom
// role). Permissions is the flat resource.action JSONB array (see
// pkg/authz/permissions.go for the typed registry). IsSystem=true rows are
// immutable; enforcement is application-side in Phases 2/5.
//
// CreatedBy / UpdatedBy are the DATA-08 audit-hook columns — nullable,
// populated by handlers in Phase 5.
//
// NOTE: The canonical Role identifier was freed up in Phase 1 by renaming
// the legacy users.role enum (formerly type Role string) to UserRole — see
// pkg/domain/roles.go header comment. Phase 6 (CLEAN-03) deletes the
// legacy enum file along with the users.role column.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Role struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	BusinessID  *uuid.UUID `json:"businessId,omitempty" db:"business_id"`
	Name        string     `json:"name" db:"name"`
	Description string     `json:"description" db:"description"`
	Permissions []string   `json:"permissions" db:"permissions"`
	IsSystem    bool       `json:"isSystem" db:"is_system"`
	CreatedBy   *uuid.UUID `json:"createdBy,omitempty" db:"created_by"`
	UpdatedBy   *uuid.UUID `json:"updatedBy,omitempty" db:"updated_by"`
	CreatedAt   time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time  `json:"updatedAt" db:"updated_at"`
}
