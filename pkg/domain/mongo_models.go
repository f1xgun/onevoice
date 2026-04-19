package domain

import "time"

// TitleStatus values carried by Conversation.TitleStatus. Phase 18 flips
// "auto_pending" → "auto" when the auto-titler succeeds; user overrides
// set the value to "manual".
const (
	TitleStatusAutoPending = "auto_pending"
	TitleStatusAuto        = "auto"
	TitleStatusManual      = "manual"
)

// Message.Status values (POLICY / HITL — Phase 16). An empty string is
// semantically equivalent to MessageStatusComplete for backward compatibility
// with pre-Phase-16 messages; see the docstring on Message.Status.
const (
	MessageStatusComplete        = "complete"
	MessageStatusPendingApproval = "pending_approval"
	MessageStatusInProgress      = "in_progress"
)

// ToolCall.Status values (Phase 16 HITL). Empty string means "no approval
// required" (auto-floor tool) — the same zero-value-is-default pattern as
// Message.Status. Non-empty values track the approval lifecycle.
const (
	ToolCallStatusPending  = "pending_approval"
	ToolCallStatusApproved = "approved"
	ToolCallStatusRejected = "rejected"
	ToolCallStatusExpired  = "expired"
)

// Conversation is a chat thread stored in MongoDB. Phase 15 adds
// BusinessID, ProjectID, TitleStatus, Pinned, and LastMessageAt.
// ProjectID intentionally omits bson `omitempty` so nil serializes as
// explicit `null` (the virtual "Без проекта" bucket in UI-11) rather
// than as a missing field — matters for the move-chat endpoint in
// Plan 04 which must be able to clear the field.
type Conversation struct {
	ID            string     `json:"id" bson:"_id,omitempty"`
	UserID        string     `json:"userId" bson:"user_id"`
	BusinessID    string     `json:"businessId" bson:"business_id"`
	ProjectID     *string    `json:"projectId,omitempty" bson:"project_id"`
	Title         string     `json:"title" bson:"title"`
	TitleStatus   string     `json:"titleStatus" bson:"title_status"`
	Pinned        bool       `json:"pinned" bson:"pinned"`
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
	// Status is the Phase 16 HITL message lifecycle marker. Valid non-empty
	// values: "complete", "pending_approval", "in_progress" (see
	// MessageStatus* constants above).
	//
	// Empty string == "complete" for backward compatibility with pre-Phase-16
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
	// batch (Phase 16 HITL). Stamped on the tool call at pause time
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
}

type AgentTask struct {
	ID          string      `json:"id" bson:"_id,omitempty"`
	BusinessID  string      `json:"businessId" bson:"business_id"`
	Type        string      `json:"type" bson:"type"`
	Status      string      `json:"status" bson:"status"`
	Platform    string      `json:"platform" bson:"platform"`
	Input       interface{} `json:"input,omitempty" bson:"input,omitempty"`
	Output      interface{} `json:"output,omitempty" bson:"output,omitempty"`
	Error       string      `json:"error,omitempty" bson:"error,omitempty"`
	StartedAt   *time.Time  `json:"startedAt,omitempty" bson:"started_at,omitempty"`
	CompletedAt *time.Time  `json:"completedAt,omitempty" bson:"completed_at,omitempty"`
	CreatedAt   time.Time   `json:"createdAt" bson:"created_at"`
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
	CreatedAt    time.Time              `json:"createdAt" bson:"created_at"`
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
