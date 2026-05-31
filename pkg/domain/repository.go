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
	// GetByID filters `deleted_at IS NULL`; soft-deleted users look like
	// ErrUserNotFound. Deletion-aware paths use GetByIDIncludingDeleted.
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	// GetByIDIncludingDeleted returns the row even if deleted_at IS NOT NULL.
	// /auth/me uses this so users in the 30-day grace window can restore.
	GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	// UpdatePreferredLocale persists the user's UI language ('ru'|'en').
	// Returns ErrUserNotFound when no row matched (defense-in-depth).
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

type BusinessRepository interface {
	Create(ctx context.Context, business *Business) error
	// CreateInTx inserts inside a caller-supplied tx so businesses +
	// business_members can be dual-written atomically.
	CreateInTx(ctx context.Context, tx pgx.Tx, business *Business) error
	GetByID(ctx context.Context, id uuid.UUID) (*Business, error)
	Update(ctx context.Context, business *Business) error
	// UpdateToolApprovals replaces only settings.tool_approvals, preserving
	// other keys inside the settings JSONB.
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

// BusinessMembershipRepository — v2.0 RBAC.
//
// Insert takes a pgx.Tx (NOT a pool) so service.business.Create can
// dual-write businesses + business_members atomically. Other methods take
// only ctx and use the pool internally.
type BusinessMembershipRepository interface {
	// Insert is transaction-scoped. Wraps pgx duplicate-key as ErrMembershipExists.
	Insert(ctx context.Context, tx pgx.Tx, m *BusinessMember) error

	// GetByBusinessUser fetches the membership for (businessID, userID).
	// Returns ErrMembershipNotFound on no rows.
	GetByBusinessUser(ctx context.Context, businessID, userID uuid.UUID) (*BusinessMember, error)

	ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]BusinessMember, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]BusinessMember, error)

	// CountOwnersByBusiness returns count of active members holding
	// SystemRoleOwnerID — called inside EnsureOwnerExistsAfter's tx.
	CountOwnersByBusiness(ctx context.Context, businessID uuid.UUID) (int, error)

	UpdateRole(ctx context.Context, businessID, userID, newRoleID, actorUserID uuid.UUID) error

	// UpdateRoleInTx runs the UPDATE inside the caller's tx so it shares the
	// RepeatableRead isolation guarantee with EnsureOwnerExistsAfter's
	// SELECT FOR UPDATE.
	UpdateRoleInTx(ctx context.Context, tx pgx.Tx, businessID, userID, newRoleID, actorUserID uuid.UUID) error

	Delete(ctx context.Context, businessID, userID uuid.UUID) error

	// DeleteInTx is the tx-scoped variant of Delete — same isolation reason
	// as UpdateRoleInTx. Returns domain.ErrMembershipNotFound on no row.
	DeleteInTx(ctx context.Context, tx pgx.Tx, businessID, userID uuid.UUID) error

	// ListUserIDsByRole returns user_ids of members holding roleID in the
	// business. RolesHandler.Delete captures this BEFORE tx.Commit so it can
	// fan out authz.InvalidateMember per affected user AFTER commit —
	// InvalidateRole alone leaves stale per-member entries cached.
	ListUserIDsByRole(ctx context.Context, businessID, roleID uuid.UUID) ([]uuid.UUID, error)
}

// RoleWithMemberCount augments a Role with the per-business member count for
// the delete-with-reassignment UX. For system roles the count is per-business.
type RoleWithMemberCount struct {
	Role
	MemberCount int `json:"member_count"`
}

// RoleRepository covers full CRUD.
//
// Tx-aware siblings (CreateInTx, UpdateInTx, DeleteInTx,
// DeleteWithReassignInTx) follow the membership pattern: handler opens
// RepeatableRead, composes invariant check + mutation in one tx, commits,
// then calls authz.InvalidateRole AFTER tx.Commit.
type RoleRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*Role, error)
	ListSystem(ctx context.Context) ([]Role, error)
	ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]Role, error)
	ListByBusinessWithCounts(ctx context.Context, businessID uuid.UUID) ([]RoleWithMemberCount, error)

	// Create inserts a custom role (is_system=false). ErrRoleNameTaken on
	// UNIQUE (business_id, name) conflict.
	Create(ctx context.Context, role *Role) error
	CreateInTx(ctx context.Context, tx pgx.Tx, role *Role) error

	Update(ctx context.Context, role *Role) error
	// UpdateInTx refuses system rows via WHERE is_system=false (returns
	// ErrRoleNotFound for both "missing" and "is_system=true" cases).
	UpdateInTx(ctx context.Context, tx pgx.Tx, role *Role) error

	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteInTx removes a custom role with zero members (caller must verify).
	// Refuses system rows via WHERE is_system=false.
	DeleteInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	// DeleteWithReassignInTx reassigns then deletes in one tx. Reassign-first
	// is REQUIRED by the FK ON DELETE RESTRICT on business_members.role_id.
	DeleteWithReassignInTx(ctx context.Context, tx pgx.Tx, businessID, oldRoleID, reassignToID, actorUserID uuid.UUID) error

	// Reassign is the non-tx legacy signature; prefer DeleteWithReassignInTx.
	Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error

	CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error)

	// GetByMemberInBusiness returns the role for (business, user) via JOIN —
	// fresh-lookup variant for MyPermissions if cache staleness becomes a concern.
	GetByMemberInBusiness(ctx context.Context, businessID, userID uuid.UUID) (*Role, error)
}

// InvitationRepository:
//   - CreateInTx + CountPendingByBusinessInTx: create runs under Serializable
//     so the 20-pending cap holds under concurrent creates.
//   - MarkAcceptedInTx: race-safe single-use guarantee — must share the tx
//     with the membership INSERT.
//   - Revoke takes businessID for cross-tenant scoping (404 on mismatch).
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

// AuditLogFilter is the filter set for AuditLogRepository.ListByBusiness.
// Empty strings / nil pointers mean "no filter". Cursor fields (CursorTime
// + CursorID) are paired: both nil = first page. Limit capped at 200 by the handler.
type AuditLogFilter struct {
	Category   string     // "" | "rbac" | "auth" | "integration" | "business" | "project"
	Action     string     // "" | exact match e.g. "rbac.role_granted"
	ActorID    *uuid.UUID // nil = any actor; set = exact user_id match
	From       *time.Time // nil = no lower bound
	To         *time.Time // nil = no upper bound
	CursorTime *time.Time // tie-break by CursorID at the same created_at
	CursorID   *uuid.UUID
	Limit      int // handler enforces 1..200; default 50
}

// AuditLogRepository persists and queries audit_logs.
//
// Insert must be safe with BusinessID == nil and UserID == nil
// (failed-login entries).
//
// ListByBusiness orders by (created_at DESC, id DESC); caller passes the
// last row's tuple as cursor.
//
// DeleteOlderThan runs inside the retention sweep's advisory-lock window.
type AuditLogRepository interface {
	Insert(ctx context.Context, log *AuditLog) error
	ListByBusiness(ctx context.Context, businessID uuid.UUID, filter AuditLogFilter) ([]AuditLog, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// MongoDB repositories

type ConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	GetByID(ctx context.Context, id string) (*Conversation, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]Conversation, error)
	Update(ctx context.Context, conv *Conversation) error
	Delete(ctx context.Context, id string) error
	// UpdateProjectAssignment sets project_id only. nil clears the assignment
	// — relies on bson:"project_id" (no omitempty) so the field becomes
	// explicit null rather than missing.
	UpdateProjectAssignment(ctx context.Context, id string, projectID *string) error
	// UpdateTitleIfPending writes title + title_status="auto" only when current
	// status is "auto_pending" or null. ErrConversationNotFound when filter
	// matches zero docs. Trust-critical — manual renames MUST NOT be clobbered.
	UpdateTitleIfPending(ctx context.Context, id, title string) error
	// TransitionToAutoPending flips title_status from "auto"/null →
	// "auto_pending". ErrConversationNotFound when status was "manual" or
	// already "auto_pending" — caller maps each disposition to its 409 body.
	TransitionToAutoPending(ctx context.Context, id string) error
	// Pin sets pinned_at=now scoped by (id, business_id, user_id) — defends
	// against cross-tenant pin manipulation. Returns ErrConversationNotFound
	// on mismatch (uniform 404, never 403, to avoid existence leak).
	Pin(ctx context.Context, id, businessID, userID string) error
	// Unpin clears pinned_at, scoped by (id, business_id, user_id).
	Unpin(ctx context.Context, id, businessID, userID string) error
	// SearchTitles runs $text against conversations.title scoped by
	// (user_id, business_id, project_id?). Empty businessID/userID returns
	// ErrInvalidScope.
	SearchTitles(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]ConversationTitleHit, []string, error)
	// ScopedConversationIDs returns conversation IDs visible to scope, capped
	// at MaxScopedConversations. Empty businessID/userID returns ErrInvalidScope.
	ScopedConversationIDs(ctx context.Context, businessID, userID string, projectID *string) ([]string, error)
	// MongoConversationsCleanup sets user_id=null + email_at_delete on each
	// conversation owned by a deleted user. Does NOT delete documents
	// (business-level history stays intact). Best-effort post-PG-TX (PG is
	// source of truth; Mongo failure logged as warning).
	MongoConversationsCleanup(ctx context.Context, userID string, originalEmail string) (int64, error)
}

// ConversationTitleHit is the per-row projection from SearchTitles. Lives in
// pkg/domain so the interface signature does not import the implementation
// package (implementations import interfaces, not vice versa).
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
	// Update overwrites a message by ID. HITL resume uses this to append
	// ToolResults to the SAME assistant Message that carried the pause-time
	// ToolCalls (one assistant Message per LLM turn, even across a pause).
	Update(ctx context.Context, msg *Message) error
	// FindByConversationActive returns the most recent assistant Message
	// with Status in {pending_approval, in_progress}, or ErrMessageNotFound.
	// chat_proxy's stream-open gate uses this to detect in-flight turns.
	FindByConversationActive(ctx context.Context, conversationID string) (*Message, error)
	// SearchByConversationIDs runs $text on messages.content scoped by the
	// conversation_id allowlist. Empty allowlist returns (nil, nil) without
	// invoking Mongo. Cross-tenant scope is enforced ENTIRELY by the
	// allowlist — Message documents have no business_id field.
	SearchByConversationIDs(ctx context.Context, query string, convIDs []string, limit int) ([]MessageSearchHit, error)
}

// MessageSearchHit is the per-conversation projection from SearchByConversationIDs.
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

	// ListPendingWithoutDraft returns reviews needing an AI draft. "generating"
	// rows are excluded so concurrent passes don't double-call the LLM.
	ListPendingWithoutDraft(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	// ListRepliedExamples returns prior replied reviews as few-shot examples
	// so the drafter mirrors the owner's tone and length.
	ListRepliedExamples(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	// UpdateDraft writes the four draft_* fields atomically. status="generating"
	// (with empty draft+errMsg) claims a row before the LLM call.
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
// approval batch: one doc per assistant turn that hit ≥1 manual-floor tool.
// Lifecycle: orchestrator writes status="preparing", promotes to "pending"
// before flushing the SSE tool_approval_required event; the resolve endpoint
// atomically transitions to "resolving" then "resolved". Expired batches are
// swept by the Mongo TTL index on ExpiresAt.
//
// ProjectID is nullable — conversations may not be scoped to any project.
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
// the LLM's real tool_call.id. Verdict/EditedArgs/RejectReason are written by
// the resolve endpoint. Dispatched is the orchestrator-side double-execution
// guard — on resume, entries with Dispatched=true are skipped.
type PendingCall struct {
	CallID    string                 `bson:"call_id"`
	ToolName  string                 `bson:"tool_name"`
	Arguments map[string]interface{} `bson:"arguments"`

	// FloorAtPause is the effective ToolFloor at the pause moment, persisted
	// so the resolve-time TOCTOU re-check uses the same registry that classified
	// the call at pause — avoids divergence between the orchestrator's warm
	// in-process Registry and the api's lazily-warmed ToolsRegistryCache.
	// Always ToolFloorManual for pause-time-persisted calls. omitempty so
	// legacy batches decode with FloorAtPause == "".
	FloorAtPause ToolFloor `bson:"floor_at_pause,omitempty"`

	Verdict      string                 `bson:"verdict,omitempty"` // "approve" | "edit" | "reject"
	EditedArgs   map[string]interface{} `bson:"edited_args,omitempty"`
	RejectReason string                 `bson:"reject_reason,omitempty"`
	Dispatched   bool                   `bson:"dispatched"`
	DispatchedAt *time.Time             `bson:"dispatched_at,omitempty"`
}

// PendingToolCallRepository is implemented by services/api.
//
// Atomicity discipline: Mongo is STANDALONE (no multi-doc transactions), so
// cross-document consistency is encoded as a strict write-order:
//
//	Persist → emit SSE
//	↓ (crash mid-Persist → ReconcileOrphanPreparing sweeps after olderThan)
//	AtomicTransitionToResolving → RecordDecisions → MarkDispatched* → MarkResolved
//
// Persist stages "preparing" then promotes to "pending" with TTL set; the
// preparing window exists only so a crash can be reaped instead of leaving a
// row ticking toward premature TTL deletion. Callers never observe it.
//
// AtomicTransitionToResolving uses findOneAndUpdate with filter
// `{_id, status: "pending"}` to guarantee exactly-one-wins on concurrent resolves.
type PendingToolCallRepository interface {
	Persist(ctx context.Context, b *PendingToolCallBatch) error
	GetByBatchID(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	ListPendingByConversation(ctx context.Context, conversationID string) ([]*PendingToolCallBatch, error)
	AtomicTransitionToResolving(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	RecordDecisions(ctx context.Context, batchID string, calls []PendingCall) error
	MarkDispatched(ctx context.Context, batchID, callID string) error
	MarkResolved(ctx context.Context, batchID string) error
	MarkExpired(ctx context.Context, batchID string) error
	ReconcileOrphanPreparing(ctx context.Context, olderThan time.Duration) (int64, error)
}
