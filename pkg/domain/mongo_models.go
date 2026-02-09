package domain

import "time"

type Conversation struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	UserID    string    `json:"userId" bson:"user_id"`
	Title     string    `json:"title" bson:"title"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
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
	CreatedAt      time.Time              `json:"createdAt" bson:"created_at"`
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
