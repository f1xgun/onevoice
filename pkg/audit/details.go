package audit

import "github.com/google/uuid"

// Per-action Details structs. One per Action* constant in actions.go.
//
// PII discipline (D-13/D-14/D-15/D-16):
//
//   - Auth events may carry IP, User-Agent, and email — never password or
//     JWT body. Email is intentionally logged on failed-login for
//     brute-force analysis.
//   - Integration events carry platform + external_id only — NEVER token,
//     secret, cookie, or session material.
//   - RBAC events carry IDs; the human-readable names are resolved at read
//     time from the live users/roles tables (not snapshotted).
//   - No request body / form data is logged anywhere.
//
// TestNoSensitiveFields_inDetailsJSON enforces the PII rule by regex over
// JSON keys.

// ---- rbac ---------------------------------------------------------------

// RoleGrantedDetails captures a member's role change. Old/new role IDs are
// recorded so timeline tools can reconstruct trajectory.
type RoleGrantedDetails struct {
	TargetUserID uuid.UUID  `json:"target_user_id"`
	OldRoleID    *uuid.UUID `json:"old_role_id,omitempty"`
	NewRoleID    uuid.UUID  `json:"new_role_id"`
}

// MemberRemovedDetails distinguishes self-removal vs kick-out.
type MemberRemovedDetails struct {
	TargetUserID uuid.UUID `json:"target_user_id"`
	SelfRemoval  bool      `json:"self_removal"`
}

// RoleCreatedDetails captures the permanent record of a custom role's
// initial permission set (custom roles can be edited later).
type RoleCreatedDetails struct {
	RoleID      uuid.UUID `json:"role_id"`
	RoleName    string    `json:"role_name"`
	Permissions []string  `json:"permissions"`
}

// RoleUpdatedDetails captures the post-update permission set (no diff in v1).
type RoleUpdatedDetails struct {
	RoleID      uuid.UUID `json:"role_id"`
	NewName     string    `json:"new_name"`
	Permissions []string  `json:"permissions"`
}

// RoleDeletedDetails captures blast radius via affected user count.
type RoleDeletedDetails struct {
	RoleID        uuid.UUID  `json:"role_id"`
	RoleName      string     `json:"role_name"`
	ReassignedTo  *uuid.UUID `json:"reassigned_to,omitempty"`
	AffectedUsers int        `json:"affected_users"`
}

// InvitationCreatedDetails — NO token or token_hash (D-14 PII rule).
type InvitationCreatedDetails struct {
	InvitationID uuid.UUID `json:"invitation_id"`
	RoleID       uuid.UUID `json:"role_id"`
	ExpiresAt    string    `json:"expires_at"` // RFC3339
}

// InvitationRevokedDetails records the invitation ID being revoked.
type InvitationRevokedDetails struct {
	InvitationID uuid.UUID `json:"invitation_id"`
}

// InvitationAcceptedDetails records which role was granted on acceptance.
type InvitationAcceptedDetails struct {
	InvitationID  uuid.UUID `json:"invitation_id"`
	GrantedRoleID uuid.UUID `json:"granted_role_id"`
}

// ---- auth ---------------------------------------------------------------

// LoginSuccessDetails captures forensic context. NO password, NO JWT body.
type LoginSuccessDetails struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

// LoginFailedDetails records the attempt without a known user_id.
// Email is intentionally logged (D-13) — used for brute-force analysis.
type LoginFailedDetails struct {
	AttemptedEmail string `json:"attempted_email"`
	IP             string `json:"ip"`
	UserAgent      string `json:"user_agent"`
	Reason         string `json:"reason"` // e.g. "invalid_credentials"
}

// LogoutDetails is intentionally empty; the actor+timestamp on the row are
// sufficient.
type LogoutDetails struct{}

// PasswordChangedDetails captures forensic context. NO old or new password.
type PasswordChangedDetails struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

// UserRegisteredDetails records new-account creation context.
type UserRegisteredDetails struct {
	Email     string `json:"email"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

// EmailVerifiedDetails — Phase 21-03 / ACCT-02. No old/new state; the
// AuditLog.UserEmailAtEvent snapshot captures the address that was just
// verified.
type EmailVerifiedDetails struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

// EmailChangedBeforeVerifyDetails captures both addresses across the
// pre-verification change (D-21). OldEmail is the pre-change value; the
// AuditLog.UserEmailAtEvent snapshot already reflects OldEmail (the row is
// written BEFORE the users.email column flips).
type EmailChangedBeforeVerifyDetails struct {
	OldEmail  string `json:"old_email"`
	NewEmail  string `json:"new_email"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

// ConsentRecordedDetails — Phase 21-03 / D-40. Phase 22 will extend with
// policy_sha256 + cross-border consent fields.
type ConsentRecordedDetails struct {
	Purpose       string `json:"purpose"`
	PolicyVersion string `json:"policy_version"`
}

// ---- integration --------------------------------------------------------

// IntegrationConnectedDetails — NEVER store the access/refresh token or any
// session cookie. Only the platform name + the external (provider) id.
type IntegrationConnectedDetails struct {
	IntegrationID uuid.UUID `json:"integration_id"`
	Platform      string    `json:"platform"`
	ExternalID    string    `json:"external_id"`
}

// IntegrationDisconnectedDetails records what was removed; platform is
// captured pre-delete because the row is gone by audit-write time.
type IntegrationDisconnectedDetails struct {
	IntegrationID uuid.UUID `json:"integration_id"`
	Platform      string    `json:"platform"`
}

// IntegrationTokenRotatedDetails — D-14: NO token material. Just the
// integration ID + platform so ops can correlate rotations with refreshes.
type IntegrationTokenRotatedDetails struct {
	IntegrationID uuid.UUID `json:"integration_id"`
	Platform      string    `json:"platform"`
}

// ---- business -----------------------------------------------------------

// BusinessCreatedDetails records the human-readable name at creation time.
type BusinessCreatedDetails struct {
	Name string `json:"name"`
}

// BusinessUpdatedDetails — v1 logs "updated" with no per-field diff
// (Assumption A3). Add a fields slice in a later phase if compliance asks.
type BusinessUpdatedDetails struct{}

// ---- project ------------------------------------------------------------

// ProjectCreatedDetails captures the project ID + human name.
type ProjectCreatedDetails struct {
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
}

// ProjectUpdatedDetails captures the project ID; no per-field diff in v1.
type ProjectUpdatedDetails struct {
	ProjectID uuid.UUID `json:"project_id"`
}

// ProjectDeletedDetails captures blast radius (number of cascaded
// conversations).
type ProjectDeletedDetails struct {
	ProjectID            uuid.UUID `json:"project_id"`
	Name                 string    `json:"name"`
	DeletedConversations int       `json:"deleted_conversations"`
}
