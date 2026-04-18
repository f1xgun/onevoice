package prompt_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
