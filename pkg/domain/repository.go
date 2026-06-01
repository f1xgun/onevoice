package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PostgreSQL repositories. See docs/pkg/domain-repository.md.

// UserRepository persists application users.
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	// GetByID filters deleted_at IS NULL — soft-deleted rows surface as ErrUserNotFound.
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	// GetByIDIncludingDeleted is the explicit opt-out used by /auth/me grace-window restore.
	GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

// BusinessRepository persists business records.
type BusinessRepository interface {
	Create(ctx context.Context, business *Business) error
	// CreateInTx dual-writes businesses + business_members atomically via caller tx.
	CreateInTx(ctx context.Context, tx pgx.Tx, business *Business) error
	GetByID(ctx context.Context, id uuid.UUID) (*Business, error)
	Update(ctx context.Context, business *Business) error
	// UpdateToolApprovals replaces only settings.tool_approvals, preserving other JSONB keys.
	UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]ToolFloor) error
}

// BusinessScheduleRepository persists per-business schedule windows.
type BusinessScheduleRepository interface {
	GetByBusinessID(ctx context.Context, businessID uuid.UUID) ([]BusinessSchedule, error)
	Upsert(ctx context.Context, schedule *BusinessSchedule) error
	DeleteByBusinessID(ctx context.Context, businessID uuid.UUID) error
}

// IntegrationRepository persists per-business platform integrations.
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

// BusinessMembershipRepository persists the v2 RBAC membership graph.
// See docs/pkg/domain-repository.md — InTx pairs compose with authz invariants under one tx.
type BusinessMembershipRepository interface {
	// Insert is tx-scoped; wraps pgx duplicate-key as ErrMembershipExists.
	Insert(ctx context.Context, tx pgx.Tx, m *BusinessMember) error

	GetByBusinessUser(ctx context.Context, businessID, userID uuid.UUID) (*BusinessMember, error)

	ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]BusinessMember, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]BusinessMember, error)

	// CountOwnersByBusiness shares the active-only filter used by EnsureOwnerExistsAfter.
	CountOwnersByBusiness(ctx context.Context, businessID uuid.UUID) (int, error)

	UpdateRole(ctx context.Context, businessID, userID, newRoleID, actorUserID uuid.UUID) error

	// UpdateRoleInTx shares RepeatableRead with EnsureOwnerExistsAfter's SELECT FOR UPDATE.
	UpdateRoleInTx(ctx context.Context, tx pgx.Tx, businessID, userID, newRoleID, actorUserID uuid.UUID) error

	Delete(ctx context.Context, businessID, userID uuid.UUID) error

	// DeleteInTx shares the tx with the last-owner invariant lock window.
	DeleteInTx(ctx context.Context, tx pgx.Tx, businessID, userID uuid.UUID) error

	// ListUserIDsByRole feeds post-commit authz.InvalidateMember fan-out; captured BEFORE tx.Commit.
	ListUserIDsByRole(ctx context.Context, businessID, roleID uuid.UUID) ([]uuid.UUID, error)
}

// RoleWithMemberCount augments a Role with per-business member count for delete-with-reassign UX.
type RoleWithMemberCount struct {
	Role
	MemberCount int `json:"member_count"`
}

// RoleRepository covers role CRUD. See docs/pkg/domain-repository.md.
type RoleRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*Role, error)
	ListSystem(ctx context.Context) ([]Role, error)
	ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]Role, error)
	ListByBusinessWithCounts(ctx context.Context, businessID uuid.UUID) ([]RoleWithMemberCount, error)

	// Create inserts a custom role; UNIQUE (business_id, name) conflict → ErrRoleNameTaken.
	Create(ctx context.Context, role *Role) error
	CreateInTx(ctx context.Context, tx pgx.Tx, role *Role) error

	Update(ctx context.Context, role *Role) error
	// UpdateInTx filters is_system=false — both missing and system rows return ErrRoleNotFound.
	UpdateInTx(ctx context.Context, tx pgx.Tx, role *Role) error

	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteInTx requires zero members (caller verifies) and refuses system rows.
	DeleteInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	// DeleteWithReassignInTx reassigns then deletes — FK business_members.role_id is ON DELETE RESTRICT.
	DeleteWithReassignInTx(ctx context.Context, tx pgx.Tx, businessID, oldRoleID, reassignToID, actorUserID uuid.UUID) error

	// Reassign is the legacy non-tx signature; prefer DeleteWithReassignInTx.
	Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error

	CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error)

	GetByMemberInBusiness(ctx context.Context, businessID, userID uuid.UUID) (*Role, error)
}

// InvitationRepository persists business-scoped invitations. See docs/pkg/domain-repository.md.
type InvitationRepository interface {
	Create(ctx context.Context, inv *Invitation) error
	// CreateInTx + CountPendingByBusinessInTx hold under Serializable for the 20-pending cap.
	CreateInTx(ctx context.Context, tx pgx.Tx, inv *Invitation) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	ListPendingByBusiness(ctx context.Context, businessID uuid.UUID) ([]Invitation, error)
	CountPendingByBusiness(ctx context.Context, businessID uuid.UUID) (int, error)
	CountPendingByBusinessInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) (int, error)
	// Revoke scopes by businessID — 404 on cross-tenant mismatch.
	Revoke(ctx context.Context, id, businessID uuid.UUID) error
	MarkAccepted(ctx context.Context, id, accepterUserID uuid.UUID) error
	// MarkAcceptedInTx MUST share the tx with the membership INSERT for single-use atomicity.
	MarkAcceptedInTx(ctx context.Context, tx pgx.Tx, id, accepterUserID uuid.UUID) error
}

// AuditLogFilter is the filter set for AuditLogRepository.ListByBusiness.
// Empty strings / nil pointers mean "no filter"; CursorTime + CursorID are paired.
type AuditLogFilter struct {
	Category   string
	Action     string
	ActorID    *uuid.UUID
	From       *time.Time
	To         *time.Time
	CursorTime *time.Time
	CursorID   *uuid.UUID
	Limit      int
}

// AuditLogRepository persists and queries audit_logs. See docs/pkg/domain-repository.md.
type AuditLogRepository interface {
	// Insert must be safe with BusinessID == nil AND UserID == nil (failed-login entries).
	Insert(ctx context.Context, log *AuditLog) error
	// ListByBusiness orders by (created_at DESC, id DESC); caller passes last row tuple as cursor.
	ListByBusiness(ctx context.Context, businessID uuid.UUID, filter AuditLogFilter) ([]AuditLog, error)
	// DeleteOlderThan runs inside the retention sweep's advisory-lock window (held by caller).
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// MongoDB repositories. See docs/pkg/domain-repository.md.

// ConversationRepository persists chat conversations.
type ConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	GetByID(ctx context.Context, id string) (*Conversation, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]Conversation, error)
	Update(ctx context.Context, conv *Conversation) error
	Delete(ctx context.Context, id string) error
	// UpdateProjectAssignment: bson:"project_id" without omitempty so nil writes explicit null.
	UpdateProjectAssignment(ctx context.Context, id string, projectID *string) error
	// UpdateTitleIfPending: trust-critical — manual renames MUST NOT be clobbered.
	UpdateTitleIfPending(ctx context.Context, id, title string) error
	TransitionToAutoPending(ctx context.Context, id string) error
	// Pin scopes by (id, business_id, user_id) — uniform 404 over 403 to avoid existence leak.
	Pin(ctx context.Context, id, businessID, userID string) error
	Unpin(ctx context.Context, id, businessID, userID string) error
	SearchTitles(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]ConversationTitleHit, []string, error)
	ScopedConversationIDs(ctx context.Context, businessID, userID string, projectID *string) ([]string, error)
	// MongoConversationsCleanup is best-effort post-PG-TX — PG is source of truth.
	MongoConversationsCleanup(ctx context.Context, userID string, originalEmail string) (int64, error)
}

// ConversationTitleHit is the per-row projection from SearchTitles.
type ConversationTitleHit struct {
	ID            string     `bson:"_id"`
	Title         string     `bson:"title"`
	ProjectID     *string    `bson:"project_id"`
	UserID        string     `bson:"user_id"`
	BusinessID    string     `bson:"business_id"`
	Score         float64    `bson:"score"`
	LastMessageAt *time.Time `bson:"last_message_at"`
}

// MessageRepository persists chat messages.
type MessageRepository interface {
	Create(ctx context.Context, msg *Message) error
	ListByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]Message, error)
	CountByConversationID(ctx context.Context, conversationID string) (int64, error)
	// Update lets HITL resume append ToolResults to the same assistant Message across a pause.
	Update(ctx context.Context, msg *Message) error
	FindByConversationActive(ctx context.Context, conversationID string) (*Message, error)
	// SearchByConversationIDs: scope enforced ENTIRELY by allowlist — Message has no business_id.
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

// Filter types.

// ReviewFilter filters ReviewRepository.ListByBusinessID.
type ReviewFilter struct {
	Platform    string
	ReplyStatus string
	Limit       int
	Offset      int
}

// PostFilter filters PostRepository.ListByBusinessID.
type PostFilter struct {
	Platform string
	Status   string
	Limit    int
	Offset   int
}

// TaskFilter filters AgentTaskRepository.ListByBusinessID.
type TaskFilter struct {
	Platform string
	Status   string
	Type     string
	Limit    int
	Offset   int
}

// MongoDB repositories for content.

// ReviewRepository persists platform reviews + AI draft state.
type ReviewRepository interface {
	ListByBusinessID(ctx context.Context, businessID string, filter ReviewFilter) ([]Review, int, error)
	GetByID(ctx context.Context, id string) (*Review, error)
	UpdateReply(ctx context.Context, id, replyText, replyStatus string) error
	Upsert(ctx context.Context, review *Review) error

	// ListPendingWithoutDraft excludes status="generating" so concurrent passes don't double-call the LLM.
	ListPendingWithoutDraft(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	ListRepliedExamples(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	// UpdateDraft writes draft_* atomically; status="generating" claims a row before the LLM call.
	UpdateDraft(ctx context.Context, id, draft, status, errMsg string) error
}

// PostRepository persists platform posts.
type PostRepository interface {
	Create(ctx context.Context, post *Post) error
	ListByBusinessID(ctx context.Context, businessID string, filter PostFilter) ([]Post, int, error)
	GetByID(ctx context.Context, id string) (*Post, error)
}

// AgentTaskRepository persists agent task state.
type AgentTaskRepository interface {
	Create(ctx context.Context, task *AgentTask) error
	Update(ctx context.Context, task *AgentTask) error
	GetByID(ctx context.Context, businessID, taskID string) (*AgentTask, error)
	ListByBusinessID(ctx context.Context, businessID string, filter TaskFilter) ([]AgentTask, int, error)
}

// --- HITL — pending tool-call batches. See docs/pkg/hitlstore.md + docs/pkg/domain-repository.md. ---

// PendingToolCallBatch is the persisted snapshot of a paused multi-tool approval batch.
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

// PendingCall is a single proposed tool invocation within a batch.
type PendingCall struct {
	CallID    string                 `bson:"call_id"`
	ToolName  string                 `bson:"tool_name"`
	Arguments map[string]interface{} `bson:"arguments"`

	// FloorAtPause pins resolve-time TOCTOU re-check to the registry that classified the call at pause.
	// Avoids orchestrator-vs-API registry drift. omitempty for legacy-batch decode.
	FloorAtPause ToolFloor `bson:"floor_at_pause,omitempty"`

	Verdict      string                 `bson:"verdict,omitempty"` // "approve" | "edit" | "reject"
	EditedArgs   map[string]interface{} `bson:"edited_args,omitempty"`
	RejectReason string                 `bson:"reject_reason,omitempty"`
	// Dispatched is the orchestrator-side double-execution guard on resume.
	Dispatched   bool       `bson:"dispatched"`
	DispatchedAt *time.Time `bson:"dispatched_at,omitempty"`
}

// PendingToolCallRepository is implemented by pkg/hitlstore.
// See docs/pkg/hitlstore.md for state machine + atomicity proof.
type PendingToolCallRepository interface {
	Persist(ctx context.Context, b *PendingToolCallBatch) error
	GetByBatchID(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	ListPendingByConversation(ctx context.Context, conversationID string) ([]*PendingToolCallBatch, error)
	// AtomicTransitionToResolving uses findOneAndUpdate{_id, status:"pending"} for exactly-one-wins.
	AtomicTransitionToResolving(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	RecordDecisions(ctx context.Context, batchID string, calls []PendingCall) error
	MarkDispatched(ctx context.Context, batchID, callID string) error
	MarkResolved(ctx context.Context, batchID string) error
	MarkExpired(ctx context.Context, batchID string) error
	ReconcileOrphanPreparing(ctx context.Context, olderThan time.Duration) (int64, error)
}
