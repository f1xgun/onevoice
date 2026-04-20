package prompt_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

func TestBuildSystemPrompt_ContainsBusinessName(t *testing.T) {
	ctx := prompt.BusinessContext{
		Name:        "Кофейня Уют",
		Category:    "кофейня",
		Description: "Уютная кофейня в центре",
		Now:         time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC),
	}

	msgs := prompt.Build(ctx, nil, nil)
	require.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Contains(t, msgs[0].Content, "Кофейня Уют")
}

func TestBuildSystemPrompt_ContainsIntegrations(t *testing.T) {
	ctx := prompt.BusinessContext{
		Name:               "Test Biz",
		ActiveIntegrations: []string{"telegram", "vk"},
		Now:                time.Now(),
	}

	msgs := prompt.Build(ctx, nil, nil)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Content, "telegram")
	assert.Contains(t, msgs[0].Content, "vk")
}

func TestBuildSystemPrompt_AppendsHistory(t *testing.T) {
	ctx := prompt.BusinessContext{Name: "Test", Now: time.Now()}
	history := []llm.Message{
		{Role: "user", Content: "Привет"},
		{Role: "assistant", Content: "Здравствуйте!"},
	}

	msgs := prompt.Build(ctx, nil, history)
	require.Len(t, msgs, 3) // system + 2 history messages
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "assistant", msgs[2].Role)
}

func TestBuildSystemPrompt_EmptyIntegrations(t *testing.T) {
	ctx := prompt.BusinessContext{Name: "Test", Now: time.Now()}
	msgs := prompt.Build(ctx, nil, nil)
	require.Len(t, msgs, 1)
	assert.NotEmpty(t, msgs[0].Content)
}

// TestBuildSystemPrompt_NilProject_NoProjectBlock — Behavior 1:
// passing nil project leaves the system message identical (no "## Проект" block).
func TestBuildSystemPrompt_NilProject_NoProjectBlock(t *testing.T) {
	biz := prompt.BusinessContext{
		Name:               "Кофейня Уют",
		ActiveIntegrations: []string{"telegram"},
		Now:                time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC),
	}
	history := []llm.Message{{Role: "user", Content: "hi"}}

	msgs := prompt.Build(biz, nil, history)
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].Role)
	assert.NotContains(t, msgs[0].Content, "## Проект", "nil project must not emit a project block")
	assert.NotContains(t, msgs[0].Content, "Проект:")
}

// TestBuildSystemPrompt_WithProject_AppendsAfterRules — Behavior 2:
// project block contains everything the nil case contains plus the "## Проект: {Name}"
// block AFTER the "## Правила" block, followed by the system prompt verbatim.
func TestBuildSystemPrompt_WithProject_AppendsAfterRules(t *testing.T) {
	biz := prompt.BusinessContext{
		Name:               "Кофейня Уют",
		ActiveIntegrations: []string{"telegram"},
		Now:                time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC),
	}
	history := []llm.Message{{Role: "user", Content: "hi"}}
	proj := &prompt.ProjectContext{
		ID:           "proj-x",
		Name:         "Отзывы",
		SystemPrompt: "Отвечай вежливо",
	}

	msgs := prompt.Build(biz, proj, history)
	require.Len(t, msgs, 2)
	content := msgs[0].Content

	// Contains original business content + project block
	assert.Contains(t, content, "Кофейня Уют")
	assert.Contains(t, content, "## Правила")
	assert.Contains(t, content, "## Проект: Отзывы")
	assert.Contains(t, content, "Отвечай вежливо")

	// Ordering: Правила must come BEFORE Проект
	idxRules := strings.Index(content, "## Правила")
	idxProj := strings.Index(content, "## Проект:")
	require.NotEqual(t, -1, idxRules, "rules block not found")
	require.NotEqual(t, -1, idxProj, "project block not found")
	assert.Less(t, idxRules, idxProj, "project block MUST appear after business rules block")
}

// TestBuildSystemPrompt_WithProject_BlankPrompt — Behavior 3:
// project block still renders the header even when SystemPrompt is empty
// (so the LLM knows which project it is in).
func TestBuildSystemPrompt_WithProject_BlankPrompt(t *testing.T) {
	biz := prompt.BusinessContext{Name: "Test", Now: time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC)}
	proj := &prompt.ProjectContext{ID: "x", Name: "Blank", SystemPrompt: ""}

	msgs := prompt.Build(biz, proj, nil)
	require.Len(t, msgs, 1)
	content := msgs[0].Content
	assert.Contains(t, content, "## Проект: Blank")
}

// TestBuildSystemPrompt_Purity — Behavior 4:
// the builder is pure w.r.t. the project layering — two calls with identical
// input (including identical Now) yield byte-identical output.
func TestBuildSystemPrompt_Purity(t *testing.T) {
	biz := prompt.BusinessContext{
		Name:               "Pure",
		ActiveIntegrations: []string{"telegram"},
		Now:                time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC),
	}
	proj := &prompt.ProjectContext{ID: "x", Name: "Отзывы", SystemPrompt: "rules"}
	history := []llm.Message{{Role: "user", Content: "hi"}}

	a := prompt.Build(biz, proj, history)
	b := prompt.Build(biz, proj, history)
	require.Len(t, a, 2)
	require.Len(t, b, 2)
	assert.Equal(t, a[0].Content, b[0].Content)
}

// TestBuildSystemPrompt_ProjectPromptWithTrailingNewline — sanity check:
// a system prompt that already ends with a newline should not gain a double newline.
func TestBuildSystemPrompt_ProjectPromptWithTrailingNewline(t *testing.T) {
	biz := prompt.BusinessContext{Name: "Test", Now: time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC)}
	proj := &prompt.ProjectContext{ID: "x", Name: "N", SystemPrompt: "body\n"}

	msgs := prompt.Build(biz, proj, nil)
	require.Len(t, msgs, 1)
	content := msgs[0].Content
	// Should not contain three consecutive newlines as a result of the body
	assert.NotContains(t, content, "body\n\n\n", "trailing newline should not compound")
}

// GAP-02 tests — appendProjectBlock must communicate whitelist restrictions to
// the LLM so it explains "channel unavailable" instead of silently
// substituting the closest allowed tool. See
// .planning/phases/15-projects-foundation/15-VERIFICATION.md §GAP-02.
//
// All tests below go through prompt.Build (not appendProjectBlock directly —
// it's unexported) so they cover the real end-to-end system-prompt shape.

func buildWithProj(t *testing.T, proj *prompt.ProjectContext) string {
	t.Helper()
	biz := prompt.BusinessContext{Name: "Test", Now: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)}
	msgs := prompt.Build(biz, proj, nil)
	require.Len(t, msgs, 1)
	return msgs[0].Content
}

func TestAppendProjectBlock_ExplicitMode_AppendsAllowedToolsHint(t *testing.T) {
	proj := &prompt.ProjectContext{
		Name:          "Отзывы",
		SystemPrompt:  "Отвечай вежливо",
		WhitelistMode: domain.WhitelistModeExplicit,
		AllowedTools:  []string{"telegram__send_channel_post", "telegram__send_channel_photo"},
	}
	got := buildWithProj(t, proj)

	assert.Contains(t, got, "### Ограничения инструментов", "expected hint header")
	assert.Contains(t, got, "telegram__send_channel_post, telegram__send_channel_photo", "expected comma-joined tool list")
	assert.Contains(t, got, "разрешены только", "expected phrase 'разрешены только'")
	assert.Contains(t, got, "НЕ подменяй канал молча", "expected anti-substitution instruction")
}

func TestAppendProjectBlock_NoneMode_AppendsNoneHint(t *testing.T) {
	proj := &prompt.ProjectContext{
		Name:          "Молчун",
		WhitelistMode: domain.WhitelistModeNone,
	}
	got := buildWithProj(t, proj)

	assert.Contains(t, got, "### Ограничения инструментов")
	assert.Contains(t, got, "все инструменты отключены")
	assert.Contains(t, got, "НЕ подменяй канал молча")
}

func TestAppendProjectBlock_AllMode_NoHint(t *testing.T) {
	proj := &prompt.ProjectContext{
		Name:          "Всё разрешено",
		WhitelistMode: domain.WhitelistModeAll,
		AllowedTools:  []string{"telegram__send_channel_post"}, // should be ignored for `all`
	}
	got := buildWithProj(t, proj)

	assert.NotContains(t, got, "### Ограничения инструментов", "no hint expected for WhitelistModeAll")
}

func TestAppendProjectBlock_InheritMode_NoHint(t *testing.T) {
	proj := &prompt.ProjectContext{
		Name:          "Наследует",
		WhitelistMode: domain.WhitelistModeInherit,
	}
	got := buildWithProj(t, proj)

	assert.NotContains(t, got, "### Ограничения инструментов", "no hint expected for WhitelistModeInherit")
}

func TestAppendProjectBlock_EmptyMode_NoHint(t *testing.T) {
	// Back-compat: call sites that don't populate the new fields still work —
	// empty string falls through to the default branch with no hint.
	proj := &prompt.ProjectContext{
		Name:         "Legacy",
		SystemPrompt: "body",
	}
	got := buildWithProj(t, proj)

	assert.NotContains(t, got, "### Ограничения инструментов", "no hint expected for empty WhitelistMode")
	assert.Contains(t, got, "## Проект: Legacy", "legacy header must still render")
}

func TestAppendProjectBlock_ExplicitModeEmptyAllowedTools_FallsBackToNoneWording(t *testing.T) {
	// Defensive: service layer rejects this via ErrProjectWhitelistEmpty, but
	// if bad data sneaks through we emit the same wording as WhitelistModeNone.
	proj := &prompt.ProjectContext{
		Name:          "Пусто",
		WhitelistMode: domain.WhitelistModeExplicit,
		AllowedTools:  []string{},
	}
	got := buildWithProj(t, proj)

	assert.Contains(t, got, "### Ограничения инструментов")
	assert.Contains(t, got, "все инструменты отключены", "empty allowed-tools should read like WhitelistModeNone")
	assert.NotContains(t, got, "разрешены только", "don't emit 'разрешены только' when list is empty")
}

func TestAppendProjectBlock_ExplicitMode_InstructsAgainstSubstitution(t *testing.T) {
	// Regression guard against the GAP-02 reproduction: the anti-substitution
	// instruction MUST be present verbatim. If a refactor removes this
	// substring, GAP-02 comes back.
	proj := &prompt.ProjectContext{
		Name:          "Регрессия",
		WhitelistMode: domain.WhitelistModeExplicit,
		AllowedTools:  []string{"telegram__send_channel_post"},
	}
	got := buildWithProj(t, proj)

	assert.Contains(t, got, "НЕ подменяй канал молча")
	// And the weaker but useful invariant — we tell it HOW to refuse (explain + alternative).
	assert.Contains(t, got, "объясни вежливо")
}
