package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConversation_PinnedAtJSON — Phase 19 / D-02. The Conversation struct's
// `PinnedAt *time.Time` (added in Phase 19) replaces the legacy `Pinned bool`.
// The new field MUST be JSON-omitted when nil so the API response shape stays
// minimal for unpinned chats and the frontend's `pinnedAt: string | null` model
// receives `undefined` (which == null on read) rather than a literal `null`.
func TestConversation_PinnedAtJSON(t *testing.T) {
	t.Run("non-nil PinnedAt round-trips as ISO timestamp", func(t *testing.T) {
		when := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
		conv := Conversation{
			ID:       "abc",
			PinnedAt: &when,
		}
		b, err := json.Marshal(conv)
		require.NoError(t, err)
		s := string(b)
		assert.Contains(t, s, `"pinnedAt":"2026-04-27T12:00:00Z"`,
			"PinnedAt non-nil must serialize as ISO timestamp under JSON key pinnedAt")
	})

	t.Run("nil PinnedAt omits the JSON key entirely (omitempty)", func(t *testing.T) {
		conv := Conversation{ID: "abc"}
		b, err := json.Marshal(conv)
		require.NoError(t, err)
		s := string(b)
		assert.False(t, strings.Contains(s, "pinnedAt"),
			"nil PinnedAt must NOT emit a pinnedAt key (json:omitempty)")
	})

	t.Run("legacy Pinned bool field is removed (D-02 single source of truth)", func(t *testing.T) {
		// If `Pinned bool` ever returns to the struct it would re-introduce
		// the dual-source-of-truth bug Phase 19 D-02 explicitly removed.
		// Marshal a zero Conversation and assert the JSON contains no
		// `"pinned"` token (only `pinnedAt` is allowed in Phase 19+).
		conv := Conversation{ID: "abc"}
		b, err := json.Marshal(conv)
		require.NoError(t, err)
		s := string(b)
		assert.False(t, strings.Contains(s, `"pinned"`),
			"legacy Pinned bool field is removed in Phase 19 D-02 — JSON output must contain no `pinned` key")
	})

	t.Run("JSON unmarshaling parses pinnedAt back to *time.Time", func(t *testing.T) {
		raw := `{"id":"abc","pinnedAt":"2026-04-27T12:00:00Z"}`
		var conv Conversation
		require.NoError(t, json.Unmarshal([]byte(raw), &conv))
		require.NotNil(t, conv.PinnedAt)
		expected := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
		assert.True(t, conv.PinnedAt.Equal(expected),
			"unmarshalled PinnedAt must equal the original ISO timestamp")
	})
}
