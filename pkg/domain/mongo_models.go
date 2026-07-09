package domain

import "time"

// TitleStatus values carried by Conversation.TitleStatus. The auto-titler
// flips "auto_pending" → "auto" when it succeeds; user overrides
// set the value to "manual".
const (
	TitleStatusAutoPending = "auto_pending"
	TitleStatusAuto        = "auto"
	TitleStatusManual      = "manual"
)

// Message.Status values (POLICY / HITL). An empty string is
// semantically equivalent to MessageStatusComplete for backward compatibility
// with legacy messages; see the docstring on Message.Status.
const (
	MessageStatusComplete        = "complete"
	MessageStatusPendingApproval = "pending_approval"
	MessageStatusInProgress      = "in_progress"
)

// ToolCall.Status values (HITL). Empty string means "no approval
// required" (auto-floor tool) — the same zero-value-is-default pattern as
// Message.Status. Non-empty values track the approval lifecycle.
const (
	ToolCallStatusPending  = "pending_approval"
	ToolCallStatusApproved = "approved"
	ToolCallStatusRejected = "rejected"
	ToolCallStatusExpired  = "expired"
)

// Review.DraftStatus values. Empty string == "never attempted" for backward
// compatibility with reviews that pre-date the AI-draft feature.
const (
	ReviewDraftStatusGenerating = "generating"
	ReviewDraftStatusReady      = "ready"
	ReviewDraftStatusFailed     = "failed"
)

// Review.ReplyStatus values used by the syncer and the manual-reply handler.
const (
	ReviewReplyStatusPending = "pending"
	ReviewReplyStatusReplied = "replied"
	ReviewReplyStatusError   = "error"
)

// ReviewNeedsReviewMaxRating is the inclusive upper rating bound at which a
// drafted reply is flagged NeedsReview: ratings at or below it are negative /
// neutral, so their drafts are held back for careful individual approval and
// are never eligible for a one-tap bulk publish. Ratings strictly above it are
// treated as positive.
const ReviewNeedsReviewMaxRating = 3

// Conversation is a chat thread stored in MongoDB.
// ProjectID intentionally omits bson `omitempty` so nil serializes as
// explicit `null` (the virtual "Без проекта" bucket) rather
// than as a missing field — matters for the move-chat endpoint
// which must be able to clear the field.
//
// Pinned bool removed — single source of truth is
// PinnedAt != nil. See repository/mongo_backfill.go:BackfillConversationsV19
// for the migration that drops the legacy bool via $unset and migrates
// any pinned:true rows to pinned_at = updated_at.
type Conversation struct {
	ID            string     `json:"id" bson:"_id,omitempty"`
	UserID        string     `json:"userId" bson:"user_id"`
	BusinessID    string     `json:"businessId" bson:"business_id"`
	ProjectID     *string    `json:"projectId,omitempty" bson:"project_id"`
	Title         string     `json:"title" bson:"title"`
	TitleStatus   string     `json:"titleStatus" bson:"title_status"`
	PinnedAt      *time.Time `json:"pinnedAt,omitempty" bson:"pinned_at,omitempty"`
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty" bson:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"createdAt" bson:"created_at"`
	UpdatedAt     time.Time  `json:"updatedAt" bson:"updated_at"`
}

type Message struct {
	ID             string                 `json:"id" bson:"_id,omitempty"`
	ConversationID string                 `json:"conversationId" bson:"conversation_id"`
	Role           string                 `json:"role" bson:"role"`
	Content        string                 `json:"content" bson:"content"`
	Attachments    []Attachment           `json:"attachments,omitempty" bson:"attachments,omitempty"`
	ToolCalls      []ToolCall             `json:"toolCalls,omitempty" bson:"tool_calls,omitempty"`
	ToolResults    []ToolResult           `json:"toolResults,omitempty" bson:"tool_results,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
	// Status is the HITL message lifecycle marker. Valid non-empty
	// values: "complete", "pending_approval", "in_progress" (see
	// MessageStatus* constants above).
	//
	// Empty string == "complete" for backward compatibility with legacy
	// messages (no backfill write needed). Any reader that branches on Status
	// MUST treat "" and "complete" identically.
	Status    string    `json:"status,omitempty" bson:"status,omitempty"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
}

type Attachment struct {
	Type     string `json:"type" bson:"type"`
	URL      string `json:"url" bson:"url"`
	MimeType string `json:"mimeType" bson:"mime_type"`
	Name     string `json:"name" bson:"name"`
}

type ToolCall struct {
	ID        string                 `json:"id" bson:"id"`
	Name      string                 `json:"name" bson:"name"`
	Arguments map[string]interface{} `json:"arguments" bson:"arguments"`
	// ApprovalID correlates a persisted tool call with a pending-approval
	// batch (HITL). Stamped on the tool call at pause time
	// (chat_proxy.go), persisted in the approval_id header of the NATS
	// dispatch, and keyed into Redis at each platform agent for dedupe.
	// Format: "<batch_id>-<call_id>". Empty for auto-floor tools.
	ApprovalID string `json:"approvalId,omitempty" bson:"approval_id,omitempty"`
	// Status tracks the approval lifecycle of this call (see
	// ToolCallStatus* constants). Empty means "no approval required"
	// (auto-floor tool); non-empty values are "pending_approval",
	// "approved", "rejected", or "expired".
	Status string `json:"status,omitempty" bson:"status,omitempty"`
}

type ToolResult struct {
	ToolCallID string                 `json:"toolCallId" bson:"tool_call_id"`
	Content    map[string]interface{} `json:"content" bson:"content"`
	IsError    bool                   `json:"isError" bson:"is_error"`
	// Code carries the typed classifier emitted by the platform agent
	// when the call errored. Empty for success and for historical
	// rows (omitempty preserves backward-compat — old documents decode
	// with Code == "").
	Code string `json:"code,omitempty" bson:"code,omitempty"`
}

type AgentTask struct {
	ID         string `json:"id" bson:"_id,omitempty"`
	BusinessID string `json:"businessId" bson:"business_id"`
	Type       string `json:"type" bson:"type"`
	// DisplayName is the legacy Russian-only label written before i18n landed
	// Kept as the fallback for documents that have not yet been
	// backfilled; new writes always populate DisplayNameKey in addition.
	DisplayName string `json:"displayName,omitempty" bson:"display_name,omitempty"`
	// DisplayNameKey is the i18n catalog key the frontend uses to render
	// the task title (e.g. "sync.business_name", "tools.telegram.send_channel_post.name").
	// The FE prefers t(displayNameKey) and falls back to DisplayName when
	// the key is missing. New code MUST set this whenever it writes
	// DisplayName so the document can be rendered in any locale.
	DisplayNameKey string      `json:"displayNameKey,omitempty" bson:"display_name_key,omitempty"`
	Status         string      `json:"status" bson:"status"`
	Platform       string      `json:"platform" bson:"platform"`
	Input          interface{} `json:"input,omitempty" bson:"input,omitempty"`
	Output         interface{} `json:"output,omitempty" bson:"output,omitempty"`
	Error          string      `json:"error,omitempty" bson:"error,omitempty"`
	// ErrorCode is the typed classifier from the platform agent's
	// classify*Error function (locked enum: integration_token_invalid,
	// rate_limit_exceeded, transient, channel_not_found, media_too_large).
	// Empty for success rows and for historical documents — omitempty
	// preserves backward-compat (old rows decode with ErrorCode == "" and
	// the FE renders the fallback summary).
	ErrorCode string `json:"errorCode,omitempty" bson:"error_code,omitempty"`
	// DispatchApprovalID is the HITL dedupe key ("<batch_id>-<call_id>") the
	// original approved dispatch ran under. Stamped at first-dispatch time so a
	// retry re-sends the SAME key and the agent's (business_id, approval_id)
	// dedupe gate returns the cached result of a call that already landed instead
	// of repeating an irreversible side effect. Empty for legacy rows that
	// predate the field (those fall back to the stable per-task retry key).
	DispatchApprovalID string     `json:"dispatchApprovalId,omitempty" bson:"dispatch_approval_id,omitempty"`
	StartedAt          *time.Time `json:"startedAt,omitempty" bson:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completedAt,omitempty" bson:"completed_at,omitempty"`
	CreatedAt          time.Time  `json:"createdAt" bson:"created_at"`
}

type Review struct {
	ID           string                 `json:"id" bson:"_id,omitempty"`
	BusinessID   string                 `json:"businessId" bson:"business_id"`
	Platform     string                 `json:"platform" bson:"platform"`
	ExternalID   string                 `json:"externalId" bson:"external_id"`
	AuthorName   string                 `json:"authorName" bson:"author_name"`
	Rating       int                    `json:"rating" bson:"rating"`
	Text         string                 `json:"text" bson:"text"`
	ReplyText    string                 `json:"replyText,omitempty" bson:"reply_text,omitempty"`
	ReplyStatus  string                 `json:"replyStatus" bson:"reply_status"`
	PlatformMeta map[string]interface{} `json:"platformMeta,omitempty" bson:"platform_meta,omitempty"`
	// DispatchApprovalID is the HITL dedupe key ("<batch_id>-<call_id>") of the
	// approved chat dispatch that first replied to this review. Stamped when the
	// LLM-dispatched reply reconciles, so an operator's later manual retry
	// re-sends the SAME key and the agent returns the cached result instead of
	// posting a second public reply. Empty for legacy rows and for replies never
	// dispatched through the chat path (those fall back to the stable per-review
	// retry key).
	DispatchApprovalID string    `json:"dispatchApprovalId,omitempty" bson:"dispatch_approval_id,omitempty"`
	CreatedAt          time.Time `json:"createdAt" bson:"created_at"`

	// RepliedAt is the moment the reply that transitioned ReplyStatus to
	// "replied" was persisted. It is stamped in the same write that flips the
	// status (repository.updateReply) so response-time math has an end point.
	// Nil means unknown — legacy rows answered before this field existed, and
	// rows that were never answered, both decode with RepliedAt == nil and are
	// excluded from response-time aggregates rather than treated as instant.
	RepliedAt *time.Time `json:"repliedAt,omitempty" bson:"replied_at,omitempty"`

	// AI draft of a reply, generated by the orchestrator from few-shot
	// examples of past replies. The draft is offered to the operator —
	// they choose to send it as-is or edit. Status values:
	//   ""           — never attempted (default for legacy docs)
	//   "generating" — in-flight (next sync may retry on stale generating)
	//   "ready"      — DraftReply is populated and safe to surface
	//   "failed"     — generation errored; DraftError carries the reason
	DraftReply       string    `json:"draftReply,omitempty" bson:"draft_reply,omitempty"`
	DraftStatus      string    `json:"draftStatus,omitempty" bson:"draft_status,omitempty"`
	DraftGeneratedAt time.Time `json:"draftGeneratedAt,omitempty" bson:"draft_generated_at,omitempty"`
	DraftError       string    `json:"draftError,omitempty" bson:"draft_error,omitempty"`

	// NeedsReview marks a drafted reply that must be approved individually and
	// is excluded from any one-tap bulk publish. It is set true when a draft is
	// generated for a negative / neutral review (rating <= ReviewNeedsReviewMaxRating)
	// so a critical reply is never auto-sent from a batch. Default false for
	// legacy docs — a reader that gates on it treats missing as false.
	NeedsReview bool `json:"needsReview,omitempty" bson:"needs_review,omitempty"`

	// DraftAcceptedUnedited / DraftEditDistance record how the owner treated the
	// AI draft that produced this reply (see ReviewDraftFeedback). They are
	// stamped on the transition to "replied" and, unlike the transient draft_*
	// fields, are NOT cleared — so the drafter can prefer drafts the owner
	// accepted with little editing as few-shot exemplars. Nil for legacy rows and
	// for replies that had no prior AI draft (no signal to record).
	DraftAcceptedUnedited *bool `json:"draftAcceptedUnedited,omitempty" bson:"draft_accepted_unedited,omitempty"`
	DraftEditDistance     *int  `json:"draftEditDistance,omitempty" bson:"draft_edit_distance,omitempty"`
}

// ReviewDraftFeedback is the signal captured at reply time about how the owner
// treated the AI draft: whether they sent it essentially unedited, and the rune
// edit distance from the draft to the final reply. It feeds the self-improving
// loop — the drafter biases future few-shot toward drafts the owner accepts.
type ReviewDraftFeedback struct {
	AcceptedUnedited bool
	EditDistance     int
}

type Post struct {
	ID              string                    `json:"id" bson:"_id,omitempty"`
	BusinessID      string                    `json:"businessId" bson:"business_id"`
	Content         string                    `json:"content" bson:"content"`
	MediaURLs       []string                  `json:"mediaUrls,omitempty" bson:"media_urls,omitempty"`
	PlatformResults map[string]PlatformResult `json:"platformResults,omitempty" bson:"platform_results,omitempty"`
	Status          string                    `json:"status" bson:"status"`
	ScheduledAt     *time.Time                `json:"scheduledAt,omitempty" bson:"scheduled_at,omitempty"`
	PublishedAt     *time.Time                `json:"publishedAt,omitempty" bson:"published_at,omitempty"`
	CreatedAt       time.Time                 `json:"createdAt" bson:"created_at"`
}

type PlatformResult struct {
	PostID string `json:"postId" bson:"post_id"`
	URL    string `json:"url" bson:"url"`
	Status string `json:"status" bson:"status"`
	Error  string `json:"error,omitempty" bson:"error,omitempty"`
}
