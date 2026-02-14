package prompt_test

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemPrompt_ContainsBusinessName(t *testing.T) {
	ctx := prompt.BusinessContext{
		Name:        "Кофейня Уют",
		Category:    "кофейня",
		Description: "Уютная кофейня в центре",
		Now:         time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC),
	}

	msgs := prompt.Build(ctx, nil)
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

	msgs := prompt.Build(ctx, nil)
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

	msgs := prompt.Build(ctx, history)
	require.Len(t, msgs, 3) // system + 2 history messages
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "assistant", msgs[2].Role)
}

func TestBuildSystemPrompt_EmptyIntegrations(t *testing.T) {
	ctx := prompt.BusinessContext{Name: "Test", Now: time.Now()}
	msgs := prompt.Build(ctx, nil)
	require.Len(t, msgs, 1)
	assert.NotEmpty(t, msgs[0].Content)
}
