package domain

// Sentinel errors returned by repositories and services. The strings below are
// surfaced (often via a stable wire code at the handler layer) and form part
// of the public API contract — changing one is a breaking API change. See
// docs/domain/errors.md for the full catalog (HTTP status, wire code,
// triggers).

import "errors"

// User errors.
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// Business errors.
var (
	ErrBusinessNotFound = errors.New("business not found")
	ErrBusinessExists   = errors.New("business already exists")
)

// Integration errors.
var (
	ErrIntegrationNotFound = errors.New("integration not found")
	ErrIntegrationExists   = errors.New("integration already exists")
	ErrTokenExpired        = errors.New("token expired")
)

// Auth errors.
var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrInvalidToken  = errors.New("invalid token")
	ErrTokenNotFound = errors.New("token not found")
)

// Password reset errors. ConsumeAtomic collapses
// (expired | already-consumed | unknown-hash) → ErrResetTokenInvalid to avoid
// a timing oracle; ErrResetTokenExpired is reserved for a non-atomic future
// path. See docs/domain/errors.md ("Reset / verify atomic-consume collapse").
var (
	ErrResetTokenInvalid   = errors.New("password reset token invalid")
	ErrResetTokenExpired   = errors.New("password reset token expired")
	ErrResetTokenCollision = errors.New("password reset token hash collision")
)

// Email verification errors. ConsumeAtomic collapses the same three failure
// modes as password reset into ErrVerifyTokenInvalid; the handler runs a
// follow-up "row present but expired?" lookup to distinguish copy. See
// docs/domain/errors.md.
var (
	ErrVerifyTokenInvalid = errors.New("email verification token invalid")
	ErrAlreadyVerified    = errors.New("email already verified")
	ErrResendThrottled    = errors.New("email verification resend throttled")
	ErrEmailTaken         = errors.New("email already used by another account")
)

// Consent errors.
var (
	ErrConsentMissing         = errors.New("consent missing or stale version")
	ErrConsentVersionMismatch = errors.New("consent version mismatch (operator bumped policy mid-review)")
)

// Account deletion errors.
var (
	ErrDeletionAlreadyPending = errors.New("account deletion already pending")
	ErrNoDeletionPending      = errors.New("no account deletion pending")
	ErrAlreadyPurged          = errors.New("account deletion grace expired")
)

// Conversation errors.
var (
	ErrConversationNotFound = errors.New("conversation not found")
)

// Message errors.
var (
	ErrMessageNotFound = errors.New("message not found")
)

// Review errors.
var (
	ErrReviewNotFound = errors.New("review not found")
)

// Post errors.
var (
	ErrPostNotFound = errors.New("post not found")
)

// AgentTask errors.
var (
	ErrAgentTaskNotFound = errors.New("agent task not found")
)

// Project errors.
var (
	ErrProjectNotFound            = errors.New("project not found")
	ErrProjectExists              = errors.New("project already exists")
	ErrProjectNameRequired        = errors.New("project name required")
	ErrProjectSystemPromptTooLong = errors.New("project system prompt too long (max 4000 chars)")
	ErrProjectWhitelistEmpty      = errors.New("explicit whitelist must contain at least one tool")
	ErrProjectWhitelistMode       = errors.New("invalid whitelist mode")
)

// Membership errors (RBAC). Returned by BusinessMembershipRepository
// implementations: map pgx.ErrNoRows → ErrMembershipNotFound, duplicate-key →
// ErrMembershipExists.
var (
	ErrMembershipNotFound = errors.New("business membership not found")
	ErrMembershipExists   = errors.New("business membership already exists")
)

// Role errors (RBAC). ErrRoleInUse is decided by RolesHandler.Delete from
// CountMembersByRole, not by the repository. See docs/domain/errors.md.
var (
	ErrRoleNotFound        = errors.New("role not found")
	ErrSystemRoleImmutable = errors.New("system role is immutable")
	ErrRoleNameTaken       = errors.New("role name already taken in this business")
	ErrRoleInUse           = errors.New("role is in use by one or more members")
)

// Invitation errors (RBAC).
var (
	ErrInvitationNotFound = errors.New("invitation not found")
	ErrInvitationExpired  = errors.New("invitation expired")
	ErrInvitationRevoked  = errors.New("invitation revoked")
	ErrInvitationAccepted = errors.New("invitation already accepted")
	ErrAlreadyMember      = errors.New("user is already a member of this business")
)

// Search sentinels. ErrInvalidScope is defense-in-depth against accidental
// cross-tenant search — callers must NEVER fall back to "default to all" on
// this error; surface it as 500.
var (
	ErrInvalidScope        = errors.New("search: invalid scope (business_id and user_id required)")
	ErrSearchIndexNotReady = errors.New("search: index not ready")
)
