package domain

import "errors"

// User errors
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// Business errors
var (
	ErrBusinessNotFound = errors.New("business not found")
	ErrBusinessExists   = errors.New("business already exists")
)

// Integration errors
var (
	ErrIntegrationNotFound = errors.New("integration not found")
	ErrIntegrationExists   = errors.New("integration already exists")
	ErrTokenExpired        = errors.New("token expired")
)

// Auth errors
var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrInvalidToken  = errors.New("invalid token")
	ErrTokenNotFound = errors.New("token not found")
)

// Password reset errors — Phase 21b (ACCT-01).
//
// PITFALLS §1.1: the repository ConsumeAtomic statement collapses
// (expired | already-consumed | unknown-hash) → ErrResetTokenInvalid so the
// caller cannot distinguish failure modes. ErrResetTokenExpired is kept
// as a separate sentinel for the (currently unused) future "look up first,
// then mutate" path where the expiry IS surfaceable; the live atomic-consume
// path always returns ErrResetTokenInvalid.
//
// ErrResetTokenCollision fires only on the astronomically improbable case
// where 256-bit entropy produces a duplicate of an existing hash. The
// service may retry on this sentinel.
var (
	ErrResetTokenInvalid   = errors.New("password reset token invalid")
	ErrResetTokenExpired   = errors.New("password reset token expired")
	ErrResetTokenCollision = errors.New("password reset token hash collision")
)

// Email verification errors — Phase 21c (ACCT-02).
//
// Same PITFALLS §1.1 collapse as password reset: the atomic-consume
// statement in EmailVerificationTokenRepository.ConsumeAtomic merges
// (expired | already-consumed | unknown-hash) into ErrVerifyTokenInvalid.
// The handler surfaces verify_token_invalid / verify_token_expired by
// running a follow-up "is this row present but expired?" lookup ONLY on
// the invalid branch — kept distinct so the verify-email page can show
// the right copy ("link просрочена" vs "link недействительна").
//
// ErrAlreadyVerified is the guard returned by RequestResend and
// ChangeEmailBeforeVerify when the user already has email_verified=TRUE.
// Maps to HTTP 403 email_already_verified.
//
// ErrResendThrottled signals the Redis 1/min or 5/hr ceiling was hit.
// Maps to HTTP 429 verify_resend_throttled.
//
// ErrEmailTaken signals the new email in PATCH /auth/email-before-verify
// is already used by another user. Maps to HTTP 409 email_taken. Bonus:
// also returned by UpdateEmailInTx on UNIQUE-violation race.
var (
	ErrVerifyTokenInvalid = errors.New("email verification token invalid")
	ErrAlreadyVerified    = errors.New("email already verified")
	ErrResendThrottled    = errors.New("email verification resend throttled")
	ErrEmailTaken         = errors.New("email already used by another account")
)

// Account deletion errors — Phase 21-04 (ACCT-03).
//
// ErrDeletionAlreadyPending fires when a second DELETE /users/me comes in
// while the user already has deletion_requested_at set and not canceled.
// Handler maps to HTTP 423 with body code=account_pending_deletion.
//
// ErrNoDeletionPending fires when POST /users/me/restore is called on a
// user with no pending deletion. Handler maps to HTTP 404
// no_deletion_pending.
//
// ErrAlreadyPurged fires when POST /users/me/restore is called past the
// 30-day grace window — the underlying repository UPDATE matched zero
// rows because either (a) the row was hard-deleted by the sweeper or
// (b) the deletion_requested_at boundary was crossed before this call.
// Handler maps to HTTP 410 deletion_too_old.
var (
	ErrDeletionAlreadyPending = errors.New("account deletion already pending")
	ErrNoDeletionPending      = errors.New("no account deletion pending")
	ErrAlreadyPurged          = errors.New("account deletion grace expired")
)

// Conversation errors
var (
	ErrConversationNotFound = errors.New("conversation not found")
)

// Message errors
var (
	ErrMessageNotFound = errors.New("message not found")
)

// Review errors
var (
	ErrReviewNotFound = errors.New("review not found")
)

// Post errors
var (
	ErrPostNotFound = errors.New("post not found")
)

// AgentTask errors
var (
	ErrAgentTaskNotFound = errors.New("agent task not found")
)

// Project errors
var (
	ErrProjectNotFound            = errors.New("project not found")
	ErrProjectExists              = errors.New("project already exists")
	ErrProjectNameRequired        = errors.New("project name required")
	ErrProjectSystemPromptTooLong = errors.New("project system prompt too long (max 4000 chars)")
	ErrProjectWhitelistEmpty      = errors.New("explicit whitelist must contain at least one tool")
	ErrProjectWhitelistMode       = errors.New("invalid whitelist mode")
)

// Membership errors — RBAC. Returned by BusinessMembershipRepository
// implementations; map pgx.ErrNoRows → ErrMembershipNotFound, duplicate-key →
// ErrMembershipExists.
var (
	ErrMembershipNotFound = errors.New("business membership not found")
	ErrMembershipExists   = errors.New("business membership already exists")
)

// Role errors — RBAC. ErrSystemRoleImmutable is enforced by
// handlers (PATCH/DELETE on a row with is_system=true returns
// HTTP 422 system_role_immutable).
var (
	ErrRoleNotFound        = errors.New("role not found")
	ErrSystemRoleImmutable = errors.New("system role is immutable")

	// ErrRoleNameTaken is returned by RoleRepository.Create / UpdateInTx when the
	// `UNIQUE (business_id, name)` constraint on the roles table fires (Postgres
	// sqlstate 23505). Handler maps this to HTTP 409 role_name_taken.
	ErrRoleNameTaken = errors.New("role name already taken in this business")

	// ErrRoleInUse is returned by Phase 5 RolesHandler.Delete when the role has
	// member_count > 0 and the request lacks `?reassign_to=<otherRoleId>`. Mapped
	// to HTTP 422 role_in_use. The repository never returns this — the handler
	// decides based on CountMembersByRole result.
	ErrRoleInUse = errors.New("role is in use by one or more members")
)

// Invitation errors — RBAC. Handler maps states to HTTP codes:
// ErrInvitationExpired/Revoked/Accepted → 410, ErrAlreadyMember → 409,
// ErrInvitationNotFound → 404 (or treated as 410 via aliasing in handler).
var (
	ErrInvitationNotFound = errors.New("invitation not found")
	ErrInvitationExpired  = errors.New("invitation expired")
	ErrInvitationRevoked  = errors.New("invitation revoked")
	ErrInvitationAccepted = errors.New("invitation already accepted")
	ErrAlreadyMember      = errors.New("user is already a member of this business")
)

// Search sentinels.
//
// ErrInvalidScope is returned by SearchService.Search and the underlying
// repository methods when businessID or userID is empty. Defense-in-depth:
// prevents accidental "search across all tenants" if any
// upstream caller forgets to scope. Callers must NEVER fall back to a
// "default to all" path on this error — surface it as 500 (server-side
// bug) at the handler layer.
//
// ErrSearchIndexNotReady is returned by SearchService.Search while the
// startup-time EnsureSearchIndexes call has not completed. Maps to
// HTTP 503 + Retry-After: 5 in the search handler. Flips to ready via
// Searcher.MarkIndexesReady() in main.go AFTER EnsureSearchIndexes returns nil.
var (
	ErrInvalidScope        = errors.New("search: invalid scope (business_id and user_id required)")
	ErrSearchIndexNotReady = errors.New("search: index not ready")
)
