package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PostgreSQL repositories

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	// UpdatePreferredLocale persists the user's UI language choice
	// ('ru' | 'en'). Used by PATCH /auth/locale to sync the
	// cookie-side choice into the row so the value survives across devices.
	// Returns ErrUserNotFound when no row matched (defense-in-depth on top of
	// the authenticated route — the JWT subject *should* always resolve to an
	// existing user, but we surface the row-missing case explicitly rather
	// than silently 204'ing).
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

type BusinessRepository interface {
	Create(ctx context.Context, business *Business) error
	// CreateInTx inserts the business inside a caller-supplied transaction.
	// Used by service.business.Create() to dual-write businesses +
	// business_members atomically (DATA-06, Phase 1 v2.0 RBAC).
	CreateInTx(ctx context.Context, tx pgx.Tx, business *Business) error
	GetByID(ctx context.Context, id uuid.UUID) (*Business, error)
	Update(ctx context.Context, business *Business) error
	// UpdateToolApprovals replaces only settings.tool_approvals on the target
	// business, preserving other keys inside the generic settings JSONB.
	// Feeds PUT /api/v1/business/{id}/tool-approvals.
	UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]ToolFloor) error
}

type BusinessScheduleRepository interface {
	GetByBusinessID(ctx context.Context, businessID uuid.UUID) ([]BusinessSchedule, error)
	Upsert(ctx context.Context, schedule *BusinessSchedule) error
	DeleteByBusinessID(ctx context.Context, businessID uuid.UUID) error
}

type IntegrationRepository interface {
	Create(ctx context.Context, integration *Integration) error
	GetByID(ctx context.Context, id uuid.UUID) (*Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*Integration, error)
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]Integration, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]Integration, error)
	GetByBusinessPlatformExternal(ctx context.Context, businessID uuid.UUID, platform string, externalID string) (*Integration, error)
	ListAllActiveByPlatforms(ctx context.Context, platforms []string) ([]Integration, error)
	Update(ctx context.Context, integration *Integration) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// ProjectRepository is declared in project.go to keep all project-related
// domain types in one file. See pkg/domain/project.go.

// BusinessMembershipRepository — Phase 1 v2.0 RBAC.
//
// The full method surface is declared here so Phases 2/3/5 add
// implementations, not interface churn. Phase 1 implements ONLY Insert and
// GetByBusinessUser (CONTEXT decision D-07); the rest return
// ErrMembershipNotFound or are unimplemented in the Phase-1 repo and will
// be filled in later phases.
//
// Insert takes a pgx.Tx (NOT a pool) because service.business.Create()
// dual-writes businesses + business_members atomically (DATA-06 / D-14).
// Other methods take a context only and use the pool internally.
type BusinessMembershipRepository interface {
	// Insert is transaction-scoped: callers BEGIN a tx, INSERT into
	// businesses, INSERT via this method, then COMMIT both or roll both
	// back. Returns an error wrapping pgx duplicate-key as ErrMembershipExists.
	Insert(ctx context.Context, tx pgx.Tx, m *BusinessMember) error

	// GetByBusinessUser fetches the single membership row for
	// (businessID, userID). Returns ErrMembershipNotFound on no rows.
	GetByBusinessUser(ctx context.Context, businessID, userID uuid.UUID) (*BusinessMember, error)

	// --- Below: declared in Phase 1, IMPLEMENTED in later phases. ---

	// ListByBusiness returns active+suspended members of a business with
	// their role_id. Used by Phase 2 GET /businesses/{id}/members.
	ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]BusinessMember, error)

	// ListByUser returns memberships the user has across businesses. Used
	// by Phase 2 GET /businesses (user-scoped list).
	ListByUser(ctx context.Context, userID uuid.UUID) ([]BusinessMember, error)

	// CountOwnersByBusiness returns the number of active members holding
	// SystemRoleOwnerID for a given business. Phase 1 includes this in the
	// interface because EnsureOwnerExistsAfter (Plan D) calls a wrapped
	// version inside its transaction. The unimplemented stub returns
	// ErrMembershipNotFound until Phase 2.
	CountOwnersByBusiness(ctx context.Context, businessID uuid.UUID) (int, error)

	// UpdateRole changes a membership's role_id and audit columns
	// (role_changed_at/by). Phase 2 wires this from the demote/role-change
	// handler.
	UpdateRole(ctx context.Context, businessID, userID, newRoleID, actorUserID uuid.UUID) error

	// UpdateRoleInTx is the transaction-scoped variant of UpdateRole. Callers
	// supply an open pgx.Tx so the UPDATE executes inside the same transaction
	// as the EnsureOwnerExistsAfter SELECT FOR UPDATE, preserving the
	// RepeatableRead isolation guarantee (CR-01).
	UpdateRoleInTx(ctx context.Context, tx pgx.Tx, businessID, userID, newRoleID, actorUserID uuid.UUID) error

	// Delete removes a membership row. Phase 2 wires this from the
	// remove-member handler.
	Delete(ctx context.Context, businessID, userID uuid.UUID) error

	// DeleteInTx is the transaction-scoped variant of Delete. The DELETE executes
	// on the supplied pgx.Tx so it participates in the caller's RepeatableRead
	// transaction alongside EnsureOwnerExistsAfter's SELECT FOR UPDATE, preserving
	// the isolation guarantee (G-07 fix — same shape as CR-01 for UpdateRoleInTx).
	// Returns domain.ErrMembershipNotFound when no row matched.
	DeleteInTx(ctx context.Context, tx pgx.Tx, businessID, userID uuid.UUID) error

	// ListUserIDsByRole returns the user_id values for every business_members
	// row holding roleID in the given business. Phase 5 RolesHandler.Delete
	// captures this set BEFORE tx.Commit() so it can fanout
	// authz.InvalidateMember per affected user AFTER commit succeeds (Open
	// Question A2: InvalidateRole alone evicts only the role-perms entry, not
	// the per-member membership entry that caches the OLD role_id).
	ListUserIDsByRole(ctx context.Context, businessID, roleID uuid.UUID) ([]uuid.UUID, error)
}

// RoleWithMemberCount augments a Role with the number of business_members
// holding it in a specific business. Used by GET /businesses/{id}/roles for
// the delete-with-reassignment UX (CONTEXT D-08 smart-branching). For system
// roles the count is per-business (the JOIN is filtered by business_id), so
// it reflects "how many local members hold this preset" — not the global
// across-all-businesses count.
type RoleWithMemberCount struct {
	Role
	MemberCount int `json:"member_count"`
}

// RoleRepository — Phase 1 declared the surface; Phase 2 added Get/List
// implementations; Phase 5 adds full CRUD (CONTEXT D-08, ROLE-04..07).
//
// Tx-aware siblings (CreateInTx, UpdateInTx, DeleteInTx, DeleteWithReassignInTx)
// follow the same pattern as BusinessMembershipRepository.UpdateRoleInTx /
// DeleteInTx (Phase 2): the handler opens RepeatableRead, composes the
// invariant check (CheckEscalationSubset / CheckSelfLockout / optional
// EnsureOwnerExistsAfter) and the mutation in one tx, commits, then calls
// authz.InvalidateRole AFTER tx.Commit() (AUTHZ-04 + ROLE-07).
type RoleRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*Role, error)
	ListSystem(ctx context.Context) ([]Role, error)
	ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]Role, error)

	// ListByBusinessWithCounts is the Phase 5 variant returning system + custom
	// roles with member_count populated via LEFT JOIN business_members. Used by
	// GET /businesses/{id}/roles for the new response shape (CONTEXT D-08).
	ListByBusinessWithCounts(ctx context.Context, businessID uuid.UUID) ([]RoleWithMemberCount, error)

	// Create inserts a custom role (is_system=false). Returns ErrRoleNameTaken
	// on UNIQUE (business_id, name) conflict.
	Create(ctx context.Context, role *Role) error
	CreateInTx(ctx context.Context, tx pgx.Tx, role *Role) error

	Update(ctx context.Context, role *Role) error
	// UpdateInTx replaces name/description/permissions/updated_by on a custom
	// role; refuses system rows via `WHERE is_system=false` (returns
	// ErrRoleNotFound for both "missing" and "is_system=true" cases — defense
	// in depth on top of handler-level CheckSystemRoleImmutable).
	UpdateInTx(ctx context.Context, tx pgx.Tx, role *Role) error

	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteInTx removes a custom role with zero members (caller verifies
	// member_count==0 first). Refuses system rows via `WHERE is_system=false`.
	DeleteInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	// DeleteWithReassignInTx reassigns all business_members holding oldRoleID
	// in the given business to reassignToID, then deletes the old role — all
	// in one tx. The reassign-first ordering is REQUIRED by the FK
	// ON DELETE RESTRICT on business_members.role_id. actorUserID is written
	// to business_members.role_changed_by for audit (DATA-08).
	DeleteWithReassignInTx(ctx context.Context, tx pgx.Tx, businessID, oldRoleID, reassignToID, actorUserID uuid.UUID) error

	// Reassign is the non-tx legacy signature retained for compatibility with
	// Phase 1's interface declaration. Phase 5 prefers DeleteWithReassignInTx
	// which composes reassign + delete atomically.
	Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error

	// CountMembersByRole returns the number of active business_members rows
	// with role_id == roleID in the given business. Used by RolesHandler.Delete
	// to branch on the `?reassign_to=` requirement (ROLE-06).
	CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error)

	// GetByMemberInBusiness returns the role for a specific (business, user)
	// pair via JOIN business_members × roles. Used by RolesHandler.MyPermissions
	// if the handler prefers fresh DB lookup over bc.Permissions (which is
	// cache-derived; see 05-RESEARCH.md §GET /me/permissions Option 2). The
	// bias in 05-03 is Option 1 (bc.Permissions), but this method exists so
	// a fresh-lookup variant is one-line away if cache staleness becomes a
	// concern.
	GetByMemberInBusiness(ctx context.Context, businessID, userID uuid.UUID) (*Role, error)
}

// InvitationRepository — Phase 1 declares the surface; Phase 3 implements.
//
// Phase 3 extension (per 03-RESEARCH §"Domain Interface Update"):
//   - CreateInTx + CountPendingByBusinessInTx: needed by the create handler
//     under Serializable isolation so the 20-pending cap holds under
//     concurrent creates (research P-09 / OQ-01).
//   - MarkAcceptedInTx: needed by the accept handler so the conditional
//     UPDATE (race-safe single-use guarantee) runs inside the same
//     RepeatableRead tx as the membership INSERT.
//   - Revoke takes businessID for defense-in-depth cross-tenant scoping
//     (CONTEXT D-11: 404 not_found on cross-tenant revoke; OQ-02 picks
//     interface-level scoping over handler-level pre-check, matching the
//     ConversationRepository.Pin/Unpin convention at lines 170-175).
type InvitationRepository interface {
	Create(ctx context.Context, inv *Invitation) error
	CreateInTx(ctx context.Context, tx pgx.Tx, inv *Invitation) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	ListPendingByBusiness(ctx context.Context, businessID uuid.UUID) ([]Invitation, error)
	CountPendingByBusiness(ctx context.Context, businessID uuid.UUID) (int, error)
	CountPendingByBusinessInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) (int, error)
	Revoke(ctx context.Context, id, businessID uuid.UUID) error
	MarkAccepted(ctx context.Context, id, accepterUserID uuid.UUID) error
	MarkAcceptedInTx(ctx context.Context, tx pgx.Tx, id, accepterUserID uuid.UUID) error
}

// MongoDB repositories

type ConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	GetByID(ctx context.Context, id string) (*Conversation, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]Conversation, error)
	Update(ctx context.Context, conv *Conversation) error
	Delete(ctx context.Context, id string) error
	// UpdateProjectAssignment atomically updates only project_id (+ updated_at).
	// Passing nil clears the assignment ("Без проекта" bucket) — move-chat
	// relies on the `bson:"project_id"` tag (no omitempty) so the
	// Mongo field becomes explicit null rather than missing.
	UpdateProjectAssignment(ctx context.Context, id string, projectID *string) error
	// UpdateTitleIfPending atomically writes title + title_status="auto" only
	// when current status is "auto_pending" or null. Returns ErrConversationNotFound
	// when the filter matches zero docs (manual rename won the race, or doc deleted).
	// Trust-critical path — manual renames MUST NOT be clobbered.
	UpdateTitleIfPending(ctx context.Context, id, title string) error
	// TransitionToAutoPending atomically flips title_status from "auto" or null
	// → "auto_pending". Used by POST /regenerate-title. Returns
	// ErrConversationNotFound when filter matches zero docs (status was "manual"
	// OR "auto_pending" — caller maps each disposition to its 409 body).
	TransitionToAutoPending(ctx context.Context, id string) error
	// Pin atomically sets pinned_at = now (UTC) on the
	// conversation, scoped by (id, business_id, user_id) for defense-in-depth
	// (defends against cross-tenant pin manipulation even if
	// callers misroute IDs). Returns ErrConversationNotFound on mismatch
	// (uniform 404 at the handler layer, never 403, to avoid leaking
	// existence-vs-ownership).
	Pin(ctx context.Context, id, businessID, userID string) error
	// Unpin atomically sets pinned_at = nil on the
	// conversation, scoped by (id, business_id, user_id). Returns
	// ErrConversationNotFound on mismatch.
	Unpin(ctx context.Context, id, businessID, userID string) error
	// SearchTitles runs the $text
	// query against conversations.title scoped by (user_id, business_id,
	// project_id?). Returns title hits AND the slice of matching conversation
	// IDs. Empty businessID or userID returns ErrInvalidScope (cross-tenant
	// defense-in-depth). Result types live in
	// services/api/internal/repository.ConversationTitleHit.
	SearchTitles(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]ConversationTitleHit, []string, error)
	// ScopedConversationIDs returns the conversation IDs visible to (user_id,
	// business_id, project_id?) ordered by last_message_at desc, capped at
	// MaxScopedConversations (overflow logged + truncated). Empty
	// businessID or userID returns ErrInvalidScope.
	ScopedConversationIDs(ctx context.Context, businessID, userID string, projectID *string) ([]string, error)
}

// ConversationTitleHit is the per-row projection returned by
// ConversationRepository.SearchTitles. Mirrors the BSON shape decoded from
// a Find()+SetProjection that includes the $meta:textScore virtual field.
//
// Lives in pkg/domain (not services/api/internal/repository) so the
// interface signature does not import the implementation package — Go's
// "implementations import interfaces, not the other way around" idiom.
type ConversationTitleHit struct {
	ID            string     `bson:"_id"`
	Title         string     `bson:"title"`
	ProjectID     *string    `bson:"project_id"`
	UserID        string     `bson:"user_id"`
	BusinessID    string     `bson:"business_id"`
	Score         float64    `bson:"score"`
	LastMessageAt *time.Time `bson:"last_message_at"`
}

type MessageRepository interface {
	Create(ctx context.Context, msg *Message) error
	ListByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]Message, error)
	CountByConversationID(ctx context.Context, conversationID string) (int64, error)
	// Update overwrites an existing message by ID. Used by the HITL
	// resume path to append ToolResults to the SAME assistant
	// Message that carried the pause-time ToolCalls (one
	// assistant Message per LLM turn, even across a pause). If the message
	// does not exist, returns ErrMessageNotFound.
	Update(ctx context.Context, msg *Message) error
	// FindByConversationActive returns the most recent assistant Message in
	// the conversation whose Status is in {pending_approval, in_progress},
	// or (nil, ErrMessageNotFound) if none exists. Used by chat_proxy.go's
	// stream-open gate to detect in-flight turns before
	// creating a new assistant Message.
	FindByConversationActive(ctx context.Context, conversationID string) (*Message, error)
	// SearchByConversationIDs runs an aggregation pipeline that runs $text
	// on messages.content scoped by
	// the conversation_id allowlist (computed from
	// ConversationRepository.ScopedConversationIDs). Returns one row per
	// conversation: (top_message_id, top_content, top_score, match_count).
	// Empty allowlist returns (nil, nil) without invoking Mongo.
	// Cross-tenant scope is enforced ENTIRELY by the allowlist — Message
	// documents have no business_id field.
	SearchByConversationIDs(ctx context.Context, query string, convIDs []string, limit int) ([]MessageSearchHit, error)
}

// MessageSearchHit is the per-conversation projection produced by the
// SearchByConversationIDs aggregation. ConversationID is the group key
// (the $group stage maps the grouping value into _id). TopMessageID,
// TopContent, TopScore come from $first over the per-message score sort;
// MatchCount counts the messages in the conversation that hit the $text
// query.
type MessageSearchHit struct {
	ConversationID string  `bson:"_id"`
	TopMessageID   string  `bson:"top_message_id"`
	TopContent     string  `bson:"top_content"`
	TopScore       float64 `bson:"top_score"`
	MatchCount     int     `bson:"match_count"`
}

// Filter types

type ReviewFilter struct {
	Platform    string
	ReplyStatus string
	Limit       int
	Offset      int
}

type PostFilter struct {
	Platform string
	Status   string
	Limit    int
	Offset   int
}

type TaskFilter struct {
	Platform string
	Status   string
	Type     string
	Limit    int
	Offset   int
}

// MongoDB repositories for content

type ReviewRepository interface {
	ListByBusinessID(ctx context.Context, businessID string, filter ReviewFilter) ([]Review, int, error)
	GetByID(ctx context.Context, id string) (*Review, error)
	UpdateReply(ctx context.Context, id, replyText, replyStatus string) error
	Upsert(ctx context.Context, review *Review) error

	// ListPendingWithoutDraft returns reviews that need an AI draft —
	// reply_status="pending" AND draft_status in {missing, "", "failed"}.
	// "generating" rows are excluded so concurrent passes don't double-call
	// the LLM. limit caps the per-call work; ordering is created_at desc.
	// Empty platform means "any platform for the business".
	ListPendingWithoutDraft(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	// ListRepliedExamples returns up to limit prior reviews of the same
	// business (and platform when non-empty) that already have a non-empty
	// reply_text and reply_status="replied". Used as few-shot examples for
	// the AI drafter to mirror the owner's tone and length.
	ListRepliedExamples(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	// UpdateDraft writes the four draft_* fields atomically. On status="ready"
	// callers pass the generated text; on "failed" they pass errMsg. On
	// "generating" both are empty strings — used to claim a row before the
	// LLM call.
	UpdateDraft(ctx context.Context, id, draft, status, errMsg string) error
}

type PostRepository interface {
	Create(ctx context.Context, post *Post) error
	ListByBusinessID(ctx context.Context, businessID string, filter PostFilter) ([]Post, int, error)
	GetByID(ctx context.Context, id string) (*Post, error)
}

type AgentTaskRepository interface {
	Create(ctx context.Context, task *AgentTask) error
	Update(ctx context.Context, task *AgentTask) error
	GetByID(ctx context.Context, businessID, taskID string) (*AgentTask, error)
	ListByBusinessID(ctx context.Context, businessID string, filter TaskFilter) ([]AgentTask, int, error)
}

// --- HITL — pending tool-call batches ---

// PendingToolCallBatch is the persisted snapshot of a paused multi-tool
// approval batch: one document per assistant turn that hit ≥1 manual-floor
// tool. Written by services/orchestrator at pause time (status="preparing"),
// promoted to "pending" just before the SSE tool_approval_required event is
// flushed, transitioned to "resolving" atomically by the resolve endpoint,
// then "resolved" after all decisions are recorded. Expired batches are
// swept by the Mongo TTL index on ExpiresAt.
//
// ProjectID is nullable (bson:",omitempty") because conversations may not be
// scoped to any project (the virtual "Без проекта" bucket).
// When present, it is the key used to look up the
// project's approval_overrides for the TOCTOU re-check.
type PendingToolCallBatch struct {
	ID             string        `bson:"_id"`
	ConversationID string        `bson:"conversation_id"`
	BusinessID     string        `bson:"business_id"`
	ProjectID      string        `bson:"project_id,omitempty"`
	UserID         string        `bson:"user_id"`
	MessageID      string        `bson:"message_id"`
	Status         string        `bson:"status"` // "preparing" | "pending" | "resolving" | "resolved" | "expired"
	Calls          []PendingCall `bson:"calls"`
	ModelMessages  []byte        `bson:"model_messages"` // JSON-serialized []llm.Message snapshot
	IterationIdx   int           `bson:"iteration_idx"`
	CreatedAt      time.Time     `bson:"created_at"`
	UpdatedAt      time.Time     `bson:"updated_at"`
	ExpiresAt      time.Time     `bson:"expires_at"`
}

// PendingCall is a single proposed tool invocation within a batch. CallID is
// the LLM's real tool_call.id (no synthetic "tc-N" placeholder).
// Verdict/EditedArgs/RejectReason are populated by the resolve endpoint.
// Dispatched is the orchestrator-side double-execution guard:
// on resume, any entry with Dispatched=true is skipped.
type PendingCall struct {
	CallID    string                 `bson:"call_id"`
	ToolName  string                 `bson:"tool_name"`
	Arguments map[string]interface{} `bson:"arguments"`

	// FloorAtPause is the effective ToolFloor at the moment the orchestrator
	// paused the turn for this call. Persisted so the resolve-time TOCTOU
	// re-check can consult the same registry that classified the call at
	// pause time, eliminating divergence between the orchestrator's
	// in-process tools.Registry (always warm) and the api's
	// service.ToolsRegistryCache (HTTP-backed, lazily warmed).
	//
	// For pause-time-persisted calls this is always ToolFloorManual (only
	// manual-floor calls reach the orchestrator's pause path; auto and
	// forbidden are bucketed elsewhere). bson:",omitempty" so legacy
	// batches decode with FloorAtPause == ""
	// (ToolFloorRank returns -1 for invalid values, so an empty floor
	// cannot dominate a valid business/project override — strictest-wins
	// still detects a post-pause forbidden flip; the orchestrator-side
	// TOCTOU recheck remains the load-bearing primitive for safety).
	FloorAtPause ToolFloor `bson:"floor_at_pause,omitempty"`

	Verdict      string                 `bson:"verdict,omitempty"` // "approve" | "edit" | "reject"
	EditedArgs   map[string]interface{} `bson:"edited_args,omitempty"`
	RejectReason string                 `bson:"reject_reason,omitempty"`
	Dispatched   bool                   `bson:"dispatched"`
	DispatchedAt *time.Time             `bson:"dispatched_at,omitempty"`
}

// PendingToolCallRepository is implemented by services/api. The
// interface declares every primitive that the orchestrator (at pause time),
// the resolve handler (at decision time), and the chat_proxy (at SSE emission
// time) need — no type assertions, no out-of-band helpers.
//
// Atomicity discipline: because MongoDB in this deployment is STANDALONE (no
// multi-document transactions), all cross-document
// consistency is encoded as a strict write-order:
//
//	InsertPreparing → PromoteToPending → emit SSE
//	↓ (crash here → ReconcileOrphanPreparing sweeps after olderThan)
//	AtomicTransitionToResolving → RecordDecisions → MarkDispatched* → MarkResolved
//
// AtomicTransitionToResolving uses findOneAndUpdate with filter
// `{_id, status: "pending"}` and update `{$set: {status: "resolving"}}` to
// guarantee exactly-one-wins on concurrent resolve attempts.
type PendingToolCallRepository interface {
	InsertPreparing(ctx context.Context, b *PendingToolCallBatch) error
	PromoteToPending(ctx context.Context, batchID string) error
	GetByBatchID(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	ListPendingByConversation(ctx context.Context, conversationID string) ([]*PendingToolCallBatch, error)
	AtomicTransitionToResolving(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	RecordDecisions(ctx context.Context, batchID string, calls []PendingCall) error
	MarkDispatched(ctx context.Context, batchID, callID string) error
	MarkResolved(ctx context.Context, batchID string) error
	MarkExpired(ctx context.Context, batchID string) error
	ReconcileOrphanPreparing(ctx context.Context, olderThan time.Duration) (int64, error)
}
