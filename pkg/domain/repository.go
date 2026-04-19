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
	CallID       string                 `bson:"call_id"`
	ToolName     string                 `bson:"tool_name"`
	Arguments    map[string]interface{} `bson:"arguments"`
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
