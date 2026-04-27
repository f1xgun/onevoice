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

// Phase 19 search sentinels (Plan 19-03 / SEARCH-02 + SEARCH-06).
//
// ErrInvalidScope is returned by SearchService.Search and the underlying
// repository methods when businessID or userID is empty. Defense-in-depth
// (Pitfalls §19): prevents accidental "search across all tenants" if any
// upstream caller forgets to scope. Callers must NEVER fall back to a
// "default to all" path on this error — surface it as 500 (server-side
// bug) at the handler layer.
//
// ErrSearchIndexNotReady is returned by SearchService.Search while the
// startup-time EnsureSearchIndexes call has not completed. Maps to
// HTTP 503 + Retry-After: 5 in the search handler (T-19-INDEX-503
// mitigation). Flips to ready via Searcher.MarkIndexesReady() in main.go
// AFTER EnsureSearchIndexes returns nil.
var (
	ErrInvalidScope        = errors.New("search: invalid scope (business_id and user_id required)")
	ErrSearchIndexNotReady = errors.New("search: index not ready")
)
