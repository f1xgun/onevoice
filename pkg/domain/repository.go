package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PostgreSQL repositories

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
}

type BusinessRepository interface {
	Create(ctx context.Context, business *Business) error
	GetByID(ctx context.Context, id uuid.UUID) (*Business, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*Business, error)
	Update(ctx context.Context, business *Business) error
	// UpdateToolApprovals replaces only settings.tool_approvals on the target
	// business, preserving other keys inside the generic settings JSONB.
	// Phase 16 (POLICY-05): feeds PUT /api/v1/business/{id}/tool-approvals.
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

// MongoDB repositories

type ConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	GetByID(ctx context.Context, id string) (*Conversation, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]Conversation, error)
	Update(ctx context.Context, conv *Conversation) error
	Delete(ctx context.Context, id string) error
	// UpdateProjectAssignment atomically updates only project_id (+ updated_at).
	// Passing nil clears the assignment ("Без проекта" bucket) — move-chat in
	// Plan 15-04 relies on the `bson:"project_id"` tag (no omitempty) so the
	// Mongo field becomes explicit null rather than missing.
	UpdateProjectAssignment(ctx context.Context, id string, projectID *string) error
	// UpdateTitleIfPending atomically writes title + title_status="auto" only
	// when current status is "auto_pending" or null. Returns ErrConversationNotFound
	// when the filter matches zero docs (manual rename won the race, or doc deleted).
	// TITLE-04 / D-08: trust-critical path — manual renames MUST NOT be clobbered.
	UpdateTitleIfPending(ctx context.Context, id, title string) error
	// TransitionToAutoPending atomically flips title_status from "auto" or null
	// → "auto_pending". Used by POST /regenerate-title (Plan 05). Returns
	// ErrConversationNotFound when filter matches zero docs (status was "manual"
	// OR "auto_pending" — caller maps each disposition to its 409 body).
	TransitionToAutoPending(ctx context.Context, id string) error
	// Pin — Phase 19 / D-02. Atomically sets pinned_at = now (UTC) on the
	// conversation, scoped by (id, business_id, user_id) for defense-in-depth
	// (Pitfalls §19 — defends against cross-tenant pin manipulation even if
	// callers misroute IDs). Returns ErrConversationNotFound on mismatch
	// (uniform 404 at the handler layer, never 403, to avoid leaking
	// existence-vs-ownership).
	Pin(ctx context.Context, id, businessID, userID string) error
	// Unpin — Phase 19 / D-02. Atomically sets pinned_at = nil on the
	// conversation, scoped by (id, business_id, user_id). Returns
	// ErrConversationNotFound on mismatch.
	Unpin(ctx context.Context, id, businessID, userID string) error
	// SearchTitles — Phase 19 / Plan 19-03 / D-12 phase 1. Runs the $text
	// query against conversations.title scoped by (user_id, business_id,
	// project_id?). Returns title hits AND the slice of matching conversation
	// IDs. Empty businessID or userID returns ErrInvalidScope (cross-tenant
	// defense-in-depth). Result types live in
	// services/api/internal/repository.ConversationTitleHit.
	SearchTitles(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]ConversationTitleHit, []string, error)
	// ScopedConversationIDs — Phase 19 / Plan 19-03 / D-12 phase 1
	// allowlist. Returns the conversation IDs visible to (user_id,
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
	// Update overwrites an existing message by ID. Used by the Phase 16 HITL
	// resume path (Plan 16-06) to append ToolResults to the SAME assistant
	// Message that carried the pause-time ToolCalls (invariant D-17: one
	// assistant Message per LLM turn, even across a pause). If the message
	// does not exist, returns ErrMessageNotFound.
	Update(ctx context.Context, msg *Message) error
	// FindByConversationActive returns the most recent assistant Message in
	// the conversation whose Status is in {pending_approval, in_progress},
	// or (nil, ErrMessageNotFound) if none exists. Used by chat_proxy.go's
	// D-04 stream-open gate (Plan 16-06) to detect in-flight turns before
	// creating a new assistant Message.
	FindByConversationActive(ctx context.Context, conversationID string) (*Message, error)
	// SearchByConversationIDs — Phase 19 / Plan 19-03 / D-12 phase 2.
	// Aggregation pipeline that runs $text on messages.content scoped by
	// the conversation_id allowlist (computed in phase 1 from
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

// --- Phase 16 HITL — pending tool-call batches ---

// PendingToolCallBatch is the persisted snapshot of a paused multi-tool
// approval batch: one document per assistant turn that hit ≥1 manual-floor
// tool. Written by services/orchestrator at pause time (status="preparing"),
// promoted to "pending" just before the SSE tool_approval_required event is
// flushed, transitioned to "resolving" atomically by the resolve endpoint,
// then "resolved" after all decisions are recorded. Expired batches are
// swept by the Mongo TTL index on ExpiresAt (HITL-10).
//
// ProjectID is nullable (bson:",omitempty") because conversations may not be
// scoped to any project (the virtual "Без проекта" bucket from Phase 15).
// When present, it is the key that Plan 16-05 and 16-07 use to look up the
// project's approval_overrides for the TOCTOU re-check (POLICY-03 + HITL-06).
//
// See .planning/phases/16-hitl-backend/16-02-PLAN.md for the Mongo
// collection/index spec and the implementation.
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
// the LLM's real tool_call.id (no synthetic "tc-N" placeholder — HITL-13).
// Verdict/EditedArgs/RejectReason are populated by the resolve endpoint.
// Dispatched is the orchestrator-side double-execution guard (Overview
// invariant #3): on resume, any entry with Dispatched=true is skipped.
type PendingCall struct {
	CallID    string                 `bson:"call_id"`
	ToolName  string                 `bson:"tool_name"`
	Arguments map[string]interface{} `bson:"arguments"`

	// FloorAtPause is the effective ToolFloor at the moment the orchestrator
	// paused the turn for this call. Persisted so the resolve-time TOCTOU
	// re-check can consult the same registry that classified the call at
	// pause time, eliminating divergence between the orchestrator's
	// in-process tools.Registry (always warm) and the api's
	// service.ToolsRegistryCache (HTTP-backed, lazily warmed). See
	// 17-VERIFICATION.md §GAP-04 for the divergence root cause.
	//
	// For pause-time-persisted calls this is always ToolFloorManual (only
	// manual-floor calls reach the orchestrator's pause path; auto and
	// forbidden are bucketed elsewhere). bson:",omitempty" so legacy
	// batches written before plan 17-11 decode with FloorAtPause == ""
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

// PendingToolCallRepository is implemented by services/api in Plan 16-02. The
// interface declares every primitive that the orchestrator (at pause time),
// the resolve handler (at decision time), and the chat_proxy (at SSE emission
// time) need — no type assertions, no out-of-band helpers.
//
// Atomicity discipline: because MongoDB in this deployment is STANDALONE (no
// multi-document transactions — see Overview invariant #1), all cross-document
// consistency is encoded as a strict write-order:
//
//	InsertPreparing → PromoteToPending → emit SSE
//	↓ (crash here → ReconcileOrphanPreparing sweeps after olderThan)
//	AtomicTransitionToResolving → RecordDecisions → MarkDispatched* → MarkResolved
//
// AtomicTransitionToResolving uses findOneAndUpdate with filter
// `{_id, status: "pending"}` and update `{$set: {status: "resolving"}}` to
// guarantee exactly-one-wins on concurrent resolve attempts (Overview
// anti-footgun #5).
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
