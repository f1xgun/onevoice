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

// Organization (business) deletion errors. Mirror the account-deletion
// sentinels — same lifecycle, different resource.
var (
	ErrBusinessDeletionAlreadyPending = errors.New("organization deletion already pending")
	ErrNoBusinessDeletionPending      = errors.New("no organization deletion pending")
	ErrBusinessAlreadyPurged          = errors.New("organization deletion grace expired")
	// ErrNotBusinessOwner gates DELETE/restore to members holding the system
	// OWNER role; surfaces as 403 at the handler boundary.
	ErrNotBusinessOwner = errors.New("not organization owner")
)

// Integration errors.
var (
	ErrIntegrationNotFound = errors.New("integration not found")
	ErrIntegrationExists   = errors.New("integration already exists")
	ErrTokenExpired        = errors.New("token expired")
	// ErrServiceUnavailable is returned when a downstream resource (e.g. the
	// OAuth advisory lock) is exhausted or temporarily unavailable.
	ErrServiceUnavailable = errors.New("service unavailable")
	// ErrActorEmailNotVerified is returned by Connect when the acting user's
	// email is unverified. Mirrors the RequireVerifiedEmailDay0 middleware so
	// the public OAuth callbacks (which sit outside that gate) cannot persist a
	// live integration for an unverified actor.
	ErrActorEmailNotVerified = errors.New("actor email not verified")
	// ErrActorPendingDeletion is returned by Connect when the acting user is
	// inside the account-deletion grace window. Mirrors the
	// BlockWritesDuringGrace middleware so the public OAuth callbacks cannot
	// persist a live integration for a user mid-deletion.
	ErrActorPendingDeletion = errors.New("actor account pending deletion")
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
	ErrProjectNameTooLong         = errors.New("project name too long (max 200 chars)")
	ErrProjectDescriptionTooLong  = errors.New("project description too long (max 2000 chars)")
	ErrProjectTooManyAllowedTools = errors.New("project has too many allowed tools")
	ErrProjectAllowedToolTooLong  = errors.New("project allowed tool name too long")
	ErrProjectTooManyQuickActions = errors.New("project has too many quick actions")
	ErrProjectQuickActionTooLong  = errors.New("project quick action too long")
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
