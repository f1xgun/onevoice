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
	// GetByEmail filters deleted_at IS NULL — soft-deleted rows surface as ErrUserNotFound.
	GetByEmail(ctx context.Context, email string) (*User, error)
	// GetByEmailIncludingDeleted is the explicit opt-out used by the login path so a
	// soft-deleted user still inside the restore grace window can authenticate.
	GetByEmailIncludingDeleted(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
	// UpdateName sets users.name. Caller trims/validates; ErrUserNotFound when no row.
	UpdateName(ctx context.Context, userID uuid.UUID, name string) error
}

// BusinessRepository persists business records.
type BusinessRepository interface {
	Create(ctx context.Context, business *Business) error
	// CreateInTx dual-writes businesses + business_members atomically via caller tx.
	CreateInTx(ctx context.Context, tx pgx.Tx, business *Business) error
	GetByID(ctx context.Context, id uuid.UUID) (*Business, error)
	// Update writes the editable business profile columns (name, category,
	// address, phone, website, description) only. It does NOT write logo_url
	// nor the settings JSONB. logo_url is persisted via the targeted
	// UpdateLogoURL path, and settings sub-keys via UpdateSettingsKeys /
	// UpdateToolApprovals, so a profile edit cannot revert a concurrent logo
	// upload or settings sub-key change.
	Update(ctx context.Context, business *Business) error
	// UpdateLogoURL writes only the logo_url column. A logo upload uses this so
	// it never carries forward a stale profile snapshot, and a concurrent
	// profile edit (Update) never reverts a freshly uploaded logo.
	UpdateLogoURL(ctx context.Context, id uuid.UUID, url string) error
	// UpdateSettingsKeys writes only the supplied settings sub-keys via a
	// targeted jsonb_set, preserving every other key in the settings JSONB.
	UpdateSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}) error
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
	// GetActiveByPlatformExternal returns the active integration for a
	// (platform, external_id) pair across ALL businesses, or
	// ErrIntegrationNotFound. Unlike GetByBusinessPlatformExternal it is not
	// scoped to one business, so Connect can detect a cross-tenant claim on the
	// same external channel.
	GetActiveByPlatformExternal(ctx context.Context, platform string, externalID string) (*Integration, error)
	ListAllActiveByPlatforms(ctx context.Context, platforms []string) ([]Integration, error)
	Update(ctx context.Context, integration *Integration) error
	Delete(ctx context.Context, id uuid.UUID) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)

	// MarkTokenExpired flips matching active, non-deleted integrations to
	// IntegrationStatusTokenExpired and returns the number of rows updated.
	// When externalID is non-empty the flip is scoped to the single
	// (businessID, platform, externalID) integration so one channel's rejected
	// token does not force a reconnect of a sibling channel on the same
	// platform. An empty externalID falls back to the platform-wide flip for
	// callers that cannot identify the failing integration.
	MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) (int64, error)

	// UpdateMetadata writes only the metadata jsonb of a non-deleted integration
	// via a targeted single-column UPDATE, leaving status, token, and envelope
	// columns untouched so a concurrent status flip (e.g. MarkTokenExpired) is
	// not reverted by a stale read-modify-write snapshot. Returns
	// ErrIntegrationNotFound when no active row matches.
	UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]interface{}) error

	// UpdateExternalID writes only the external_id of a non-deleted integration
	// via a targeted single-column UPDATE, leaving status, token, and envelope
	// columns untouched so a concurrent status flip (e.g. MarkTokenExpired) is
	// not reverted by a stale read-modify-write snapshot. Returns
	// ErrIntegrationNotFound when no active row matches.
	UpdateExternalID(ctx context.Context, id uuid.UUID, externalID string) error

	// CountIntegrationsWithDifferentFingerprint returns the count of non-deleted
	// integrations whose encryption_key_fingerprint is set and differs from
	// currentFP. Used by the fingerprint boot-check to detect key-ID drift.
	CountIntegrationsWithDifferentFingerprint(ctx context.Context, currentFP string) (int, error)

	// SelectForRekey returns up to limit integration rows whose wrapped_dek IS
	// NULL or key_version < targetVersion, locked FOR UPDATE SKIP LOCKED inside
	// the caller's transaction.
	SelectForRekey(ctx context.Context, tx pgx.Tx, targetVersion int16, limit int) ([]Integration, error)

	// UpdateEnvelopeFieldsTx rewrites the four envelope columns
	// (encrypted_access_token, encrypted_refresh_token, encrypted_user_token,
	// wrapped_dek, key_version, encryption_key_fingerprint) inside tx.
	UpdateEnvelopeFieldsTx(ctx context.Context, tx pgx.Tx, integ Integration) error

	// CountRekeyRemaining returns the count of rows still needing rekey —
	// (wrapped_dek IS NULL OR key_version < targetVersion) AND deleted_at IS NULL.
	CountRekeyRemaining(ctx context.Context, targetVersion int16) (int, error)
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

	// ListUserIDsByRole enumerates the user_ids holding roleID in a business (non-tx snapshot).
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
	// Returns the user_ids actually reassigned by the in-tx UPDATE (RETURNING) so the caller fans out
	// authz.InvalidateMember over the authoritative set, not a racy pre-tx snapshot.
	DeleteWithReassignInTx(ctx context.Context, tx pgx.Tx, businessID, oldRoleID, reassignToID, actorUserID uuid.UUID) ([]uuid.UUID, error)

	// Reassign is the legacy non-tx signature; prefer DeleteWithReassignInTx.
	Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error

	CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error)

	// CountInvitationsByRole counts ALL invitations referencing roleID, in any
	// state. invitations.role_id is ON DELETE RESTRICT, so terminal
	// (accepted/revoked/expired) rows pin the role just as pending ones do.
	CountInvitationsByRole(ctx context.Context, roleID uuid.UUID) (int, error)

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
	Category string
	Action   string
	// ExcludeActions drops rows whose action is in this set (SQL NOT IN).
	// Used to hide high-volume system events (e.g. integration.token_decrypted)
	// from the default journal feed while keeping them reachable when the
	// caller explicitly filters by that action. Empty = no exclusion.
	ExcludeActions []string
	ActorID        *uuid.UUID
	From           *time.Time
	To             *time.Time
	CursorTime     *time.Time
	CursorID       *uuid.UUID
	Limit          int
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

// --- Billing (v1.6). See the billing-scaffolding blueprint. ---

// SubscriptionRepository persists per-business billing subscriptions.
type SubscriptionRepository interface {
	// ActiveByBusiness returns the single active subscription for a business,
	// or ErrSubscriptionNotFound when none exists.
	ActiveByBusiness(ctx context.Context, businessID uuid.UUID) (*Subscription, error)
	// Upsert is the Track-B write path (checkout / webhook). Defined now so the
	// interface is stable; Track-A never calls it.
	Upsert(ctx context.Context, sub *Subscription) error
}

// PlanDefinitionRepository reads the plan_definitions catalog.
type PlanDefinitionRepository interface {
	// GetByCode returns one plan, or ErrPlanNotFound.
	GetByCode(ctx context.Context, code string) (*PlanDefinition, error)
	// ListActive returns active plans ordered by sort_order.
	ListActive(ctx context.Context) ([]PlanDefinition, error)
}

// CreditLedgerRepository reads and appends to the append-only credit_ledger.
type CreditLedgerRepository interface {
	// CurrentBalance returns the latest balance_after for a business, or 0 when
	// the business has no ledger rows yet.
	CurrentBalance(ctx context.Context, businessID uuid.UUID) (int, error)
	// Append inserts one ledger row inside the caller's transaction. Idempotent
	// on entry.IdempotencyKey via ON CONFLICT DO NOTHING.
	Append(ctx context.Context, tx pgx.Tx, entry *CreditLedgerEntry) error
}

// MongoDB repositories. See docs/pkg/domain-repository.md.

// ConversationRepository persists chat conversations.
type ConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	GetByID(ctx context.Context, id string) (*Conversation, error)
	// ListByUserID scopes by (user_id, business_id) so a member of multiple
	// organizations only sees the active organization's conversations.
	ListByUserID(ctx context.Context, userID, businessID string, limit, offset int) ([]Conversation, error)
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
	// BumpLastMessageAt advances last_message_at (and updated_at) to ts on every
	// message-append path so the recency sort key the search read paths order by
	// stays current — without it, post-backfill conversations are field-absent
	// and sink/truncate out of the search allowlist.
	BumpLastMessageAt(ctx context.Context, id string, ts time.Time) error
	SearchTitles(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]ConversationTitleHit, []string, error)
	ScopedConversationIDs(ctx context.Context, businessID, userID string, projectID *string) ([]string, error)
	// MongoConversationsCleanup is best-effort post-PG-TX — PG is source of truth.
	MongoConversationsCleanup(ctx context.Context, userID string, originalEmail string) (int64, error)
	// MongoBusinessCleanup flags conversations/posts/agent_tasks/reviews scoped
	// to a hard-deleted organization (and nulls business_id on
	// conversations/posts/agent_tasks; reviews keep it to avoid colliding on
	// their unique key). Best-effort post-PG-TX — PG is source of truth.
	MongoBusinessCleanup(ctx context.Context, businessID string, originalName string) (int64, error)
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
	// DeleteByConversationID removes every message of a conversation. Messages
	// carry only conversation_id (no business_id/user_id), so a conversation
	// delete MUST cascade here first or the message bodies become unreachable by
	// every read path and every cleanup sweep.
	DeleteByConversationID(ctx context.Context, conversationID string) (int64, error)
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

	// GetByExternalID resolves a review by its natural key
	// (business_id, platform, external_id). Returns ErrReviewNotFound when no
	// stored review matches — the chat-reply reconciliation uses it to find the
	// review an LLM-dispatched reply tool just answered on the platform.
	GetByExternalID(ctx context.Context, businessID, platform, externalID string) (*Review, error)
	UpdateReply(ctx context.Context, id, replyText, replyStatus string) error
	Upsert(ctx context.Context, review *Review) error

	// BulkUpsert upserts many reviews in a single round-trip (used by the sync
	// path, which otherwise fired one UpdateOne per fetched review).
	BulkUpsert(ctx context.Context, reviews []*Review) error

	// ListPendingWithoutDraft excludes status="generating" so concurrent passes don't double-call the LLM.
	ListPendingWithoutDraft(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	ListRepliedExamples(ctx context.Context, businessID, platform string, limit int) ([]Review, error)

	// ClaimDraftForGenerating atomically transitions a review to
	// status="generating" only if its draft_status is currently absent, empty,
	// or "failed" (the predicate ListPendingWithoutDraft selects on). It returns
	// claimed=true when this caller won the row and may proceed to the LLM;
	// claimed=false means another concurrent pass already claimed it, so the
	// caller must skip the review (no orchestrator call). This compare-and-swap
	// is what stops two overlapping sync passes from drafting one review twice.
	ClaimDraftForGenerating(ctx context.Context, id string) (claimed bool, err error)

	// UpdateDraft writes draft_* atomically; the ready/failed transitions persist the outcome after the LLM call.
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
	// omitempty keeps a zero ExpiresAt out of the marshaled doc so preparing
	// rows carry no expires_at and stay invisible to the TTL sweep until the
	// promotion UpdateOne sets it explicitly via $set.
	ExpiresAt time.Time `bson:"expires_at,omitempty"`
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
	// AtomicTransitionResolvingToResuming uses findOneAndUpdate{_id, status:"resolving"}
	// to serialize the post-approval resume continuation: at most one /resume
	// claims the batch and runs the billed LLM step. Concurrent callers see
	// ErrBatchNotResolving. Returns ErrBatchNotFound when the _id is missing.
	AtomicTransitionResolvingToResuming(ctx context.Context, batchID string) (*PendingToolCallBatch, error)
	// ResetResolvingToPending compensates a RecordDecisions failure that lands
	// after a successful transition: it flips status resolving→pending only while
	// no verdicts were recorded, so a retried resolve can win the transition again.
	ResetResolvingToPending(ctx context.Context, batchID string) error
	// AtomicTransitionResumingToResolving compensates a resume that claimed the
	// batch (resolving→resuming) but failed to open the orchestrator stream, so
	// the approved tool never dispatched: it flips status resuming→resolving so a
	// retried /resume can re-win the resolving→resuming claim and dispatch the
	// approved tool. It is a no-op once the resume progressed past "resuming".
	// Returns ErrBatchNotFound when the _id is missing.
	AtomicTransitionResumingToResolving(ctx context.Context, batchID string) error
	RecordDecisions(ctx context.Context, batchID string, calls []PendingCall) error
	MarkDispatched(ctx context.Context, batchID, callID string) error
	MarkResolved(ctx context.Context, batchID string) error
	MarkExpired(ctx context.Context, batchID string) error
	ReconcileOrphanPreparing(ctx context.Context, olderThan time.Duration) (int64, error)
	// ReconcileOrphanResolving heals batches stranded in status="resolving" with
	// no recorded verdicts after a crash between a RecordDecisions failure and
	// the compensating reset. olderThan gates it off a legitimately in-flight
	// resolve, which holds "resolving" only momentarily.
	ReconcileOrphanResolving(ctx context.Context, olderThan time.Duration) (int64, error)
	// DeleteByConversationIDs hard-deletes every batch whose conversation_id is in
	// ids and returns the number removed. Used by the project/conversation
	// hard-delete cascade so a batch's ModelMessages snapshot (a full
	// conversation-history PII copy) never outlives the conversation it belongs
	// to. A batch carries only conversation_id/business_id/project_id, so it is
	// unreachable by any read path once its conversation is gone — the cascade
	// must remove it explicitly.
	DeleteByConversationIDs(ctx context.Context, ids []string) (int64, error)
	// DeleteByConversationID is the single-conversation form of
	// DeleteByConversationIDs, used by the per-conversation delete cascade.
	DeleteByConversationID(ctx context.Context, conversationID string) (int64, error)
	// DeleteByBusinessID hard-deletes every batch scoped to businessID and
	// returns the number removed. Used by the business hard-delete cascade: a
	// batch carries business_id + user_id + a ModelMessages snapshot (a full
	// conversation-history PII copy) with no live read path once the business is
	// gone, and an un-promoted "preparing" or reconciled "expired" batch carries
	// no expires_at so the TTL sweep never reaps it. Backed by the
	// pending_tool_calls_business index.
	DeleteByBusinessID(ctx context.Context, businessID string) (int64, error)
}
