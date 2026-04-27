package domain

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WhitelistMode ---

func TestWhitelistMode_Values(t *testing.T) {
	assert.Equal(t, WhitelistMode("inherit"), WhitelistModeInherit)
	assert.Equal(t, WhitelistMode("all"), WhitelistModeAll)
	assert.Equal(t, WhitelistMode("explicit"), WhitelistModeExplicit)
	assert.Equal(t, WhitelistMode("none"), WhitelistModeNone)
}

func TestValidWhitelistMode(t *testing.T) {
	tests := []struct {
		name  string
		mode  WhitelistMode
		valid bool
	}{
		{"inherit", WhitelistModeInherit, true},
		{"all", WhitelistModeAll, true},
		{"explicit", WhitelistModeExplicit, true},
		{"none", WhitelistModeNone, true},
		{"bogus", WhitelistMode("bogus"), false},
		{"empty", WhitelistMode(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, ValidWhitelistMode(tt.mode))
		})
	}
}

// --- MaxProjectSystemPromptChars ---

func TestMaxProjectSystemPromptChars_Value(t *testing.T) {
	assert.Equal(t, 4000, MaxProjectSystemPromptChars,
		"4000 must agree with the Postgres CHECK constraint and the frontend zod schema")
}

// --- Project JSON marshaling ---

func TestProject_JSON_CamelCaseKeys(t *testing.T) {
	p := Project{
		ID:            uuid.New(),
		BusinessID:    uuid.New(),
		Name:          "Reviews",
		Description:   "handle reviews",
		SystemPrompt:  "Reply formally",
		WhitelistMode: WhitelistModeExplicit,
		AllowedTools:  []string{"telegram__send_channel_post"},
		QuickActions:  []string{"Опубликовать", "Ответить на отзыв"},
		CreatedAt:     time.Now().Truncate(time.Second),
		UpdatedAt:     time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(p)
	require.NoError(t, err)
	out := string(data)

	// camelCase keys — matches project's JSON convention (see models.go).
	assert.Contains(t, out, `"businessId"`)
	assert.Contains(t, out, `"systemPrompt"`)
	assert.Contains(t, out, `"whitelistMode"`)
	assert.Contains(t, out, `"allowedTools"`)
	assert.Contains(t, out, `"quickActions"`)

	// snake_case names must NOT appear in JSON (those are db tags).
	assert.NotContains(t, out, `"business_id"`)
	assert.NotContains(t, out, `"system_prompt"`)
	assert.NotContains(t, out, `"whitelist_mode"`)

	// Value round-trip.
	var decoded Project
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, p.Name, decoded.Name)
	assert.Equal(t, WhitelistModeExplicit, decoded.WhitelistMode)
	assert.Equal(t, p.AllowedTools, decoded.AllowedTools)
	assert.Equal(t, p.QuickActions, decoded.QuickActions)
}

// --- Conversation (Phase 15 extensions) ---

func TestConversation_JSON_IncludesNewFields(t *testing.T) {
	projID := "11111111-1111-1111-1111-111111111111"
	lm := time.Now().Truncate(time.Second)
	pinned := time.Now().UTC().Truncate(time.Second)
	c := Conversation{
		ID:            "conv-1",
		UserID:        "user-1",
		BusinessID:    "biz-1",
		ProjectID:     &projID,
		Title:         "Hello",
		TitleStatus:   TitleStatusAuto,
		PinnedAt:      &pinned, // Phase 19 D-02 — replaced legacy `Pinned bool`
		LastMessageAt: &lm,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	data, err := json.Marshal(c)
	require.NoError(t, err)
	out := string(data)

	assert.Contains(t, out, `"businessId"`)
	assert.Contains(t, out, `"projectId"`)
	assert.Contains(t, out, `"titleStatus"`)
	assert.Contains(t, out, `"pinnedAt"`)
	assert.Contains(t, out, `"lastMessageAt"`)
}

func TestConversation_JSON_OmitsNilLastMessageAt(t *testing.T) {
	c := Conversation{
		ID:          "conv-1",
		UserID:      "user-1",
		BusinessID:  "biz-1",
		Title:       "Hello",
		TitleStatus: TitleStatusAutoPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	data, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"lastMessageAt"`,
		"nil LastMessageAt must be omitted via omitempty")
}

func TestConversation_JSON_OmitsNilProjectIDForJSON(t *testing.T) {
	// For JSON we use omitempty on projectId so a nil pointer drops the key.
	c := Conversation{
		ID:          "conv-1",
		UserID:      "user-1",
		BusinessID:  "biz-1",
		Title:       "Hello",
		TitleStatus: TitleStatusAutoPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	data, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"projectId"`,
		"nil ProjectID must be omitted from JSON via omitempty")
}

func TestConversation_BSONTags(t *testing.T) {
	// pkg/ must stay free of the mongo driver (only shared *code* belongs
	// here — repository implementations live in services/api). Verify the
	// BSON tags via reflection so the invariant is enforced without pulling
	// go.mongodb.org/mongo-driver/v2 into pkg/go.mod.
	typ := reflect.TypeOf(Conversation{})

	tests := []struct {
		field string
		want  string
	}{
		// No ,omitempty — nil pointer must serialize as explicit null so
		// Mongo can distinguish "field not set" from "explicitly no project".
		// Move-chat in Plan 04 depends on this.
		{"ProjectID", "project_id"},
		{"BusinessID", "business_id"},
		{"TitleStatus", "title_status"},
		// Phase 19 D-02 — `PinnedAt *time.Time` replaces legacy `Pinned bool`.
		// `pinned_at,omitempty` so unpinned chats serialize without the key
		// (the backfill's $exists:false guard relies on missing-key semantics
		// — see services/api/internal/repository/mongo_backfill.go:BackfillConversationsV19).
		{"PinnedAt", "pinned_at,omitempty"},
		// last_message_at has ,omitempty — nil pointer omits the key.
		{"LastMessageAt", "last_message_at,omitempty"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			f, ok := typ.FieldByName(tt.field)
			require.True(t, ok, "field %s must exist", tt.field)
			assert.Equal(t, tt.want, f.Tag.Get("bson"),
				"bson tag mismatch on %s", tt.field)
		})
	}
}

// --- TitleStatus constants ---

func TestTitleStatus_Values(t *testing.T) {
	assert.Equal(t, "auto_pending", TitleStatusAutoPending)
	assert.Equal(t, "auto", TitleStatusAuto)
	assert.Equal(t, "manual", TitleStatusManual)
}

// --- Project sentinel errors ---

func TestProjectSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrProjectNotFound", ErrProjectNotFound, "project not found"},
		{"ErrProjectExists", ErrProjectExists, "project already exists"},
		{"ErrProjectNameRequired", ErrProjectNameRequired, "project name required"},
		{"ErrProjectSystemPromptTooLong", ErrProjectSystemPromptTooLong, "project system prompt too long (max 4000 chars)"},
		{"ErrProjectWhitelistEmpty", ErrProjectWhitelistEmpty, "explicit whitelist must contain at least one tool"},
		{"ErrProjectWhitelistMode", ErrProjectWhitelistMode, "invalid whitelist mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.err)
			assert.Equal(t, tt.msg, tt.err.Error())
		})
	}
}
